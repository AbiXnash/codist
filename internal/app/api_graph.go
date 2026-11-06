package app

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"codist/internal/java"
	"codist/internal/web"
)

type member struct {
	Name      string     `json:"name"`
	Kind      string     `json:"kind"` // method, constructor, field
	Signature string     `json:"signature"`
	StartLine int        `json:"startLine"`
	EndLine   int        `json:"endLine"`
	Value     string     `json:"value,omitempty"`
	Cost      *java.Cost `json:"cost,omitempty"`
}

type graphNode struct {
	ID          string   `json:"id"`
	FQN         string   `json:"fqn"`
	Module      string   `json:"module,omitempty"`
	FeignTarget string   `json:"feignTarget,omitempty"`
	Name        string   `json:"name"`
	Pkg         string   `json:"pkg"`
	Kind        string   `json:"kind"`
	Stereotype  string   `json:"stereotype"`
	File        string   `json:"file"`
	StartLine   int      `json:"startLine"`
	EndLine     int      `json:"endLine"`
	Annotations []string `json:"annotations"`
	Members     []member `json:"members"`
	Endpoints   []string `json:"endpoints"`
}

type graphEdge struct {
	Source string   `json:"source"`
	Target string   `json:"target"`
	Kind   string   `json:"kind"` // extends, implements, injects, uses, feign
	Via    []string `json:"via,omitempty"`
}

// role is the class role the interface draws: the index's Spring role, refined with the roles that
// have their own shape (DTO, exception, advice).
func role(t *java.Type) string {
	switch {
	case t.Stereotype == "controller" && hasAnn(t, "ControllerAdvice", "RestControllerAdvice"):
		return "advice"
	case t.Stereotype != "other" && t.Stereotype != "component":
		return t.Stereotype
	case strings.HasSuffix(t.Name, "Exception") || strings.HasSuffix(t.Name, "Error"):
		return "exception"
	case t.Kind == "record" || hasSuffix(t.Name, "Request", "Response", "Dto", "DTO", "Command", "Event", "View"):
		return "dto"
	}
	return t.Stereotype
}

func hasSuffix(s string, ends ...string) bool {
	for _, e := range ends {
		if strings.HasSuffix(s, e) {
			return true
		}
	}
	return false
}

func hasAnn(t *java.Type, names ...string) bool {
	for _, a := range t.Annotations {
		for _, n := range names {
			if strings.HasPrefix(a, "@"+n) {
				return true
			}
		}
	}
	return false
}

func nodeOf(t *java.Type) graphNode {
	n := graphNode{ID: t.ID, FQN: t.ID, Module: t.Module, FeignTarget: t.Feign, Name: t.Name, Pkg: t.Package, Kind: t.Kind,
		Stereotype: role(t), File: t.File, StartLine: t.Line, EndLine: t.End, Annotations: t.Annotations, Members: []member{}, Endpoints: []string{}}
	if n.Annotations == nil {
		n.Annotations = []string{}
	}
	for _, f := range t.Fields {
		n.Members = append(n.Members, member{Name: f.Name, Kind: "field", Signature: f.Type + " " + f.Name, StartLine: f.Line, EndLine: f.Line, Value: f.Value})
	}
	for i := range t.Methods {
		m := &t.Methods[i]
		kind := "method"
		if m.Name == t.Name {
			kind = "constructor"
		}
		var params []string
		for _, p := range m.Params {
			params = append(params, p.Type+" "+p.Name)
		}
		sig := strings.TrimSpace(m.Returns + " " + m.Name + "(" + strings.Join(params, ", ") + ")")
		cost := m.Cost
		n.Members = append(n.Members, member{Name: m.Name, Kind: kind, Signature: sig, StartLine: m.Line, EndLine: m.End, Cost: &cost})
	}
	for _, e := range t.Endpoints {
		n.Endpoints = append(n.Endpoints, e.Verb+" "+e.Path+"  → "+e.Handler)
	}
	return n
}

// graphEdges lists how classes depend on each other. Each pair keeps its strongest kind.
func graphEdges(ix *java.Index) []graphEdge {
	rank := map[string]int{"uses": 0, "injects": 1, "extends": 2, "implements": 2, "feign": 3}
	edges := map[[2]string]*graphEdge{}
	add := func(from, to, kind, via string) {
		if from == to {
			return
		}
		k := [2]string{from, to}
		e, ok := edges[k]
		if !ok {
			e = &graphEdge{Source: from, Target: to, Kind: kind}
			edges[k] = e
		} else if rank[kind] > rank[e.Kind] {
			e.Kind = kind
		}
		if via != "" {
			e.Via = append(e.Via, via)
		}
	}
	for _, t := range ix.Types {
		for _, f := range t.Fields {
			if o := ix.Lookup(f.Type, t); o != nil {
				add(t.ID, o.ID, "injects", "")
			}
		}
		for _, name := range t.Extends {
			if o := ix.Lookup(name, t); o != nil {
				kind := "extends"
				if o.Kind == "interface" && t.Kind != "interface" {
					kind = "implements"
				}
				add(t.ID, o.ID, kind, "")
			}
		}
	}
	for _, l := range ix.Links {
		add(strings.Split(l.From, "#")[0], strings.Split(l.To, "#")[0], "uses", "")
	}
	// A Feign client is linked to the controllers in the called service that serve its routes.
	served := map[string][]*java.Type{}
	for _, t := range ix.Types {
		if t.Stereotype == "controller" {
			for _, e := range t.Endpoints {
				served[t.Module+" "+e.Verb+" "+e.Path] = append(served[t.Module+" "+e.Verb+" "+e.Path], t)
			}
		}
	}
	for _, t := range ix.Types {
		if t.Stereotype != "client" || t.Feign == "" {
			continue
		}
		target := ""
		for _, m := range ix.Modules {
			if m.App == t.Feign || m.Name == t.Feign {
				target = m.Name
			}
		}
		for _, e := range t.Endpoints {
			for _, c := range served[target+" "+e.Verb+" "+e.Path] {
				add(t.ID, c.ID, "feign", e.Verb+" "+e.Path)
			}
		}
	}
	out := make([]graphEdge, 0, len(edges))
	for _, e := range edges {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Source+out[i].Target < out[j].Source+out[j].Target })
	return out
}

func (a *App) graph(w http.ResponseWriter, r *http.Request) {
	ix := a.index()
	nodes := make([]graphNode, 0, len(ix.Types))
	for _, t := range ix.Types {
		nodes = append(nodes, nodeOf(t))
	}
	web.JSON(w, map[string]any{"nodes": nodes, "edges": graphEdges(ix), "source": a.ixSHA, "builtAt": time.Now().UnixMilli(), "parseErrors": 0})
}

type callEnd struct {
	Class string `json:"class"`
	Name  string `json:"name"`
	File  string `json:"file"`
	Line  int    `json:"line"`
}

func (a *App) calls(w http.ResponseWriter, r *http.Request) {
	ix := a.index()
	end := func(ref string) (callEnd, bool) {
		typeID, name, ok := strings.Cut(ref, "#")
		t := ix.ByID[typeID]
		if !ok || t == nil || t.Method(name) == nil {
			return callEnd{}, false
		}
		return callEnd{Class: t.Name, Name: name, File: t.File, Line: t.Method(name).Line}, true
	}
	out := []map[string]any{}
	seen := map[string]bool{}
	for _, l := range ix.Links {
		if l.From == l.To || seen[l.From+">"+l.To] {
			continue
		}
		from, ok1 := end(l.From)
		to, ok2 := end(l.To)
		if ok1 && ok2 {
			seen[l.From+">"+l.To] = true
			out = append(out, map[string]any{"from": from, "to": to, "confirmed": true})
		}
	}
	web.JSON(w, out)
}
