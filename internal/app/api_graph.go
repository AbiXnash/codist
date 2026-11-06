package app

import (
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
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

// role is a class's role as the interface draws it.
func role(t *java.Type) string { return t.Stereotype }

func nodeOf(t *java.Type) graphNode {
	kind := t.Kind
	if t.Abstract {
		kind = "abstract"
	}
	n := graphNode{ID: t.ID, FQN: t.ID, Module: t.Module, FeignTarget: t.Feign, Name: t.Name, Pkg: t.Package, Kind: kind,
		Stereotype: t.Stereotype, File: t.File, StartLine: t.Line, EndLine: t.End, Annotations: []string{}, Members: []member{}, Endpoints: []string{}}
	for _, a := range t.Annotations {
		n.Annotations = append(n.Annotations, java.AnnotationName(a))
	}
	// Members in the order they are written: fields declared together are one member.
	type item struct {
		line int
		m    member
	}
	var items []item
	seen := map[int]bool{}
	for _, f := range t.Fields {
		if f.Component || seen[f.Line] {
			continue
		}
		seen[f.Line] = true
		sig := f.Signature
		if sig == "" {
			sig = f.Type + " " + f.Name
		}
		names := f.Names
		if names == "" {
			names = f.Name
		}
		items = append(items, item{f.Line, member{Name: names, Kind: "field", Signature: sig, StartLine: f.Line, EndLine: max(f.End, f.Line), Value: f.Value}})
	}
	for i := range t.Methods {
		m := &t.Methods[i]
		kind := "method"
		if m.Name == t.Name {
			kind = "constructor"
		}
		cost := m.Cost
		items = append(items, item{m.Line, member{Name: m.Name, Kind: kind, Signature: m.Signature, StartLine: m.Line, EndLine: m.End, Cost: &cost}})
	}
	sort.SliceStable(items, func(a, b int) bool { return items[a].line < items[b].line })
	for _, it := range items {
		n.Members = append(n.Members, it.m)
	}
	for _, e := range t.Endpoints {
		n.Endpoints = append(n.Endpoints, e.Verb+" "+e.Path+"  → "+e.Handler+"()")
	}
	sort.Strings(n.Endpoints)
	return n
}

// edgeRank orders link kinds: a pair of classes keeps the strongest (lowest) one.
var edgeRank = map[string]int{"feign": -1, "extends": 0, "implements": 1, "injects": 2, "uses": 3}

// graphEdges lists how classes depend on each other, built the way the first Codist built them.
func graphEdges(ix *java.Index) []graphEdge {
	edges := map[[2]string]*graphEdge{}
	add := func(from, to, kind string) {
		if to == "" || to == from || strings.HasPrefix(to, from+".") || strings.HasPrefix(from, to+".") {
			return
		}
		k := [2]string{from, to}
		if e, ok := edges[k]; !ok || edgeRank[kind] < edgeRank[e.Kind] {
			edges[k] = &graphEdge{Source: from, Target: to, Kind: kind}
		}
	}
	for _, t := range ix.Types {
		for _, b := range t.Extends {
			add(t.ID, ix.Resolve(b, t), "extends")
		}
		for _, b := range t.Implements {
			add(t.ID, ix.Resolve(b, t), "implements")
		}
		for _, b := range t.Injected {
			add(t.ID, ix.Resolve(b, t), "injects")
		}
		for _, b := range t.Uses {
			add(t.ID, ix.Resolve(b, t), "uses")
		}
	}
	// A Feign client reaches the controllers of the service it names that serve one of its routes.
	// Variable names in paths are ignored: {code} on one side matches {id} on the other.
	for _, c := range ix.Types {
		if c.Feign == "" {
			continue
		}
		for _, t := range ix.Types {
			if t.Stereotype != "controller" || !strings.EqualFold(t.Module, c.Feign) {
				continue
			}
			var via []string
			for _, ce := range c.Endpoints {
				for _, te := range t.Endpoints {
					if routeKey(ce) == routeKey(te) {
						via = append(via, routeKey(ce))
					}
				}
			}
			if len(via) > 0 {
				edges[[2]string{c.ID, t.ID}] = &graphEdge{Source: c.ID, Target: t.ID, Kind: "feign", Via: dedupeStrings(via)}
			}
		}
	}
	out := make([]graphEdge, 0, len(edges))
	for _, e := range edges {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Source+"->"+out[i].Target < out[j].Source+"->"+out[j].Target })
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

// callsCache keeps the method calls of the current index; finding them parses every file again.
var callsCache struct {
	sync.Mutex
	sha   string
	calls []java.MethodCall
}

func (a *App) calls(w http.ResponseWriter, r *http.Request) {
	ix := a.index()
	callsCache.Lock()
	if callsCache.sha != a.ixSHA || callsCache.calls == nil {
		callsCache.sha, callsCache.calls = a.ixSHA, java.BuildCallGraph(ix.Sources, ix.Types).Calls()
	}
	calls := callsCache.calls
	callsCache.Unlock()
	web.JSON(w, calls)
}

var pathVar = regexp.MustCompile(`\{[^}]*\}`)

// routeKey is an endpoint's route with variable names ignored: "GET /customers/{}".
func routeKey(e java.Endpoint) string {
	return e.Verb + " " + pathVar.ReplaceAllString(e.Path, "{}")
}

func dedupeStrings(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
