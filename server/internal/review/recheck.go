package review

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"codist/internal/javaidx"
	"codist/internal/knowledge"
)

// RuleContext is what re-running the rules needs: the project context (constants, domains, beans)
// and the knowledge base.
// ok is false until the project has been indexed.
type RuleContext func() (ctx javaidx.Context, kb *knowledge.Base, ok bool)

// RuleScan runs the Java rules and knowledge-base patterns over every line of a file.
func RuleScan(path string, src []byte, ctx javaidx.Context, kb *knowledge.Base) []Finding {
	ls := strings.Split(string(src), "\n")
	all := make(map[int]bool, len(ls))
	for n := 1; n <= len(ls); n++ {
		all[n] = true
	}
	ctx.Path = path
	out := []Finding{}
	for _, x := range javaidx.Check(src, all, ctx) {
		out = append(out, Finding{Line: x.Line, EndLine: x.EndLine, Severity: x.Severity, Category: x.Category,
			Title: x.Title, Detail: x.Detail, Suggestion: x.Suggestion, Source: "rule", Ref: x.ID, Snippet: lineAt(ls, x.Line)})
	}
	if kb != nil {
		for _, h := range kb.Detect(path, ls, nil) {
			if hasFinding(out, h.Line, h.Rule.ID) {
				continue
			}
			out = append(out, Finding{Line: h.Line, EndLine: h.Line, Severity: h.Rule.Severity, Category: h.Rule.Category,
				Title: h.Rule.Title, Detail: strings.Join(strings.Fields(h.Rule.Why), " "), Suggestion: strings.TrimRight(h.Rule.Good, "\n"),
				Source: "rule", Ref: h.Rule.ID, Snippet: lineAt(ls, h.Line)})
		}
	}
	return out
}

// Recheckable: a rule finding on a Java file that a whole-file scan can confirm. Findings that
// come from the diff itself (a constant's value changing) cannot be re-derived from one version.
func Recheckable(path string, f Finding) bool {
	return f.Source == "rule" && f.Ref != "" && f.Ref != "shared-constant-changed" && strings.HasSuffix(path, ".java")
}

// fires reports whether a scan still flags ref at line. The code can be unchanged while the issue
// is gone, e.g. a constant's value was corrected in another file.
func fires(scan []Finding, ref string, line int) bool {
	for _, x := range scan {
		if x.Ref == ref && (x.Line == line || (line >= x.Line && line <= x.EndLine)) {
			return true
		}
	}
	return false
}

var constDecl = regexp.MustCompile(`(?m)\bstatic\s+final\s+[\w<>\[\]]+\s+([A-Z][A-Z0-9_]*)\s*=\s*("(?:[^"\\]|\\.)*")\s*;`)

// constants maps each string constant declared in src to its literal.
func constants(src string) map[string]string {
	out := map[string]string{}
	for _, m := range constDecl.FindAllStringSubmatch(src, -1) {
		out[m[1]] = m[2]
	}
	return out
}

// duplicateConstants lists constants the proposed version adds whose value another constant in the
// same file already holds: the fix should reuse that one instead.
func duplicateConstants(before, after string) []string {
	old := constants(before)
	now := constants(after)
	var out []string
	for name, val := range now {
		if _, existed := old[name]; existed {
			continue
		}
		var same []string
		for other, v := range now {
			if other != name && v == val {
				same = append(same, other)
			}
		}
		if len(same) > 0 {
			sort.Strings(same)
			out = append(out, fmt.Sprintf("%s = %s duplicates %s, which already has that value; use it instead of adding a constant", name, val, strings.Join(same, ", ")))
		}
	}
	sort.Strings(out)
	return out
}
