package review

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"codist/internal/javaidx"
)

var wordInQuestion = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// CodeFacts answers the parts of a question that the code can answer for certain. The names in the
// question pick the methods; the selected class helps when it names none. The model then explains.
func CodeFacts(question, path string, g *javaidx.Graph, cg *javaidx.CallGraph) string {
	type picked struct {
		file, class, name string
	}
	classes := map[string]javaidx.Node{}
	for _, n := range g.Nodes {
		classes[n.Name] = n
	}
	var picks []picked
	seen := map[string]bool{}
	pick := func(file, class, name string) {
		key := file + class + name
		if !seen[key] && len(picks) < 8 {
			seen[key] = true
			picks = append(picks, picked{file, class, name})
		}
	}
	var words []string
	for _, w := range wordInQuestion.FindAllString(question, -1) {
		if len(w) >= 3 {
			words = append(words, w)
		}
	}
	named := map[string]bool{}
	for _, w := range words {
		named[w] = true
	}
	// A method named in the question, in any class, or in the class it names.
	for _, n := range g.Nodes {
		for _, m := range n.Members {
			if m.Kind == "method" && named[m.Name] && (named[n.Name] || n.File == path || !anyClassNamed(named, classes)) {
				pick(n.File, n.Name, m.Name)
			}
		}
	}
	// A class named with no method: its methods.
	if len(picks) == 0 {
		names := make([]string, 0, len(named))
		for w := range named {
			names = append(names, w)
		}
		sort.Strings(names)
		for _, w := range names {
			if n, ok := classes[w]; ok {
				for _, m := range n.Members {
					if m.Kind == "method" {
						pick(n.File, n.Name, m.Name)
					}
				}
			}
		}
	}
	if len(picks) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## What the code shows (measured; \"possible\" means matched by name only, and reflection or configuration may add more)\n")
	for _, p := range picks {
		m := cg.Method(p.file, p.class, p.name)
		if m == nil {
			continue
		}
		fmt.Fprintf(&b, "- %s.%s() in %s:%d\n", p.class, p.name, p.file, m.Line)
		var callers, tests, routes []string
		routes = append(routes, m.Routes...)
		for _, r := range cg.CallersOf(p.file, p.class, p.name, impactDepth) {
			name := r.Method.Class + "." + r.Method.Name
			if !r.Confirmed {
				name += " (possible)"
			}
			if r.Method.Test {
				tests = append(tests, name)
				continue
			}
			callers = append(callers, name)
			routes = append(routes, r.Method.Routes...)
		}
		fmt.Fprintf(&b, "  called by: %s\n  API routes: %s\n  tests: %s\n", listOrNone(callers), listOrNone(routes), listOrNone(tests))
	}
	return b.String()
}

func anyClassNamed(named map[string]bool, classes map[string]javaidx.Node) bool {
	for w := range named {
		if _, ok := classes[w]; ok {
			return true
		}
	}
	return false
}

func listOrNone(items []string) string {
	if len(items) == 0 {
		return "none found"
	}
	if len(items) > 12 {
		return strings.Join(items[:12], ", ") + fmt.Sprintf(" and %d more", len(items)-12)
	}
	return strings.Join(items, ", ")
}
