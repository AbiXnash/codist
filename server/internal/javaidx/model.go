// Package javaidx parses Java with tree-sitter: it builds the type-level dependency graph
// and runs the deterministic Spring/Java rule checks.
package javaidx

// Node is a Java type in the project. ID is its fully qualified name.
type Node struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Pkg         string   `json:"pkg"`
	Kind        string   `json:"kind"`       // class, abstract, interface, enum, record, annotation
	Stereotype  string   `json:"stereotype"` // controller, service, repository, entity, config, component, dto, advice, exception, test, other
	File        string   `json:"file"`
	StartLine   int      `json:"startLine"`
	EndLine     int      `json:"endLine"`
	Annotations []string `json:"annotations"`
	Members     []Member `json:"members"`
	Endpoints   []string `json:"endpoints"`
}

func (n Node) LOC() int { return n.EndLine - n.StartLine + 1 }

type Member struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"` // method, constructor, field
	Signature string `json:"signature"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Value     string `json:"value,omitempty"` // literal value of a static final constant
}

// Edge kinds: extends, implements, injects, uses.
type Edge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Kind   string `json:"kind"`
}

type Graph struct {
	Nodes       []Node `json:"nodes"`
	Edges       []Edge `json:"edges"`
	Source      string `json:"source"`
	BuiltAt     int64  `json:"builtAt"`
	ParseErrors int    `json:"parseErrors"`
}

// RuleFinding is a problem found by a deterministic check.
type RuleFinding struct {
	ID         string // stable rule id, shared with the knowledge base (used for ignores)
	Line       int
	EndLine    int
	Severity   string
	Category   string
	Title      string
	Detail     string
	Suggestion string
}
