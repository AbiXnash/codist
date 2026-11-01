package javaidx

import (
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	sitter "github.com/smacker/go-tree-sitter"
)

var mappingAnnotations = map[string]bool{
	"RequestMapping": true, "GetMapping": true, "PostMapping": true, "PutMapping": true, "DeleteMapping": true, "PatchMapping": true,
}

var repoBase = regexp.MustCompile(`^((Jpa|Crud|PagingAndSorting|ListCrud|ListPagingAndSorting|Mongo|Reactive\w*|R2dbc)Repository|Repository)$`)

var edgeRank = map[string]int{"extends": 0, "implements": 1, "injects": 2, "uses": 3}

type typeDecl struct {
	node *sitter.Node
	fqn  string
	name string
	unit *unit
}

type unit struct {
	path    string
	f       *file
	pkg     string
	imports map[string]string // simple name -> fqn
	stars   []string          // on-demand import packages
}

// Index builds the graph for a set of sources keyed by repo-relative path.
func Index(sources map[string][]byte, label string) Graph {
	paths := make([]string, 0, len(sources))
	for p := range sources {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	// Parse in parallel; tree-sitter parsers are per goroutine.
	units := make([]*unit, len(paths))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for i, p := range paths {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, p string) {
			defer wg.Done()
			defer func() { <-sem }()
			if f, err := parse(sources[p]); err == nil {
				units[i] = newUnit(p, f)
			}
		}(i, p)
	}
	wg.Wait()

	errors := 0
	var decls []typeDecl
	known := map[string]bool{}
	bySimple := map[string][]string{}
	declared := map[string][]typeDecl{}
	for _, u := range units {
		if u == nil {
			errors++
			continue
		}
		if u.f.root.HasError() {
			errors++
		}
		for _, d := range u.types() {
			decls = append(decls, d)
			if !known[d.fqn] {
				bySimple[d.name] = append(bySimple[d.name], d.fqn)
			}
			known[d.fqn] = true
			declared[d.fqn] = append(declared[d.fqn], d)
		}
	}
	ids := assignIDs(declared)

	g := Graph{Nodes: []Node{}, Edges: []Edge{}, Source: label, BuiltAt: time.Now().UnixMilli(), ParseErrors: errors}
	edges := map[string]Edge{}
	add := func(from, to, kind string) {
		if to == "" || to == from || strings.HasPrefix(to, from+".") || strings.HasPrefix(from, to+".") {
			return
		}
		key := from + "->" + to
		if e, ok := edges[key]; !ok || edgeRank[kind] < edgeRank[e.Kind] {
			edges[key] = Edge{Source: from, Target: to, Kind: kind}
		}
	}

	for _, d := range decls {
		u, f := d.unit, d.unit.f
		res := resolver{u: u, known: known, bySimple: bySimple}
		self := ids[declKey(d)]
		if self == "" { // a second declaration of the same name in one file: mid-edit code
			continue
		}
		delete(ids, declKey(d))
		node := u.node(d)
		node.ID = self
		g.Nodes = append(g.Nodes, node)
		link := func(simple, kind string) {
			add(self, pickDeclaration(res.resolve(simple), u, declared), kind)
		}

		for _, t := range typeList(d.node.ChildByFieldName("superclass")) {
			link(f.baseType(t), "extends")
		}
		for _, t := range typeList(childOfType(d.node, "extends_interfaces")) {
			link(f.baseType(t), "extends")
		}
		for _, t := range typeList(d.node.ChildByFieldName("interfaces")) {
			link(f.baseType(t), "implements")
		}
		for _, name := range u.injected(d.node) {
			link(name, "injects")
		}
		walk(d.node, func(n *sitter.Node) bool {
			switch n.Type() {
			case "type_identifier":
				link(f.text(n), "uses")
			case "method_invocation", "field_access":
				if obj := n.ChildByFieldName("object"); obj != nil && obj.Type() == "identifier" && isUpper(f.text(obj)) {
					link(f.text(obj), "uses") // static call or constant
				}
			}
			return true
		})
	}

	keys := make([]string, 0, len(edges))
	for k := range edges {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		g.Edges = append(g.Edges, edges[k])
	}
	return g
}

func declKey(d typeDecl) string { return d.unit.path + "\x00" + d.fqn }

// assignIDs gives every declaration its node ID, keyed by declKey.
func assignIDs(declared map[string][]typeDecl) map[string]string {
	ids := map[string]string{}
	for _, ds := range declared {
		for _, d := range ds {
			ids[declKey(d)] = declID(d, ds)
		}
	}
	return ids
}

// declID is one declaration's node ID, given all declarations of its name. A name declared once
// keeps its plain FQN, so single-module projects see no change. Clashes get the module as a
// prefix, or the file path when one module declares the name twice (say, in main and test).
func declID(d typeDecl, all []typeDecl) string {
	if len(all) == 1 {
		return d.fqn
	}
	module := ModuleOf(d.unit.path)
	inModule := 0
	for _, o := range all {
		if ModuleOf(o.unit.path) == module {
			inModule++
		}
	}
	if inModule > 1 {
		return d.unit.path + ":" + d.fqn
	}
	if module == "" {
		module = "(root)"
	}
	return module + ":" + d.fqn
}

// pickDeclaration returns the ID of the declaration of fqn that a reference from u means: the one
// in u's own module and, failing a unique match, u's own source root. A clashing name with no
// match is ambiguous, because module dependencies are not known here, so no link is drawn rather
// than a wrong one.
func pickDeclaration(fqn string, u *unit, declared map[string][]typeDecl) string {
	all := declared[fqn]
	if len(all) == 0 {
		return ""
	}
	if len(all) == 1 {
		return declID(all[0], all)
	}
	module, root := ModuleOf(u.path), sourceRoot(u.path)
	var inModule, inRoot []typeDecl
	for _, d := range all {
		if ModuleOf(d.unit.path) == module {
			inModule = append(inModule, d)
			if sourceRoot(d.unit.path) == root {
				inRoot = append(inRoot, d)
			}
		}
	}
	switch {
	case len(inModule) == 1:
		return declID(inModule[0], all)
	case len(inRoot) == 1:
		return declID(inRoot[0], all)
	}
	return ""
}

// ModuleOf returns the build module a source file belongs to: the directory holding its src/
// folder ("rest" for rest/src/main/java/...). It is empty for a root-level project.
func ModuleOf(path string) string {
	if strings.HasPrefix(path, "src/") {
		return ""
	}
	if i := strings.LastIndex(path, "/src/"); i >= 0 {
		return path[:i]
	}
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[:i]
	}
	return ""
}

// sourceRoot is the part of the path before the package directories: src/main or src/test.
func sourceRoot(path string) string {
	i := strings.Index(path, "src/")
	if i < 0 {
		return ""
	}
	rest := path[i+len("src/"):]
	if j := strings.Index(rest, "/"); j >= 0 {
		return rest[:j]
	}
	return ""
}

// Qualify rewrites the IDs of types parsed from a single file (see TypesIn) to match this graph,
// so a type from one module never picks up another module's links. A type the graph no longer
// holds keeps its plain name, unless another module owns that name: then it gets a qualified ID
// that links to nothing.
func (g *Graph) Qualify(types []Node) []Node {
	byFile := make(map[string]string, len(g.Nodes))
	clash := map[string]bool{}
	for _, n := range g.Nodes {
		byFile[n.File+"\x00"+n.FQN] = n.ID
		clash[n.FQN] = true
	}
	out := make([]Node, len(types))
	for i, t := range types {
		out[i] = t
		if id, ok := byFile[t.File+"\x00"+t.FQN]; ok {
			out[i].ID = id
		} else if clash[t.FQN] {
			out[i].ID = t.File + ":" + t.FQN
		}
	}
	return out
}

// TypesIn returns the types in one file with their line ranges (no dependency resolution).
func TypesIn(src []byte, path string) []Node {
	f, err := parse(src)
	if err != nil {
		return nil
	}
	u := newUnit(path, f)
	var out []Node
	for _, d := range u.types() {
		out = append(out, u.node(d))
	}
	return out
}

func newUnit(path string, f *file) *unit {
	u := &unit{path: path, f: f, imports: map[string]string{}}
	for _, c := range named(f.root) {
		switch c.Type() {
		case "package_declaration":
			for _, n := range named(c) {
				if n.Type() == "scoped_identifier" || n.Type() == "identifier" {
					u.pkg = f.text(n)
				}
			}
		case "import_declaration":
			if childOfType(c, "static") != nil {
				continue
			}
			var name string
			for _, n := range named(c) {
				if n.Type() == "scoped_identifier" || n.Type() == "identifier" {
					name = f.text(n)
				}
			}
			if childOfType(c, "asterisk") != nil {
				u.stars = append(u.stars, name)
			} else if name != "" {
				u.imports[name[strings.LastIndex(name, ".")+1:]] = name
			}
		}
	}
	return u
}

func (u *unit) types() []typeDecl {
	var out []typeDecl
	var collect func(n *sitter.Node, prefix string)
	collect = func(n *sitter.Node, prefix string) {
		for _, c := range named(n) {
			if _, ok := typeDecls[c.Type()]; ok {
				name := u.f.text(c.ChildByFieldName("name"))
				fqn := name
				if prefix != "" {
					fqn = prefix + "." + name
				}
				out = append(out, typeDecl{node: c, fqn: fqn, name: name, unit: u})
				collect(c.ChildByFieldName("body"), fqn)
			} else if c.Type() == "enum_body_declarations" {
				collect(c, prefix)
			}
		}
	}
	collect(u.f.root, u.pkg)
	return out
}

func (u *unit) node(d typeDecl) Node {
	f := u.f
	n := Node{
		ID: d.fqn, FQN: d.fqn, Module: ModuleOf(u.path), Name: d.name, Pkg: u.pkg, File: u.path,
		StartLine: startLine(d.node), EndLine: endLine(d.node),
		Annotations: f.annotationNames(d.node), Members: []Member{}, Endpoints: []string{},
	}
	n.Kind = typeDecls[d.node.Type()]
	if n.Kind == "class" && hasModifier(d.node, "abstract") {
		n.Kind = "abstract"
	}

	for _, m := range members(d.node) {
		switch m.Type() {
		case "method_declaration", "constructor_declaration", "compact_constructor_declaration":
			kind := "method"
			if m.Type() != "method_declaration" {
				kind = "constructor"
			}
			cost := MethodCost(f, m)
			n.Members = append(n.Members, Member{
				Name: f.text(m.ChildByFieldName("name")), Kind: kind, Signature: f.signature(m),
				StartLine: startLine(m), EndLine: endLine(m), Cost: &cost,
			})
		case "field_declaration", "constant_declaration":
			var names []string
			for _, v := range named(m) {
				if v.Type() == "variable_declarator" {
					names = append(names, f.text(v.ChildByFieldName("name")))
				}
			}
			sig := f.text(m)
			if i := strings.Index(sig, "="); i > 0 {
				sig = sig[:i]
			}
			n.Members = append(n.Members, Member{
				Name: strings.Join(names, ", "), Kind: "field", Signature: strings.TrimSuffix(collapse(sig), ";"),
				StartLine: startLine(m), EndLine: endLine(m), Value: f.constantValue(m, m.Type() == "constant_declaration"),
			})
		}
	}
	n.Stereotype = u.stereotype(d)
	n.Endpoints = u.endpoints(d.node)
	return n
}

var literalTypes = map[string]bool{
	"string_literal": true, "character_literal": true, "decimal_integer_literal": true, "hex_integer_literal": true,
	"decimal_floating_point_literal": true, "true": true, "false": true,
}

// constantValue is the literal initializer of a single static final field (interface fields are implicitly so).
func (f *file) constantValue(fd *sitter.Node, implicit bool) string {
	if !implicit && !(hasModifier(fd, "static") && hasModifier(fd, "final")) {
		return ""
	}
	var decls []*sitter.Node
	for _, v := range named(fd) {
		if v.Type() == "variable_declarator" {
			decls = append(decls, v)
		}
	}
	if len(decls) != 1 {
		return ""
	}
	if v := decls[0].ChildByFieldName("value"); v != nil && literalTypes[v.Type()] {
		return f.text(v)
	}
	return ""
}

// signature is a member's declaration without annotations and body: "public Order get(Long id)".
func (f *file) signature(m *sitter.Node) string {
	var parts []string
	for _, c := range children(childOfType(m, "modifiers")) {
		if c.Type() != "annotation" && c.Type() != "marker_annotation" {
			parts = append(parts, f.text(c))
		}
	}
	if tp := m.ChildByFieldName("type_parameters"); tp != nil {
		parts = append(parts, f.text(tp))
	}
	if t := m.ChildByFieldName("type"); t != nil {
		parts = append(parts, f.text(t))
	}
	parts = append(parts, f.text(m.ChildByFieldName("name"))+f.text(m.ChildByFieldName("parameters")))
	return collapse(strings.Join(parts, " "))
}

func (u *unit) stereotype(d typeDecl) string {
	f := u.f
	for _, a := range f.annotationNames(d.node) {
		switch a {
		case "RestController", "Controller":
			return "controller"
		case "RestControllerAdvice", "ControllerAdvice":
			return "advice"
		case "Service":
			return "service"
		case "Repository":
			return "repository"
		case "Entity", "Embeddable", "MappedSuperclass", "Document", "Table":
			return "entity"
		case "Configuration", "SpringBootApplication", "EnableAutoConfiguration", "ConfigurationProperties":
			return "config"
		case "Component", "Mapper":
			return "component"
		}
	}
	for _, t := range typeList(childOfType(d.node, "extends_interfaces")) {
		if repoBase.MatchString(f.baseType(t)) {
			return "repository"
		}
	}
	switch {
	case strings.HasSuffix(d.name, "Dto"), strings.HasSuffix(d.name, "DTO"),
		strings.HasSuffix(d.name, "Request"), strings.HasSuffix(d.name, "Response"):
		return "dto"
	case strings.HasSuffix(d.name, "Exception"):
		return "exception"
	case strings.Contains(u.path, "src/test/"):
		return "test"
	}
	for _, m := range membersOfType(d.node, "method_declaration") {
		if f.hasAnnotation(m, "Test") {
			return "test"
		}
	}
	return "other"
}

// injected lists types injected through constructors, @Autowired/@Inject fields or Lombok final fields.
func (u *unit) injected(decl *sitter.Node) []string {
	f := u.f
	var out []string
	for _, c := range membersOfType(decl, "constructor_declaration") {
		for _, p := range named(c.ChildByFieldName("parameters")) {
			if p.Type() == "formal_parameter" {
				out = append(out, f.baseType(p.ChildByFieldName("type")))
			}
		}
	}
	lombok := f.hasAnnotation(decl, "RequiredArgsConstructor") || f.hasAnnotation(decl, "AllArgsConstructor")
	for _, fd := range membersOfType(decl, "field_declaration") {
		if hasModifier(fd, "static") {
			continue
		}
		a := f.annotations(fd)
		_, aw := a["Autowired"]
		_, in := a["Inject"]
		_, rs := a["Resource"]
		if aw || in || rs || (lombok && hasModifier(fd, "final")) {
			out = append(out, f.baseType(fd.ChildByFieldName("type")))
		}
	}
	return out
}

func (u *unit) endpoints(decl *sitter.Node) []string {
	f := u.f
	base := ""
	if a, ok := f.annotations(decl)["RequestMapping"]; ok {
		base = f.annotationPath(a)
	}
	out := []string{}
	for _, m := range membersOfType(decl, "method_declaration") {
		for name, a := range f.annotations(m) {
			if !mappingAnnotations[name] {
				continue
			}
			verb := strings.ToUpper(strings.TrimSuffix(name, "Mapping"))
			if name == "RequestMapping" {
				verb = f.requestMethod(a)
			}
			out = append(out, verb+" "+joinPath(base, f.annotationPath(a))+"  → "+f.text(m.ChildByFieldName("name"))+"()")
		}
	}
	sort.Strings(out)
	return out
}

func (f *file) annotationValue(a *sitter.Node, keys ...string) *sitter.Node {
	args := a.ChildByFieldName("arguments")
	if args == nil {
		return nil
	}
	var single *sitter.Node
	for _, c := range named(args) {
		if c.Type() == "element_value_pair" {
			k := f.text(c.ChildByFieldName("key"))
			for _, want := range keys {
				if k == want {
					return c.ChildByFieldName("value")
				}
			}
		} else if single == nil {
			single = c
		}
	}
	for _, want := range keys {
		if want == "value" {
			return single
		}
	}
	return nil
}

func (f *file) annotationPath(a *sitter.Node) string {
	v := f.annotationValue(a, "value", "path")
	if v != nil && v.Type() == "element_value_array_initializer" {
		vals := named(v)
		if len(vals) == 0 {
			return ""
		}
		v = vals[0]
	}
	if v != nil && v.Type() == "string_literal" {
		return strings.Trim(f.text(v), `"`)
	}
	return ""
}

func (f *file) requestMethod(a *sitter.Node) string {
	v := f.annotationValue(a, "method")
	if v == nil {
		return "ANY"
	}
	s := strings.Trim(f.text(v), "{} ")
	return s[strings.LastIndex(s, ".")+1:]
}

func joinPath(base, sub string) string {
	p := "/" + base + "/" + sub
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	if len(p) > 1 {
		p = strings.TrimSuffix(p, "/")
	}
	return p
}

// resolver maps a simple type name to a project type using imports, the package and unique names.
type resolver struct {
	u        *unit
	known    map[string]bool
	bySimple map[string][]string
}

func (r resolver) resolve(simple string) string {
	if simple == "" {
		return ""
	}
	if imp, ok := r.u.imports[simple]; ok {
		if r.known[imp] {
			return imp
		}
		return ""
	}
	candidates := r.bySimple[simple]
	if len(candidates) == 0 {
		return ""
	}
	if same := r.u.pkg + "." + simple; r.known[same] {
		return same
	}
	for _, star := range r.u.stars {
		if r.known[star+"."+simple] {
			return star + "." + simple
		}
	}
	for _, c := range candidates { // nested type in this package
		if strings.HasPrefix(c, r.u.pkg+".") && strings.Contains(c[len(r.u.pkg)+1:], ".") {
			return c
		}
	}
	if len(candidates) == 1 {
		return candidates[0]
	}
	return ""
}
