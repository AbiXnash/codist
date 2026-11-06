package app

import (
	"net/http"
	"sort"
	"strings"

	"codist/internal/java"
	"codist/internal/web"
)

// Graph data is in the shape Cytoscape reads: nodes and edges, each with a data map.
type gnode struct {
	Data map[string]any `json:"data"`
}

type graphData struct {
	Level string  `json:"level"`
	Title string  `json:"title"`
	Crumb []crumb `json:"crumb"`
	Nodes []gnode `json:"nodes"`
	Edges []gnode `json:"edges"`
	Note  string  `json:"note,omitempty"`
}

type crumb struct {
	Label, Href string
}

// layers is the order classes are grouped in, from the outside of a service inward.
var layers = []string{"controller", "service", "client", "repository", "entity", "component", "config", "other"}

var layerNames = map[string]string{"controller": "Controllers", "service": "Services", "client": "Feign clients",
	"repository": "Repositories", "entity": "Entities", "component": "Components", "config": "Configuration", "other": "Other"}

func (a *App) graphRoutes() {
	a.web.Handle("GET /graph", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		a.web.Render(w, r, "graph", "graph", "Graph", map[string]string{"Module": q.Get("module"), "Class": q.Get("class")})
	})
	a.web.Handle("GET /graph/data", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		ix := a.index()
		switch {
		case q.Get("class") != "":
			web.JSON(w, classGraph(ix, q.Get("class")))
		case q.Get("module") != "":
			web.JSON(w, moduleGraph(ix, q.Get("module")))
		default:
			web.JSON(w, serviceGraph(ix))
		}
	})
	a.web.Handle("GET /graph/info", func(w http.ResponseWriter, r *http.Request) {
		a.web.Fragment(w, "graph", "info", a.nodeInfo(r.URL.Query().Get("id")))
	})
}

func serviceGraph(ix *java.Index) graphData {
	g := graphData{Level: "services", Title: "Services", Crumb: []crumb{{"Services", "/graph"}}}
	for _, m := range ix.Modules {
		g.Nodes = append(g.Nodes, gnode{map[string]any{"id": "m:" + m.Name, "label": m.Name, "kind": m.Kind,
			"sub": plural(m.Types, "class") + ", " + plural(m.Endpoints, "route"), "href": "/graph?module=" + m.Name}})
		for _, to := range m.Calls {
			g.Edges = append(g.Edges, gnode{map[string]any{"id": m.Name + ">" + to, "source": "m:" + m.Name, "target": "m:" + to, "label": "calls", "kind": "calls"}})
		}
		for _, lib := range m.Uses {
			g.Edges = append(g.Edges, gnode{map[string]any{"id": m.Name + "~" + lib, "source": "m:" + m.Name, "target": "m:" + lib, "label": "uses", "kind": "uses"}})
		}
	}
	return g
}

func plural(n int, word string) string {
	s := ""
	if n != 1 {
		s = "es"
		if !strings.HasSuffix(word, "s") {
			s = "s"
		}
		if strings.HasSuffix(word, "ss") {
			s = "es"
		}
	}
	return itoa(n) + " " + word + s
}

// moduleGraph shows a module's classes in layer groups. Tests are left out: they call everything.
func moduleGraph(ix *java.Index, module string) graphData {
	g := graphData{Level: "module", Title: module, Crumb: []crumb{{"Services", "/graph"}, {module, "/graph?module=" + module}}}
	in := map[string]*java.Type{}
	used := map[string]bool{}
	for _, t := range ix.Types {
		if t.Module == module && t.Stereotype != "test" {
			in[t.ID] = t
		}
	}
	weight := map[[2]string]int{}
	for _, l := range ix.Links {
		from, to := strings.Split(l.From, "#")[0], strings.Split(l.To, "#")[0]
		if from != to && in[from] != nil && in[to] != nil {
			weight[[2]string{from, to}]++
			used[from], used[to] = true, true
		}
	}
	// A big module is unreadable as one picture: keep classes that call or are called by another,
	// and say how many were left out.
	shown := 0
	for _, layer := range layers {
		has := false
		for id, t := range in {
			if t.Stereotype == layer && (used[id] || len(in) <= 60) {
				has = true
				g.Nodes = append(g.Nodes, gnode{map[string]any{"id": id, "label": t.Name, "parent": "l:" + layer, "kind": layer,
					"sub": plural(len(t.Methods), "method"), "href": "/graph?class=" + id}})
				shown++
			}
		}
		if has {
			g.Nodes = append(g.Nodes, gnode{map[string]any{"id": "l:" + layer, "label": layerNames[layer], "kind": "layer"}})
		}
	}
	for k, n := range weight {
		g.Edges = append(g.Edges, gnode{map[string]any{"id": k[0] + ">" + k[1], "source": k[0], "target": k[1], "weight": n, "kind": "calls"}})
	}
	sort.Slice(g.Nodes, func(i, j int) bool { return g.Nodes[i].Data["id"].(string) < g.Nodes[j].Data["id"].(string) })
	if hidden := len(in) - shown; hidden > 0 {
		g.Note = plural(hidden, "class") + " with no calls to or from other classes here are not drawn."
	}
	return g
}

// classGraph shows one class's methods with what they call and what calls them.
func classGraph(ix *java.Index, id string) graphData {
	t := ix.ByID[id]
	if t == nil {
		return graphData{Level: "class", Title: id, Note: "This class is not in the current code."}
	}
	g := graphData{Level: "class", Title: t.Name, Crumb: []crumb{{"Services", "/graph"}, {t.Module, "/graph?module=" + t.Module}, {t.Name, "/graph?class=" + id}}}
	nodes := map[string]bool{}
	add := func(nid, label, kind, parent, href string) {
		if nodes[nid] {
			return
		}
		nodes[nid] = true
		d := map[string]any{"id": nid, "label": label, "kind": kind}
		if parent != "" {
			d["parent"] = parent
		}
		if href != "" {
			d["href"] = href
		}
		g.Nodes = append(g.Nodes, gnode{d})
	}
	add("c:"+t.ID, t.Name, "layer", "", "")
	for _, m := range t.Methods {
		add(t.ID+"#"+m.Name, m.Name+"()", "method", "c:"+t.ID, "")
	}
	other := func(ref string) (string, string) {
		typeID := strings.Split(ref, "#")[0]
		ot := ix.ByID[typeID]
		if ot == nil {
			return "", ""
		}
		label := ot.Name
		if i := strings.Index(ref, "#"); i >= 0 {
			label += "." + ref[i+1:] + "()"
		}
		return label, "/graph?class=" + typeID
	}
	seen := map[string]bool{}
	for _, l := range ix.Links {
		fromType, toType := strings.Split(l.From, "#")[0], strings.Split(l.To, "#")[0]
		switch {
		case fromType == t.ID && toType != t.ID:
			label, href := other(l.To)
			add(l.To, label, ix.ByID[toType].Stereotype, "", href)
		case toType == t.ID && fromType != t.ID:
			if ft := ix.ByID[fromType]; ft == nil || ft.Stereotype == "test" {
				continue
			}
			label, href := other(l.From)
			add(l.From, label, ix.ByID[fromType].Stereotype, "", href)
		case fromType == t.ID && toType == t.ID && l.From != l.To:
		default:
			continue
		}
		key := l.From + ">" + l.To
		if seen[key] || !nodes[l.From] || !nodes[l.To] {
			continue
		}
		seen[key] = true
		g.Edges = append(g.Edges, gnode{map[string]any{"id": key, "source": l.From, "target": l.To, "kind": "calls"}})
	}
	return g
}

type info struct {
	Title, Kind, File, Note string
	Line                    int
	Facts                   [][2]string
	Lists                   []infoList
}

type infoList struct {
	Title string
	Items []string
}

// nodeInfo describes a clicked node in the side panel.
func (a *App) nodeInfo(id string) info {
	ix := a.index()
	switch {
	case strings.HasPrefix(id, "m:"):
		m := ix.Module(strings.TrimPrefix(id, "m:"))
		if m == nil {
			break
		}
		port := m.Port
		if port == "" {
			port = "not set"
		}
		return info{Title: m.Name, Kind: m.Kind, Facts: [][2]string{{"Classes", itoa(m.Types)}, {"Lines", itoa(m.Lines)}, {"Test classes", itoa(m.Tests)},
			{"Routes", itoa(m.Endpoints)}, {"Port", port}}, Lists: []infoList{{"Calls", m.Calls}, {"Called by", m.CalledBy}, {"Uses", m.Uses}}}
	case strings.Contains(id, "#"):
		typeID, name, _ := strings.Cut(id, "#")
		t := ix.ByID[typeID]
		if t == nil || t.Method(name) == nil {
			break
		}
		m := t.Method(name)
		var callers, calls []string
		for _, l := range ix.Callers(id) {
			callers = append(callers, shortRef(ix, l.From))
		}
		for _, l := range ix.Links {
			if l.From == id && l.To != id {
				calls = append(calls, shortRef(ix, l.To))
			}
		}
		return info{Title: t.Name + "." + m.Name + "()", Kind: "method", File: t.File, Line: m.Line,
			Facts: [][2]string{{"Lines", itoa(m.Cost.Lines)}, {"Paths", itoa(m.Cost.Complexity)}, {"Work as data grows", m.Cost.Estimate}},
			Lists: []infoList{{"Called by", dedupe(callers)}, {"Calls", dedupe(calls)}}}
	default:
		t := ix.ByID[strings.TrimPrefix(id, "c:")]
		if t == nil {
			break
		}
		var routes []string
		for _, e := range t.Endpoints {
			routes = append(routes, e.Verb+" "+e.Path)
		}
		facts := [][2]string{{"Module", t.Module}, {"Role", t.Stereotype}, {"Lines", itoa(t.Lines())}, {"Methods", itoa(len(t.Methods))}}
		if t.Feign != "" {
			facts = append(facts, [2]string{"Calls service", t.Feign})
		}
		return info{Title: t.Name, Kind: t.Kind, File: t.File, Line: t.Line, Facts: facts, Lists: []infoList{{"Routes", routes}}}
	}
	return info{Title: "Not found", Note: "This is no longer in the code."}
}

func shortRef(ix *java.Index, ref string) string {
	typeID, name, hasMethod := strings.Cut(ref, "#")
	label := typeID
	if t := ix.ByID[typeID]; t != nil {
		label = t.Name
	}
	if hasMethod {
		label += "." + name + "()"
	}
	return label
}

func dedupe(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}
