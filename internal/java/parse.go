// Package java reads Java source into types, methods, calls and Spring facts, using a pure-Go
// tree-sitter parser so Codist needs no C compiler.
package java

import (
	"regexp"
	"strconv"
	"strings"

	ts "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

var lang = grammars.JavaLanguage()

// Type is one class, interface, enum or record.
type Type struct {
	ID          string     `json:"id"` // package.Name, or package.Outer.Inner
	Name        string     `json:"name"`
	Package     string     `json:"package"`
	File        string     `json:"file"`
	Module      string     `json:"module"`
	Kind        string     `json:"kind"`
	Stereotype  string     `json:"stereotype"` // controller, service, repository, client, entity, config, component, test, other
	Annotations []string   `json:"annotations"`
	Extends     []string   `json:"extends"`
	Fields      []Field    `json:"fields"`
	Methods     []Method   `json:"methods"`
	Endpoints   []Endpoint `json:"endpoints"`
	Feign       string     `json:"feign,omitempty"` // service a Feign client calls
	Imports     []string   `json:"-"`
	Line, End   int
}

// Field is a field with its declared type.
type Field struct {
	Name, Type string
	Line       int
	Value      string // literal value of a constant, if any
}

// Method is a method or constructor.
type Method struct {
	Name        string  `json:"name"`
	Line        int     `json:"line"`
	End         int     `json:"end"`
	Params      []Field `json:"params"`
	Returns     string  `json:"returns"`
	Annotations []string
	Calls       []Call `json:"calls"`
	Cost        Cost   `json:"cost"`
}

// Call is a method call written in a method: receiver.name(...).
type Call struct {
	Receiver string // "" for an unqualified call, "this", a field, a variable or a type
	Name     string
	Line     int
}

// Endpoint is an HTTP route a controller handles or a Feign client calls.
type Endpoint struct {
	Verb, Path, Handler string
	Line                int
}

// File is one parsed source file.
type File struct {
	Path  string
	Src   []byte
	Lines []string
	Root  *ts.Node
	Types []Type
}

// Parse reads one Java file. It never fails: a file that does not parse yields what could be read.
func Parse(path string, src []byte) *File {
	p := ts.NewParser(lang)
	tree, err := p.Parse(src)
	f := &File{Path: path, Src: src, Lines: strings.Split(string(src), "\n")}
	if err != nil || tree == nil {
		return f
	}
	f.Root = tree.RootNode()
	pkg := ""
	var imports []string
	for i := 0; i < f.Root.NamedChildCount(); i++ {
		n := f.Root.NamedChild(i)
		switch n.Type(lang) {
		case "package_declaration":
			pkg = strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(f.text(n), "package")), ";")
		case "import_declaration":
			imports = append(imports, strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(f.text(n), "import"), " static")), ";"))
		}
	}
	for i := 0; i < f.Root.NamedChildCount(); i++ {
		f.readType(f.Root.NamedChild(i), pkg, "", imports)
	}
	return f
}

func (f *File) text(n *ts.Node) string {
	if n == nil {
		return ""
	}
	return n.Text(f.Src)
}

func line(n *ts.Node) int { return int(n.StartPoint().Row) + 1 }
func endLine(n *ts.Node) int {
	return int(n.EndPoint().Row) + 1
}

func field(n *ts.Node, name string) *ts.Node { return n.ChildByFieldName(name, lang) }

func named(n *ts.Node) []*ts.Node {
	out := make([]*ts.Node, 0, n.NamedChildCount())
	for i := 0; i < n.NamedChildCount(); i++ {
		out = append(out, n.NamedChild(i))
	}
	return out
}

var typeKinds = map[string]string{"class_declaration": "class", "interface_declaration": "interface", "enum_declaration": "enum", "record_declaration": "record", "annotation_type_declaration": "annotation"}

func (f *File) readType(n *ts.Node, pkg, outer string, imports []string) {
	kind, ok := typeKinds[n.Type(lang)]
	if !ok {
		return
	}
	name := f.text(field(n, "name"))
	id := name
	if outer != "" {
		id = outer + "." + name
	}
	t := Type{ID: strings.TrimPrefix(pkg+"."+id, "."), Name: name, Package: pkg, File: f.Path, Kind: kind, Imports: imports, Line: line(n), End: endLine(n)}
	mappingBase := ""
	for _, c := range named(n) {
		switch c.Type(lang) {
		case "modifiers":
			for _, a := range named(c) {
				if strings.HasSuffix(a.Type(lang), "annotation") {
					t.Annotations = append(t.Annotations, f.text(a))
					if p := mappingPath(a, f); p != "" && annotationName(f.text(a)) == "RequestMapping" {
						mappingBase = p
					}
					if annotationName(f.text(a)) == "FeignClient" {
						t.Feign = feignTarget(f.text(a))
					}
				}
			}
		case "superclass", "super_interfaces", "extends_interfaces":
			for _, w := range regexp.MustCompile(`[A-Z]\w*`).FindAllString(f.text(c), -1) {
				t.Extends = append(t.Extends, w)
			}
		}
	}
	t.Stereotype = stereotype(t.Annotations, name, f.Path)
	body := field(n, "body")
	if body == nil {
		f.Types = append(f.Types, t)
		return
	}
	for _, m := range named(body) {
		switch m.Type(lang) {
		case "field_declaration", "constant_declaration":
			typ := f.text(field(m, "type"))
			for _, d := range named(m) {
				if d.Type(lang) == "variable_declarator" {
					fl := Field{Name: f.text(field(d, "name")), Type: typ, Line: line(d)}
					if v := field(d, "value"); v != nil && isLiteral(v.Type(lang)) {
						fl.Value = f.text(v)
					}
					t.Fields = append(t.Fields, fl)
				}
			}
		case "method_declaration", "constructor_declaration":
			t.Methods = append(t.Methods, f.readMethod(m, &t, mappingBase))
		case "record_declaration_body":
		default:
			if _, nested := typeKinds[m.Type(lang)]; nested {
				f.readType(m, pkg, id, imports)
			}
		}
	}
	if kind == "record" {
		if ps := field(n, "parameters"); ps != nil {
			for _, p := range named(ps) {
				t.Fields = append(t.Fields, Field{Name: f.text(field(p, "name")), Type: f.text(field(p, "type")), Line: line(p)})
			}
		}
	}
	f.Types = append(f.Types, t)
}

func isLiteral(kind string) bool {
	return strings.HasSuffix(kind, "_literal") || kind == "true" || kind == "false"
}

func (f *File) readMethod(n *ts.Node, t *Type, base string) Method {
	m := Method{Name: f.text(field(n, "name")), Line: line(n), End: endLine(n), Returns: f.text(field(n, "type"))}
	if ps := field(n, "parameters"); ps != nil {
		for _, p := range named(ps) {
			if p.Type(lang) == "formal_parameter" || p.Type(lang) == "spread_parameter" {
				m.Params = append(m.Params, Field{Name: f.text(field(p, "name")), Type: f.text(field(p, "type")), Line: line(p)})
			}
		}
	}
	for _, c := range named(n) {
		if c.Type(lang) != "modifiers" {
			continue
		}
		for _, a := range named(c) {
			if !strings.HasSuffix(a.Type(lang), "annotation") {
				continue
			}
			text := f.text(a)
			m.Annotations = append(m.Annotations, text)
			if verb := verbOf(annotationName(text)); verb != "" {
				verb = requestVerb(text, verb)
				t.Endpoints = append(t.Endpoints, Endpoint{Verb: verb, Path: joinPath(base, mappingPath(a, f)), Handler: m.Name, Line: m.Line})
			}
		}
	}
	if body := field(n, "body"); body != nil {
		f.calls(body, &m)
		types := map[string]string{}
		for _, fl := range t.Fields {
			types[fl.Name] = fl.Type
		}
		for _, p := range m.Params {
			types[p.Name] = p.Type
		}
		m.Cost = measure(f, body, types)
	}
	m.Cost.Lines = m.End - m.Line + 1
	m.Cost.Params = len(m.Params)
	return m
}

func (f *File) calls(n *ts.Node, m *Method) {
	if n.Type(lang) == "method_invocation" {
		recv := f.text(field(n, "object"))
		if strings.ContainsAny(recv, "().") { // chained: keep the last name only when it is a plain field access
			if i := strings.LastIndex(recv, "."); i >= 0 && !strings.ContainsAny(recv[i+1:], "()") {
				recv = recv[i+1:]
			} else {
				recv = "?"
			}
		}
		m.Calls = append(m.Calls, Call{Receiver: recv, Name: f.text(field(n, "name")), Line: line(n)})
	}
	for _, c := range named(n) {
		f.calls(c, m)
	}
}

func annotationName(text string) string {
	s := strings.TrimPrefix(text, "@")
	if i := strings.IndexAny(s, "( "); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[i+1:]
	}
	return s
}

func verbOf(annotation string) string {
	switch annotation {
	case "GetMapping":
		return "GET"
	case "PostMapping":
		return "POST"
	case "PutMapping":
		return "PUT"
	case "DeleteMapping":
		return "DELETE"
	case "PatchMapping":
		return "PATCH"
	case "RequestMapping":
		return "ANY"
	}
	return ""
}

var methodAttr = regexp.MustCompile(`RequestMethod\.([A-Z]+)`)

func requestVerb(text, verb string) string {
	if m := methodAttr.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return verb
}

var quoted = regexp.MustCompile(`"((?:\\.|[^"\\])*)"`)

// mappingPath reads the first path from a mapping annotation: @GetMapping("/x"), value=, path=.
func mappingPath(n *ts.Node, f *File) string {
	text := f.text(n)
	if !strings.Contains(text, "(") {
		return ""
	}
	args := text[strings.Index(text, "(")+1:]
	for _, key := range []string{"value", "path"} {
		if i := regexp.MustCompile(`\b` + key + `\s*=`).FindStringIndex(args); i != nil {
			if m := quoted.FindStringSubmatch(args[i[1]:]); m != nil {
				return m[1]
			}
		}
	}
	if strings.Contains(args, "=") && !strings.HasPrefix(strings.TrimSpace(args), `"`) && !strings.HasPrefix(strings.TrimSpace(args), "{") {
		return ""
	}
	if m := quoted.FindStringSubmatch(args); m != nil {
		return m[1]
	}
	return ""
}

func joinPath(base, p string) string {
	if base == "" && p == "" {
		return "/"
	}
	out := "/" + strings.Trim(base, "/")
	if p = strings.Trim(p, "/"); p != "" {
		out = strings.TrimSuffix(out, "/") + "/" + p
	}
	return out
}

var feignName = regexp.MustCompile(`(?:name|value)\s*=\s*"([^"]+)"|^@FeignClient\s*\(\s*"([^"]+)"`)

func feignTarget(text string) string {
	if m := feignName.FindStringSubmatch(text); m != nil {
		if m[1] != "" {
			return m[1]
		}
		return m[2]
	}
	return ""
}

func stereotype(annotations []string, name, path string) string {
	has := func(names ...string) bool {
		for _, a := range annotations {
			for _, n := range names {
				if annotationName(a) == n {
					return true
				}
			}
		}
		return false
	}
	switch {
	case strings.Contains(path, "/src/test/") || strings.HasSuffix(name, "Test") || strings.HasSuffix(name, "Tests"):
		return "test"
	case has("RestController", "Controller", "ControllerAdvice", "RestControllerAdvice"):
		return "controller"
	case has("FeignClient"):
		return "client"
	case has("Repository") || strings.HasSuffix(name, "Repository"):
		return "repository"
	case has("Service"):
		return "service"
	case has("Entity", "Embeddable", "MappedSuperclass", "Document"):
		return "entity"
	case has("Configuration", "SpringBootApplication"):
		return "config"
	case has("Component"):
		return "component"
	}
	return "other"
}

// Lines counts lines of a type for size reports.
func (t Type) Lines() int { return t.End - t.Line + 1 }

func itoa(n int) string { return strconv.Itoa(n) }
