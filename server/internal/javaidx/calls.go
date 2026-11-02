package javaidx

import (
	"path/filepath"
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// Method is one method in the call graph. Overloads share an entry, because a call is matched by name.
type Method struct {
	Class     string   `json:"class"`
	Name      string   `json:"name"`
	File      string   `json:"file"`
	Line      int      `json:"line"`
	EndLine   int      `json:"-"`
	Signature string   `json:"signature"`
	Test      bool     `json:"-"`
	Routes    []string `json:"routes,omitempty"` // REST routes this method handles
	key       string
}

// Reached is a method that can be affected, how far away it is and how sure the link is.
type Reached struct {
	Method    *Method
	Depth     int
	Confirmed bool
}

// CallGraph records who calls what, from syntax and declared types. It cannot see reflection, events or
// configuration, so a complete-looking answer can still miss a caller.
type CallGraph struct {
	methods map[string]*Method
	callers map[string]map[string]bool // target -> caller -> the receiver's type was known
	related map[string][]string        // class -> classes above and below it in the hierarchy
}

func moduleOfFile(path string) string {
	if i := strings.Index(path, "/src/"); i > 0 {
		return path[:i]
	}
	return ""
}

func isTestFile(path string) bool {
	base := filepath.Base(path)
	return strings.Contains("/"+path, "/src/test/") || strings.HasSuffix(base, "Test.java") || strings.HasSuffix(base, "Tests.java")
}

type classDecl struct {
	f       *file
	node    *sitter.Node
	name    string
	module  string
	path    string
	fields  map[string]string
	methods []*sitter.Node
	supers  []string
}

func methodKey(module, class, name string) string { return module + "|" + class + "#" + name }
func classKey(module, class string) string        { return module + "|" + class }

// declaredVariables is the variables a method can see beyond its class's fields, with their types.
func declaredVariables(f *file, m *sitter.Node) map[string]string {
	vars := map[string]string{}
	for _, p := range findAll(m, "formal_parameter") {
		vars[f.text(p.ChildByFieldName("name"))] = f.baseType(p.ChildByFieldName("type"))
	}
	for _, d := range findAll(m, "local_variable_declaration") {
		t := f.baseType(d.ChildByFieldName("type"))
		for _, v := range findAll(d, "variable_declarator") {
			kind := t
			if value := v.ChildByFieldName("value"); kind == "" && value != nil && value.Type() == "object_creation_expression" {
				kind = f.baseType(value.ChildByFieldName("type"))
			}
			vars[f.text(v.ChildByFieldName("name"))] = kind
		}
	}
	for _, d := range findAll(m, "enhanced_for_statement") {
		vars[f.text(d.ChildByFieldName("name"))] = f.baseType(d.ChildByFieldName("type"))
	}
	return vars
}

// BuildCallGraph reads every source file and links method calls to the methods they reach.
func BuildCallGraph(sources map[string][]byte, g *Graph) *CallGraph {
	cg := &CallGraph{methods: map[string]*Method{}, callers: map[string]map[string]bool{}, related: map[string][]string{}}
	var decls []*classDecl
	byName := map[string][]*classDecl{}
	byMethodName := map[string][]string{}
	paths := make([]string, 0, len(sources))
	for p := range sources {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, path := range paths {
		f, err := parse(sources[path])
		if err != nil {
			continue
		}
		for typ := range typeDecls {
			for _, n := range findAll(f.root, typ) {
				d := &classDecl{f: f, node: n, name: f.text(n.ChildByFieldName("name")), module: moduleOfFile(path), path: path, fields: map[string]string{}}
				for _, t := range typeList(n.ChildByFieldName("superclass")) {
					d.supers = append(d.supers, f.baseType(t))
				}
				for _, t := range typeList(childOfType(n, "extends_interfaces")) {
					d.supers = append(d.supers, f.baseType(t))
				}
				for _, t := range typeList(n.ChildByFieldName("interfaces")) {
					d.supers = append(d.supers, f.baseType(t))
				}
				body := n.ChildByFieldName("body")
				for _, c := range named(body) {
					switch c.Type() {
					case "field_declaration":
						t := f.baseType(c.ChildByFieldName("type"))
						for _, v := range findAll(c, "variable_declarator") {
							d.fields[f.text(v.ChildByFieldName("name"))] = t
						}
					case "method_declaration":
						d.methods = append(d.methods, c)
					}
				}
				if params := n.ChildByFieldName("parameters"); params != nil { // record components
					for _, p := range named(params) {
						d.fields[f.text(p.ChildByFieldName("name"))] = f.baseType(p.ChildByFieldName("type"))
					}
				}
				decls = append(decls, d)
				byName[d.name] = append(byName[d.name], d)
				for _, m := range d.methods {
					name := f.text(m.ChildByFieldName("name"))
					key := methodKey(d.module, d.name, name)
					if cg.methods[key] == nil {
						cg.methods[key] = &Method{Class: d.name, Name: name, File: path, Line: startLine(m), EndLine: endLine(m),
							Signature: f.signature(m), Test: isTestFile(path), key: key}
						byMethodName[name] = append(byMethodName[name], key)
					} else if end := endLine(m); end > cg.methods[key].EndLine {
						cg.methods[key].EndLine = end // an overload further down the file
					}
				}
			}
		}
	}

	// A type name means the one in the same module; otherwise any module's, less certainly if several.
	resolve := func(module, name string) (found []*classDecl, sure bool) {
		for _, d := range byName[name] {
			if d.module == module {
				return []*classDecl{d}, true
			}
		}
		return byName[name], len(byName[name]) == 1
	}
	for _, d := range decls {
		for _, s := range d.supers {
			found, _ := resolve(d.module, s)
			for _, up := range found {
				a, b := classKey(d.module, d.name), classKey(up.module, up.name)
				cg.related[a] = append(cg.related[a], b)
				cg.related[b] = append(cg.related[b], a)
			}
		}
	}
	link := func(target, caller string, sure bool) {
		if target == caller {
			return
		}
		if cg.callers[target] == nil {
			cg.callers[target] = map[string]bool{}
		}
		cg.callers[target][caller] = cg.callers[target][caller] || sure
	}
	for _, d := range decls {
		f := d.f
		for _, m := range d.methods {
			caller := methodKey(d.module, d.name, f.text(m.ChildByFieldName("name")))
			vars := declaredVariables(f, m)
			for _, call := range findAll(m, "method_invocation") {
				name := f.text(call.ChildByFieldName("name"))
				object := call.ChildByFieldName("object")
				class, known := "", false
				switch {
				case object == nil || object.Type() == "this" || object.Type() == "super":
					class, known = d.name, true
				case object.Type() == "identifier":
					id := f.text(object)
					if t, ok := vars[id]; ok {
						class, known = t, t != ""
					} else if t, ok := d.fields[id]; ok {
						class, known = t, t != ""
					} else if id != "" && id[0] >= 'A' && id[0] <= 'Z' {
						class, known = id, true // a static call
					}
				case object.Type() == "field_access" && f.text(object.ChildByFieldName("object")) == "this":
					id := f.text(object.ChildByFieldName("field"))
					class, known = d.fields[id], d.fields[id] != ""
				}
				if known {
					found, sure := resolve(d.module, class)
					for _, c := range found {
						link(methodKey(c.module, c.name, name), caller, sure)
					}
					continue // a known type that is not ours, such as List, is not a project call
				}
				// Unknown receiver, such as a chained call: a name only one method has is a fair guess.
				if keys := byMethodName[name]; len(keys) == 1 {
					link(keys[0], caller, false)
				}
			}
		}
	}

	if g != nil {
		cg.linkRoutes(g)
	}
	return cg
}

// linkRoutes records REST routes, and treats a Feign client method as a caller of the handler it reaches.
func (cg *CallGraph) linkRoutes(g *Graph) {
	nodes := map[string]Node{}
	handler := func(n Node, ep string) *Method {
		_, h, _ := strings.Cut(ep, "→")
		return cg.methods[methodKey(moduleOfFile(n.File), n.Name, strings.TrimSuffix(strings.TrimSpace(h), "()"))]
	}
	for _, n := range g.Nodes {
		nodes[n.ID] = n
		for _, ep := range n.Endpoints {
			if m := handler(n, ep); m != nil && n.FeignTarget == "" {
				m.Routes = append(m.Routes, strings.TrimSpace(strings.SplitN(ep, "  →", 2)[0]))
			}
		}
	}
	for _, e := range g.Edges {
		if e.Kind != "feign" {
			continue
		}
		client, server := nodes[e.Source], nodes[e.Target]
		for _, ce := range client.Endpoints {
			for _, se := range server.Endpoints {
				if routeOf(ce) != routeOf(se) {
					continue
				}
				from, to := handler(client, ce), handler(server, se)
				if from != nil && to != nil {
					if cg.callers[to.key] == nil {
						cg.callers[to.key] = map[string]bool{}
					}
					cg.callers[to.key][from.key] = true
				}
			}
		}
	}
}

// MethodsIn is the methods of a file that contain any of the lines.
func (cg *CallGraph) MethodsIn(path string, lines []int) []*Method {
	var out []*Method
	for _, m := range cg.methods {
		if m.File != path {
			continue
		}
		for _, l := range lines {
			if l >= m.Line && l <= m.EndLine {
				out = append(out, m)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out
}

// Method looks one up by class and name in a file's module.
func (cg *CallGraph) Method(path, class, name string) *Method {
	return cg.methods[methodKey(moduleOfFile(path), class, name)]
}

// sameMethod is the method itself plus its overrides and the declarations it implements: calling the
// interface can run the implementation.
func (cg *CallGraph) sameMethod(key string) []string {
	head, name, ok := strings.Cut(key, "#")
	if !ok {
		return []string{key}
	}
	out := []string{key}
	seen := map[string]bool{head: true}
	queue := []string{head}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		for _, r := range cg.related[c] {
			if !seen[r] {
				seen[r] = true
				queue = append(queue, r)
				out = append(out, r+"#"+name)
			}
		}
	}
	return out
}

// CallersOf walks up from a method, up to depth levels. A link is confirmed only if every step is.
func (cg *CallGraph) CallersOf(path, class, name string, depth int) []Reached {
	start := methodKey(moduleOfFile(path), class, name)
	type step struct {
		key   string
		depth int
		sure  bool
	}
	best := map[string]Reached{}
	queue := []step{{start, 0, true}}
	visited := map[string]bool{start + "|true": true}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.depth >= depth {
			continue
		}
		for _, target := range cg.sameMethod(cur.key) {
			for caller, sure := range cg.callers[target] {
				m := cg.methods[caller]
				if m == nil || caller == start {
					continue
				}
				all := cur.sure && sure
				next := Reached{Method: m, Depth: cur.depth + 1, Confirmed: all}
				if old, ok := best[caller]; !ok || (all && !old.Confirmed) || (all == old.Confirmed && next.Depth < old.Depth) {
					best[caller] = next
				}
				mark := caller + "|" + map[bool]string{true: "true", false: "false"}[all]
				if !visited[mark] {
					visited[mark] = true
					queue = append(queue, step{caller, cur.depth + 1, all})
				}
			}
		}
	}
	out := make([]Reached, 0, len(best))
	for _, r := range best {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Depth != b.Depth {
			return a.Depth < b.Depth
		}
		return a.Method.key < b.Method.key
	})
	return out
}

// TopLevelMethods lists a source file's methods with their signatures, by class.
func TopLevelMethods(src []byte) map[string]string {
	out := map[string]string{}
	f, err := parse(src)
	if err != nil {
		return out
	}
	for typ := range typeDecls {
		for _, n := range findAll(f.root, typ) {
			class := f.text(n.ChildByFieldName("name"))
			for _, c := range named(n.ChildByFieldName("body")) {
				if c.Type() == "method_declaration" {
					out[class+"#"+f.text(c.ChildByFieldName("name"))] = collapse(f.signature(c))
				}
			}
		}
	}
	return out
}
