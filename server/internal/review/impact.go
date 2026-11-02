package review

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"codist/internal/gitx"
	"codist/internal/javaidx"
)

const (
	impactDepth   = 6  // how many calls up to follow
	impactListMax = 40 // the report lists this many callers; the count covers them all
)

// ImpactPlace is a method a change can affect.
type ImpactPlace struct {
	Class     string `json:"class"`
	Method    string `json:"method"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Depth     int    `json:"depth"`
	Confirmed bool   `json:"confirmed"` // false: matched by name only
}

// ImpactRoute is a REST endpoint a change can reach.
type ImpactRoute struct {
	Route string `json:"route"`
	ImpactPlace
}

// ChangedMethod is a method the commit touched, with what depends on it.
type ChangedMethod struct {
	Class   string `json:"class"`
	Method  string `json:"method"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	Status  string `json:"status"` // added, modified, signature, removed
	Callers int    `json:"callers"`
	Tests   int    `json:"tests"`
	// Names of the nearest callers, routes and tests, for the model.
	CalledBy []string `json:"calledBy"`
	Routes   []string `json:"routes"`
	TestedBy []string `json:"testedBy"`
}

// ImpactReport is what a commit can affect, measured from the code.
type ImpactReport struct {
	SHA          string          `json:"sha"`
	Changed      []ChangedMethod `json:"changed"`
	Callers      []ImpactPlace   `json:"callers"`
	CallerCount  int             `json:"callerCount"`
	Possible     int             `json:"possible"` // of those, matched by name only
	Routes       []ImpactRoute   `json:"routes"`
	Tests        []ImpactPlace   `json:"tests"`
	Classes      int             `json:"classes"`
	UntestedRisk int             `json:"untestedRisk"` // existing methods changed with no test reaching them
}

var impactCache = struct {
	sync.Mutex
	items map[string]ImpactReport
}{items: map[string]ImpactReport{}}

// ImpactFor is cached: a commit's code never changes, and the review asks once per file.
func ImpactFor(repo *gitx.Repo, a Analysis) ImpactReport {
	key := repo.Root + "|" + a.Commit.SHA
	impactCache.Lock()
	if r, ok := impactCache.items[key]; ok {
		impactCache.Unlock()
		return r
	}
	impactCache.Unlock()
	r := computeImpact(repo, a)
	impactCache.Lock()
	if len(impactCache.items) > 64 {
		impactCache.items = map[string]ImpactReport{}
	}
	impactCache.items[key] = r
	impactCache.Unlock()
	return r
}

func computeImpact(repo *gitx.Repo, a Analysis) ImpactReport {
	sources := repo.JavaFilesAt(a.Commit.SHA)
	g := javaidx.Index(sources, a.Commit.SHA)
	return buildImpact(a, sources, javaidx.BuildCallGraph(sources, &g), func(path string) (string, bool) {
		if len(a.Commit.Parents) == 0 {
			return "", false
		}
		return repo.ReadFile(a.Commit.Parents[0], path)
	})
}

func buildImpact(a Analysis, sources map[string][]byte, cg *javaidx.CallGraph, before func(path string) (string, bool)) ImpactReport {
	rep := ImpactReport{SHA: a.Commit.SHA, Changed: []ChangedMethod{}, Callers: []ImpactPlace{}, Routes: []ImpactRoute{}, Tests: []ImpactPlace{}}
	callers := map[string]ImpactPlace{}
	tests := map[string]ImpactPlace{}
	routes := map[string]ImpactRoute{}
	place := func(r javaidx.Reached) ImpactPlace {
		return ImpactPlace{Class: r.Method.Class, Method: r.Method.Name, File: r.Method.File, Line: r.Method.Line, Depth: r.Depth, Confirmed: r.Confirmed}
	}
	keep := func(old ImpactPlace, ok bool, next ImpactPlace) bool {
		return !ok || (next.Confirmed && !old.Confirmed) || (next.Confirmed == old.Confirmed && next.Depth < old.Depth)
	}
	visit := func(c *ChangedMethod) {
		for _, r := range cg.CallersOf(c.File, c.Class, c.Method, impactDepth) {
			p := place(r)
			key := p.Class + "#" + p.Method + "|" + p.File
			if r.Method.Test {
				c.Tests++
				if len(c.TestedBy) < 4 {
					c.TestedBy = append(c.TestedBy, p.Class+"."+p.Method)
				}
				if old, ok := tests[key]; keep(old, ok, p) {
					tests[key] = p
				}
				continue
			}
			c.Callers++
			if len(c.CalledBy) < 6 {
				name := p.Class + "." + p.Method
				if !r.Confirmed {
					name += " (possible)"
				}
				c.CalledBy = append(c.CalledBy, name)
			}
			if old, ok := callers[key]; keep(old, ok, p) {
				callers[key] = p
			}
			for _, route := range r.Method.Routes {
				if len(c.Routes) < 6 {
					c.Routes = append(c.Routes, route)
				}
				if old, ok := routes[route]; keep(old.ImpactPlace, ok, p) {
					routes[route] = ImpactRoute{Route: route, ImpactPlace: p}
				}
			}
		}
	}
	for _, f := range a.Files {
		if !f.IsJava() || strings.Contains("/"+f.Path, "/src/test/") {
			continue
		}
		var old map[string]string
		if src, ok := before(firstNonEmpty(f.OldPath, f.Path)); ok {
			old = javaidx.TopLevelMethods([]byte(src))
		}
		var now map[string]string
		if !f.Deleted() {
			now = javaidx.TopLevelMethods(sources[f.NewPath])
			lines := append(append([]int{}, f.AddedLines...), f.DeletedAt...)
			for _, m := range cg.MethodsIn(f.NewPath, lines) {
				c := ChangedMethod{Class: m.Class, Method: m.Name, File: m.File, Line: m.Line, Status: "modified", CalledBy: []string{}, Routes: []string{}, TestedBy: []string{}}
				key := m.Class + "#" + m.Name
				if sig, ok := old[key]; !ok {
					c.Status = "added"
				} else if sig != now[key] {
					c.Status = "signature"
				}
				for _, route := range m.Routes {
					c.Routes = append(c.Routes, route)
					routes[route] = ImpactRoute{Route: route, ImpactPlace: ImpactPlace{Class: m.Class, Method: m.Name, File: m.File, Line: m.Line, Confirmed: true}}
				}
				visit(&c)
				rep.Changed = append(rep.Changed, c)
			}
		}
		// A removed method's callers are still in the code, and they are the ones that will break.
		var gone []string
		for key := range old {
			if _, ok := now[key]; !ok {
				gone = append(gone, key)
			}
		}
		sort.Strings(gone)
		for _, key := range gone {
			class, name, _ := strings.Cut(key, "#")
			c := ChangedMethod{Class: class, Method: name, File: f.Path, Status: "removed", CalledBy: []string{}, Routes: []string{}, TestedBy: []string{}}
			visit(&c)
			rep.Changed = append(rep.Changed, c)
		}
	}
	classes := map[string]bool{}
	for _, p := range callers {
		classes[p.Class] = true
		rep.CallerCount++
		if !p.Confirmed {
			rep.Possible++
		}
		rep.Callers = append(rep.Callers, p)
	}
	rep.Classes = len(classes)
	less := func(a, b ImpactPlace) bool {
		if a.Confirmed != b.Confirmed {
			return a.Confirmed
		}
		if a.Depth != b.Depth {
			return a.Depth < b.Depth
		}
		return a.Class+a.Method < b.Class+b.Method
	}
	sort.Slice(rep.Callers, func(i, j int) bool { return less(rep.Callers[i], rep.Callers[j]) })
	if len(rep.Callers) > impactListMax {
		rep.Callers = rep.Callers[:impactListMax]
	}
	for _, t := range tests {
		rep.Tests = append(rep.Tests, t)
	}
	sort.Slice(rep.Tests, func(i, j int) bool { return less(rep.Tests[i], rep.Tests[j]) })
	for _, r := range routes {
		rep.Routes = append(rep.Routes, r)
	}
	sort.Slice(rep.Routes, func(i, j int) bool { return rep.Routes[i].Route < rep.Routes[j].Route })
	for _, c := range rep.Changed {
		if c.Status != "added" && c.Tests == 0 {
			rep.UntestedRisk++
		}
	}
	return rep
}

// impactSection is the same facts for the model, for the methods of one file.
func impactSection(rep ImpactReport, path string) string {
	var b strings.Builder
	for _, c := range rep.Changed {
		if c.File != path {
			continue
		}
		fmt.Fprintf(&b, "- %s() [%s]", c.Method, c.Status)
		switch c.Status {
		case "signature":
			b.WriteString(" its signature changed, so every caller must still match")
		case "removed":
			b.WriteString(" it no longer exists, so callers below will not work")
		}
		if len(c.CalledBy) > 0 {
			fmt.Fprintf(&b, "; called by %s", strings.Join(c.CalledBy, ", "))
			if c.Callers > len(c.CalledBy) {
				fmt.Fprintf(&b, " and %d more", c.Callers-len(c.CalledBy))
			}
		} else {
			b.WriteString("; no callers found in the project")
		}
		if len(c.Routes) > 0 {
			fmt.Fprintf(&b, "; reachable from %s", strings.Join(c.Routes, ", "))
		}
		if len(c.TestedBy) > 0 {
			fmt.Fprintf(&b, "; tests reaching it: %s", strings.Join(c.TestedBy, ", "))
		} else {
			b.WriteString("; no test reaches it")
		}
		b.WriteString("\n")
	}
	return b.String()
}
