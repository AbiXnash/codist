package review

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"codist/internal/javaidx"
)

// Mechanical fixes for rules whose correction is fully determined by the finding. They run
// instantly and produce exactly the change described; the user still approves the diff.
var mechanical = map[string]bool{
	"console-output": true, "literal-instead-of-constant": true, "duplicate-log": true,
	"log-helper-method": true, "uninjected-dependency": true,
}

type placed struct {
	path string
	f    Finding
}

// autofixMechanical applies the mechanical fixes for every place of an issue, per file.
func (x *Fixer) autofixMechanical(members []placed, current map[string]string) ([]edit, bool) {
	for _, m := range members {
		if !mechanical[m.f.Ref] {
			return nil, false
		}
	}
	byFile := map[string][]Finding{}
	var order []string
	for _, m := range members {
		if _, ok := byFile[m.path]; !ok {
			order = append(order, m.path)
		}
		byFile[m.path] = append(byFile[m.path], m.f)
	}
	var edits []edit
	for _, path := range order {
		src := current[path]
		out, ok := applyMechanical(path, src, byFile[path])
		if !ok {
			return nil, false
		}
		if out != src {
			edits = append(edits, edit{path: path, original: src, proposed: out})
		}
	}
	return edits, len(edits) > 0
}

var (
	printStmt      = regexp.MustCompile(`^(\s*)System\.(out|err)\.print(ln)?\((.*)\);\s*$`)
	logLineInFix   = regexp.MustCompile(`(?m)^log\.\w+\(.*\);$`)
	loggerClass    = regexp.MustCompile(`getLogger\((\w+)\.class\)`)
	literalTitle   = regexp.MustCompile(`^("(?:[^"\\]|\\.)*") duplicates constant (\S+)$`)
	helperTitle    = regexp.MustCompile(`^(\w+)\(\) is a log-only helper$`)
	injectionTitle = regexp.MustCompile(`^(\w+) is never injected: (\w+)\.(\w+) stays null$`)
)

func applyMechanical(path, src string, findings []Finding) (string, bool) {
	lines := strings.Split(src, "\n")
	drop := map[int]bool{}      // 0-based line indexes to delete
	after := map[int][]string{} // lines to insert after a 0-based index
	needLogger := ""

	types := javaidx.TypesIn([]byte(src), path)
	for _, f := range findings {
		i := f.Line - 1
		if i < 0 || i >= len(lines) {
			return "", false
		}
		switch f.Ref {
		case "console-output":
			m := printStmt.FindStringSubmatch(lines[i])
			call := logLineInFix.FindString(f.Suggestion)
			if m == nil || call == "" {
				return "", false
			}
			if m[2] == "err" {
				call = strings.Replace(call, "log.info(", "log.error(", 1)
			}
			lines[i] = m[1] + call
			if c := loggerClass.FindStringSubmatch(f.Suggestion); c != nil {
				needLogger = c[1]
			}
		case "literal-instead-of-constant":
			m := literalTitle.FindStringSubmatch(f.Title)
			if m == nil || !strings.Contains(lines[i], m[1]) {
				return "", false
			}
			lines[i] = strings.ReplaceAll(lines[i], m[1], m[2])
		case "duplicate-log":
			if !strings.HasSuffix(strings.TrimSpace(lines[i]), ";") {
				return "", false
			}
			drop[i] = true
		case "log-helper-method":
			m := helperTitle.FindStringSubmatch(f.Title)
			if m == nil {
				return "", false
			}
			start, end := memberRange(types, m[1], f.Line)
			if start == 0 {
				return "", false
			}
			for k := start - 1; k < end; k++ {
				drop[k] = true
			}
			if start >= 2 && strings.TrimSpace(lines[start-2]) == "" {
				drop[start-2] = true // the blank line above the method
			}
			call := regexp.MustCompile(`^\s*(this\.)?` + regexp.QuoteMeta(m[1]) + `\(\);\s*$`)
			for k, l := range lines {
				if call.MatchString(l) {
					drop[k] = true
				}
			}
		case "uninjected-dependency":
			m := injectionTitle.FindStringSubmatch(f.Title)
			if m == nil {
				return "", false
			}
			typ, cls, field := m[1], m[2], m[3]
			for _, t := range types {
				if t.Name == cls {
					for _, mem := range t.Members {
						if mem.Kind == "constructor" {
							return "", false // existing constructors: let the model merge it
						}
					}
				}
			}
			decl := regexp.MustCompile(`^(\s*)(private|protected|public)?\s*` + regexp.QuoteMeta(typ) + `\s+` + regexp.QuoteMeta(field) + `\s*;`)
			d := decl.FindStringSubmatch(lines[i])
			if d == nil {
				return "", false
			}
			vis := d[2]
			if vis == "" {
				vis = "private"
			}
			indent := d[1]
			lines[i] = fmt.Sprintf("%s%s final %s %s;", indent, vis, typ, field)
			after[i] = append(after[i], "", fmt.Sprintf("%spublic %s(%s %s) {", indent, cls, typ, field),
				fmt.Sprintf("%s    this.%s = %s;", indent, field, field), indent+"}")
		}
	}

	if needLogger != "" && !strings.Contains(src, "LoggerFactory.getLogger(") {
		cls := regexp.MustCompile(`^\s*(public\s+|final\s+|abstract\s+)*class\s+` + regexp.QuoteMeta(needLogger) + `\b.*\{\s*$`)
		placedLogger := false
		for k, l := range lines {
			if cls.MatchString(l) {
				after[k] = append([]string{"    private static final Logger log = LoggerFactory.getLogger(" + needLogger + ".class);", ""}, after[k]...)
				placedLogger = true
				break
			}
		}
		if !placedLogger {
			return "", false
		}
		lastImport := -1
		for k, l := range lines {
			if strings.HasPrefix(l, "import ") {
				lastImport = k
			}
		}
		var imports []string
		for _, imp := range []string{"import org.slf4j.Logger;", "import org.slf4j.LoggerFactory;"} {
			if !strings.Contains(src, imp) {
				imports = append(imports, imp)
			}
		}
		if lastImport >= 0 {
			after[lastImport] = append(imports, after[lastImport]...)
		}
	}

	var out []string
	for k, l := range lines {
		if !drop[k] {
			out = append(out, l)
		}
		out = append(out, after[k]...)
	}
	return collapseBlankRuns(strings.Join(out, "\n")), true
}

// memberRange finds the full line range of a method by name starting at line.
func memberRange(types []javaidx.Node, name string, line int) (int, int) {
	for _, t := range types {
		for _, m := range t.Members {
			if m.Name == name && m.StartLine <= line && line <= m.EndLine {
				return m.StartLine, m.EndLine
			}
		}
	}
	return 0, 0
}

// collapseBlankRuns removes the double blank lines that deletions leave behind.
func collapseBlankRuns(s string) string {
	return regexp.MustCompile(`\n[ \t]*\n([ \t]*\n)+`).ReplaceAllString(s, "\n\n")
}

// relocate moves findings to where their line of code is now. Places whose code is gone are
// dropped: they were already fixed or rewritten.
func relocate(members []placed, current map[string]string) []placed {
	var out []placed
	for _, m := range members {
		src, ok := current[m.path]
		if !ok {
			continue
		}
		lines := strings.Split(src, "\n")
		want := strings.TrimSpace(m.f.Snippet)
		if want == "" {
			if m.f.Line <= len(lines) {
				out = append(out, m)
			}
			continue
		}
		best, bestDist := -1, 1<<30
		for k, l := range lines {
			if strings.TrimSpace(l) == want {
				if d := abs(k + 1 - m.f.Line); d < bestDist {
					best, bestDist = k+1, d
				}
			}
		}
		if best < 0 {
			continue
		}
		delta := best - m.f.Line
		m.f.Line, m.f.EndLine = best, m.f.EndLine+delta
		out = append(out, m)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].f.GroupRoot && !out[b].f.GroupRoot })
	return out
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
