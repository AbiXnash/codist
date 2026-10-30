package javaidx

import (
	"context"
	"strings"
	"unicode"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/java"
)

// file is a parsed compilation unit.
type file struct {
	root *sitter.Node
	src  []byte
}

func parse(src []byte) (*file, error) {
	p := sitter.NewParser()
	p.SetLanguage(java.GetLanguage())
	tree, err := p.ParseCtx(context.Background(), nil, src)
	if err != nil {
		return nil, err
	}
	return &file{root: tree.RootNode(), src: src}, nil
}

func (f *file) text(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	return n.Content(f.src)
}

func startLine(n *sitter.Node) int { return int(n.StartPoint().Row) + 1 }
func endLine(n *sitter.Node) int   { return int(n.EndPoint().Row) + 1 }

func children(n *sitter.Node) []*sitter.Node {
	if n == nil {
		return nil
	}
	out := make([]*sitter.Node, 0, n.ChildCount())
	for i := 0; i < int(n.ChildCount()); i++ {
		out = append(out, n.Child(i))
	}
	return out
}

func named(n *sitter.Node) []*sitter.Node {
	if n == nil {
		return nil
	}
	out := make([]*sitter.Node, 0, n.NamedChildCount())
	for i := 0; i < int(n.NamedChildCount()); i++ {
		out = append(out, n.NamedChild(i))
	}
	return out
}

func childOfType(n *sitter.Node, typ string) *sitter.Node {
	for _, c := range children(n) {
		if c.Type() == typ {
			return c
		}
	}
	return nil
}

// walk visits n and its descendants depth-first; returning false skips a node's children.
func walk(n *sitter.Node, fn func(*sitter.Node) bool) {
	if n == nil || !fn(n) {
		return
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		walk(n.Child(i), fn)
	}
}

func findAll(n *sitter.Node, typ string) []*sitter.Node {
	var out []*sitter.Node
	walk(n, func(c *sitter.Node) bool {
		if c.Type() == typ {
			out = append(out, c)
		}
		return true
	})
	return out
}

var typeDecls = map[string]string{
	"class_declaration":           "class",
	"interface_declaration":       "interface",
	"enum_declaration":            "enum",
	"record_declaration":          "record",
	"annotation_type_declaration": "annotation",
}

func hasModifier(decl *sitter.Node, keyword string) bool {
	for _, c := range children(childOfType(decl, "modifiers")) {
		if c.Type() == keyword {
			return true
		}
	}
	return false
}

// annotations returns the annotation nodes on a declaration, keyed by simple name.
func (f *file) annotations(decl *sitter.Node) map[string]*sitter.Node {
	out := map[string]*sitter.Node{}
	for _, c := range named(childOfType(decl, "modifiers")) {
		if c.Type() == "annotation" || c.Type() == "marker_annotation" {
			name := f.text(c.ChildByFieldName("name"))
			out[name[strings.LastIndex(name, ".")+1:]] = c
		}
	}
	return out
}

func (f *file) annotationNames(decl *sitter.Node) []string {
	names := []string{}
	for _, c := range named(childOfType(decl, "modifiers")) {
		if c.Type() == "annotation" || c.Type() == "marker_annotation" {
			name := f.text(c.ChildByFieldName("name"))
			names = append(names, name[strings.LastIndex(name, ".")+1:])
		}
	}
	return names
}

func (f *file) hasAnnotation(decl *sitter.Node, name string) bool {
	_, ok := f.annotations(decl)[name]
	return ok
}

// baseType is the simple name of a type node: List<Order> -> List, Map.Entry -> Entry, Order[] -> Order.
func (f *file) baseType(t *sitter.Node) string {
	if t == nil {
		return ""
	}
	switch t.Type() {
	case "type_identifier":
		return f.text(t)
	case "generic_type":
		for _, c := range named(t) {
			if c.Type() == "type_identifier" || c.Type() == "scoped_type_identifier" {
				return f.baseType(c)
			}
		}
	case "scoped_type_identifier":
		var last string
		for _, c := range named(t) {
			if c.Type() == "type_identifier" {
				last = f.text(c)
			}
		}
		return last
	case "array_type":
		return f.baseType(t.ChildByFieldName("element"))
	}
	return ""
}

// typeList returns the types inside superclass / super_interfaces / extends_interfaces nodes.
func typeList(n *sitter.Node) []*sitter.Node {
	if n == nil {
		return nil
	}
	if tl := childOfType(n, "type_list"); tl != nil {
		return named(tl)
	}
	return named(n)
}

// members returns the declarations in a type body, unwrapping enum bodies.
func members(decl *sitter.Node) []*sitter.Node {
	var out []*sitter.Node
	for _, c := range named(decl.ChildByFieldName("body")) {
		if c.Type() == "enum_body_declarations" {
			out = append(out, named(c)...)
		} else {
			out = append(out, c)
		}
	}
	return out
}

func membersOfType(decl *sitter.Node, typ string) []*sitter.Node {
	var out []*sitter.Node
	for _, m := range members(decl) {
		if m.Type() == typ {
			out = append(out, m)
		}
	}
	return out
}

func isUpper(s string) bool {
	for _, r := range s {
		return unicode.IsUpper(r)
	}
	return false
}

func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Valid reports whether Java source parses without syntax errors.
func Valid(src []byte) bool {
	f, err := parse(src)
	return err == nil && !f.root.HasError()
}
