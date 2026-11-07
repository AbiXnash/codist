package app

import (
	"net/http"
	"path"
	"sort"
	"strings"

	"github.com/AbiXnash/codist-java"
	"github.com/AbiXnash/codist/internal/web"
)

type repoMethod struct {
	Class  string    `json:"class"`
	Node   string    `json:"node"`
	Method string    `json:"method"`
	File   string    `json:"file"`
	Line   int       `json:"line"`
	Cost   java.Cost `json:"cost"`
}

func (a *App) repoReport(w http.ResponseWriter, r *http.Request) {
	ix := a.index()
	ref := a.viewRef()
	files, _ := a.repo.Files(a.ixSHA)
	langs := map[string]int{}
	javaFiles, testFiles := 0, 0
	for _, f := range files {
		ext := strings.TrimPrefix(path.Ext(f), ".")
		if ext == "" {
			ext = path.Base(f)
		}
		langs[ext]++
		if strings.HasSuffix(f, ".java") {
			javaFiles++
			if strings.Contains(f, "/src/test/") {
				testFiles++
			}
		}
	}
	open := map[string]map[string]int{} // file -> severity -> count
	issues, sev := map[string]int{}, map[string]int{}
	for _, is := range a.store.AllIssues() {
		if is.State != "open" {
			continue
		}
		if open[is.Path] == nil {
			open[is.Path] = map[string]int{}
		}
		open[is.Path][is.Latest.Severity]++
		issues[kinds[category(is.Latest.Rule, is.Latest.Source)]]++
		sev[is.Latest.Severity]++
	}
	moduleOf := map[string]string{}
	methods := 0
	roles := map[string]int{}
	var costly []repoMethod
	largest := []map[string]any{}
	for _, t := range ix.Types {
		moduleOf[t.File] = t.Module
		methods += len(t.Methods)
		roles[role(t)]++
		for _, m := range t.Methods {
			if m.Cost.LoopIO > 0 || m.Cost.LoopDepth >= 2 || m.Cost.Complexity >= 8 {
				costly = append(costly, repoMethod{Class: t.Name, Node: t.ID, Method: m.Name, File: t.File, Line: m.Line, Cost: m.Cost})
			}
		}
	}
	sort.Slice(costly, func(i, j int) bool {
		x, y := costly[i].Cost, costly[j].Cost
		if x.LoopIO != y.LoopIO {
			return x.LoopIO > y.LoopIO
		}
		if x.LoopDepth != y.LoopDepth {
			return x.LoopDepth > y.LoopDepth
		}
		return x.Complexity > y.Complexity
	})
	costlyTotal := len(costly)
	costly = costly[:min(15, len(costly))]
	types := append([]*java.Type(nil), ix.Types...)
	sort.Slice(types, func(i, j int) bool { return types[i].Lines() > types[j].Lines() })
	for _, t := range types[:min(10, len(types))] {
		largest = append(largest, map[string]any{"Name": t.Name, "Node": t.ID, "File": t.File, "Lines": t.Lines(), "Members": len(t.Fields) + len(t.Methods)})
	}

	churn := a.repo.Churn(2000)
	modCommits := map[string]int{}
	lines := map[string]int{}
	for _, t := range ix.Types {
		lines[t.File] = max(lines[t.File], t.End)
	}
	type hot struct {
		Path          string `json:"path"`
		Commits, Open int
		Lines         int `json:"lines"`
	}
	var hots []map[string]any
	for p, n := range churn {
		if m := moduleOf[p]; m != "" {
			modCommits[m] += n
		}
		if l, ok := lines[p]; ok {
			o := 0
			for _, c := range open[p] {
				o += c
			}
			hots = append(hots, map[string]any{"path": p, "commits": n, "open": o, "lines": l})
		}
	}
	sort.Slice(hots, func(i, j int) bool {
		x, y := hots[i]["commits"].(int)*(hots[i]["open"].(int)+1), hots[j]["commits"].(int)*(hots[j]["open"].(int)+1)
		if x != y {
			return x > y
		}
		return hots[i]["path"].(string) < hots[j]["path"].(string)
	})
	hots = hots[:min(12, len(hots))]

	services := []map[string]any{}
	files2 := map[string]int{}
	for _, f := range files {
		for _, m := range ix.Modules {
			if m.Dir != "" && strings.HasPrefix(f, m.Dir+"/") {
				files2[m.Name]++
			}
		}
	}
	for _, m := range ix.Modules {
		o := map[string]int{}
		for p, bySev := range open {
			if moduleOf[p] == m.Name {
				for s, n := range bySev {
					o[s] += n
				}
			}
		}
		var port any
		if m.Port != "" {
			port = m.Port
		}
		services = append(services, map[string]any{"name": m.Name, "port": port, "kind": m.Kind, "files": files2[m.Name], "lines": m.Lines,
			"types": m.Types, "tests": m.Tests, "endpoints": m.Endpoints, "calls": orEmptyS(m.Calls), "calledBy": orEmptyS(m.CalledBy),
			"usesLibs": orEmptyS(m.Uses), "deps": []string{}, "open": o, "commits": modCommits[m.Name]})
	}

	// Calls into each route from Feign clients in this repository.
	callers := map[string]int{}
	var calls []map[string]any
	for _, t := range ix.Types {
		if t.Stereotype != "client" {
			continue
		}
		target := ""
		for _, m := range ix.Modules {
			if m.App == t.Feign || m.Name == t.Feign {
				target = m.Name
			}
		}
		var routes []string
		for _, e := range t.Endpoints {
			routes = append(routes, e.Verb+" "+e.Path)
			callers[target+" "+e.Verb+" "+e.Path]++
		}
		calls = append(calls, map[string]any{"from": t.Module, "to": orName(target, t.Feign), "client": t.Name, "node": t.ID, "routes": orEmptyS(routes)})
	}
	sort.Slice(calls, func(i, j int) bool {
		return calls[i]["from"].(string)+calls[i]["client"].(string) < calls[j]["from"].(string)+calls[j]["client"].(string)
	})
	endpoints := []map[string]any{}
	for _, t := range ix.Types {
		if t.Stereotype == "client" {
			continue
		}
		for _, e := range t.Endpoints {
			endpoints = append(endpoints, map[string]any{"service": t.Module, "route": e.Verb + " " + e.Path, "handler": e.Handler + "()",
				"class": t.Name, "node": t.ID, "callers": callers[t.Module+" "+e.Verb+" "+e.Path]})
		}
	}
	sort.Slice(endpoints, func(i, j int) bool {
		if endpoints[i]["service"] != endpoints[j]["service"] {
			return endpoints[i]["service"].(string) < endpoints[j]["service"].(string)
		}
		return endpoints[i]["route"].(string) < endpoints[j]["route"].(string)
	})

	contributors := []map[string]any{}
	for name, n := range a.repo.Authors(ref) {
		contributors = append(contributors, map[string]any{"name": name, "commits": n})
	}
	sort.Slice(contributors, func(i, j int) bool { return contributors[i]["commits"].(int) > contributors[j]["commits"].(int) })
	branch := a.repo.Branch()
	if a.viewing() {
		branch = ref
	}
	web.JSON(w, map[string]any{
		"name": base(a.repo.Root), "head": a.ixSHA, "branch": branch, "files": len(files), "javaFiles": javaFiles, "testFiles": testFiles,
		"lines": ix.Lines, "types": len(ix.Types), "methods": methods, "commits": a.repo.CountRef(ref), "contributors": contributors,
		"languages": langs, "roles": roles, "services": services, "endpoints": endpoints, "calls": orEmptyM(calls), "hotspots": orEmptyM(hots),
		"costly": orEmptyR(costly), "costlyTotal": costlyTotal, "largest": largest, "issues": issues, "urgency": map[string]int{}, "severity": sev,
	})
}

func orName(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func orEmptyM(xs []map[string]any) []map[string]any {
	if xs == nil {
		return []map[string]any{}
	}
	return xs
}

func orEmptyR(xs []repoMethod) []repoMethod {
	if xs == nil {
		return []repoMethod{}
	}
	return xs
}
