package app

import (
	"net/http"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AbiXnash/codist-java"
	"github.com/AbiXnash/codist/internal/web"
)

// repoReport is the Repository page: the code's shape from the index, joined with git history and open issues.
func (a *App) repoReport(w http.ResponseWriter, r *http.Request) {
	ix, ref := a.index(), a.viewRef()
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
	moduleOf := java.ModuleOfFile(ix)
	openByFile, openByModule := map[string]int{}, map[string]map[string]int{}
	issues, sev := map[string]int{}, map[string]int{}
	for _, is := range a.store.AllIssues() {
		if is.State != "open" {
			continue
		}
		openByFile[is.Path]++
		if m := moduleOf[is.Path]; m != "" {
			if openByModule[m] == nil {
				openByModule[m] = map[string]int{}
			}
			openByModule[m][is.Latest.Severity]++
		}
		issues[kinds[category(is.Latest.Rule, is.Latest.Source)]]++
		sev[is.Latest.Severity]++
	}
	methods, roles := 0, map[string]int{}
	for _, t := range ix.Types {
		methods += len(t.Methods)
		roles[t.Stereotype]++
	}
	churn := a.repo.Churn(2000)
	moduleCommits := map[string]int{}
	for p, n := range churn {
		moduleCommits[moduleOf[p]] += n
	}
	services := []map[string]any{}
	for _, m := range ix.Modules {
		n := 0
		for _, f := range files {
			if m.Dir != "" && strings.HasPrefix(f, m.Dir+"/") {
				n++
			}
		}
		var port any
		if m.Port != "" {
			port = m.Port
		}
		open := openByModule[m.Name]
		if open == nil {
			open = map[string]int{}
		}
		services = append(services, map[string]any{"name": m.Name, "port": port, "kind": m.Kind, "files": n, "lines": m.Lines,
			"types": m.Types, "tests": m.Tests, "endpoints": m.Endpoints, "calls": orEmptyS(m.Calls), "calledBy": orEmptyS(m.CalledBy),
			"usesLibs": orEmptyS(m.Uses), "deps": []string{}, "open": open, "commits": moduleCommits[m.Name]})
	}
	calls, endpoints := java.Services(ix)
	costly, costlyTotal := java.Costly(ix, 15)
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
		"name": filepath.Base(a.repo.Root), "head": a.ixSHA, "branch": branch, "files": len(files), "javaFiles": javaFiles, "testFiles": testFiles,
		"lines": ix.Lines, "types": len(ix.Types), "methods": methods, "commits": a.repo.CountRef(ref), "contributors": contributors,
		"languages": langs, "roles": roles, "services": services, "endpoints": endpoints, "calls": calls,
		"hotspots": java.Hotspots(ix, churn, openByFile, 12), "costly": costly, "costlyTotal": costlyTotal, "largest": java.Largest(ix, 10),
		"issues": issues, "urgency": map[string]int{}, "severity": sev,
	})
}
