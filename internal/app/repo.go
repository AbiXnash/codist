package app

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"codist/internal/java"
)

type repoPage struct {
	Modules             []*java.Module
	Services, Libraries int
	Types, Methods      int
	Lines, Files        int
	Endpoints, Commits  int
	Hotspots            []hotspot
	Largest             []*java.Type
	Costly              []costly
	CostlyTotal         int
	Clients             []client
	Languages           string
}

type hotspot struct {
	Path    string
	Name    string
	Commits int
	Open    int
	Lines   int
}

type costly struct {
	Type   *java.Type
	Method java.Method
	Why    string
}

type client struct {
	From, To, Class string
	Routes          []string
}

type endpointRow struct {
	Module, Verb, Path, Class, Handler, File string
	Line, Callers                            int
}

const endpointBatch = 60

func (a *App) repoRoutes() {
	a.web.Handle("GET /repo", func(w http.ResponseWriter, r *http.Request) {
		a.web.Render(w, r, "repo", "repo", "Repository", a.repoReport())
	})
	a.web.Handle("GET /repo/endpoints", func(w http.ResponseWriter, r *http.Request) {
		q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
		from, _ := strconv.Atoi(r.URL.Query().Get("from"))
		rows := a.endpoints(q)
		page := rows[min(from, len(rows)):min(from+endpointBatch, len(rows))]
		next := from + len(page)
		a.web.Fragment(w, "repo", "endpoint-rows", map[string]any{
			"Rows": page, "Next": next, "More": next < len(rows), "Q": q, "Total": len(rows), "First": from == 0,
		})
	})
}

func (a *App) repoReport() repoPage {
	ix := a.index()
	p := repoPage{Modules: ix.Modules, Types: len(ix.Types), Lines: ix.Lines, Files: ix.Files, Commits: a.repo.Count()}
	for _, m := range ix.Modules {
		if m.Kind == "service" {
			p.Services++
		} else {
			p.Libraries++
		}
		p.Endpoints += m.Endpoints
	}
	lines := map[string]int{}
	for _, t := range ix.Types {
		p.Methods += len(t.Methods)
		if t.End > lines[t.File] {
			lines[t.File] = t.End
		}
		for _, m := range t.Methods {
			if why := whyCostly(m.Cost); why != "" {
				p.Costly = append(p.Costly, costly{Type: t, Method: m, Why: why})
			}
		}
		if t.Stereotype == "client" {
			c := client{From: t.Module, To: t.Feign, Class: t.Name}
			for _, e := range t.Endpoints {
				c.Routes = append(c.Routes, e.Verb+" "+e.Path)
			}
			p.Clients = append(p.Clients, c)
		}
	}
	sort.Slice(p.Costly, func(i, j int) bool {
		x, y := p.Costly[i].Method.Cost, p.Costly[j].Method.Cost
		if x.LoopIO != y.LoopIO {
			return x.LoopIO > y.LoopIO
		}
		if x.LoopDepth != y.LoopDepth {
			return x.LoopDepth > y.LoopDepth
		}
		return x.Complexity > y.Complexity
	})
	p.CostlyTotal = len(p.Costly)
	p.Costly = p.Costly[:min(15, len(p.Costly))]
	sort.Slice(p.Clients, func(i, j int) bool {
		return p.Clients[i].From+p.Clients[i].Class < p.Clients[j].From+p.Clients[j].Class
	})

	open := a.openIssuesByFile()
	for path, n := range a.repo.Churn(2000) {
		if l, ok := lines[path]; ok {
			p.Hotspots = append(p.Hotspots, hotspot{Path: path, Name: path[strings.LastIndex(path, "/")+1:], Commits: n, Open: open[path], Lines: l})
		}
	}
	sort.Slice(p.Hotspots, func(i, j int) bool {
		x, y := p.Hotspots[i], p.Hotspots[j]
		if x.Commits*(x.Open+1) != y.Commits*(y.Open+1) {
			return x.Commits*(x.Open+1) > y.Commits*(y.Open+1)
		}
		return x.Path < y.Path
	})
	p.Hotspots = p.Hotspots[:min(12, len(p.Hotspots))]

	p.Largest = append([]*java.Type(nil), ix.Types...)
	sort.Slice(p.Largest, func(i, j int) bool { return p.Largest[i].Lines() > p.Largest[j].Lines() })
	p.Largest = p.Largest[:min(10, len(p.Largest))]
	return p
}

// whyCostly says in words why a method is listed, or "" if it is not.
func whyCostly(c java.Cost) string {
	var why []string
	if c.LoopIO > 0 {
		s := "s"
		if c.LoopIO == 1 {
			s = ""
		}
		why = append(why, strconv.Itoa(c.LoopIO)+" slow call"+s+" repeated in a loop")
	}
	if c.LoopDepth >= 2 {
		why = append(why, "loops nested "+strconv.Itoa(c.LoopDepth)+" deep")
	}
	if c.Complexity >= 8 {
		why = append(why, strconv.Itoa(c.Complexity)+" paths through the code")
	}
	return strings.Join(why, ", ")
}

func (a *App) endpoints(q string) []endpointRow {
	ix := a.index()
	var rows []endpointRow
	for _, t := range ix.Types {
		if t.Stereotype == "client" {
			continue
		}
		for _, e := range t.Endpoints {
			row := endpointRow{Module: t.Module, Verb: e.Verb, Path: e.Path, Class: t.Name, Handler: e.Handler, File: t.File, Line: e.Line}
			if q != "" && !strings.Contains(strings.ToLower(row.Module+" "+row.Verb+" "+row.Path+" "+row.Class+" "+row.Handler), q) {
				continue
			}
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Module != rows[j].Module {
			return rows[i].Module < rows[j].Module
		}
		return rows[i].Path < rows[j].Path
	})
	return rows
}

// openIssuesByFile is filled in once reviews exist.
func (a *App) openIssuesByFile() map[string]int {
	if a.store == nil {
		return map[string]int{}
	}
	return a.store.OpenByFile()
}
