package javaidx

import (
	"fmt"
	"regexp"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// Dead code and leftovers: commented-out code, comments that admit a bug, unused variables, fields,
// methods, imports and classes, and conditions that decide nothing. Then recommendations: a better
// method, better logs, and what a method costs.

var qualityIDs = []struct{ prefix, id string }{
	{"Feign route", "feign-route-missing"},
	{"Commented-out", "commented-out-code"},
	{"Comment flags", "bug-comment"},
	{"Unused local variable", "unused-local"},
	{"Unused private field", "unused-field"},
	{"Unused private method", "unused-method"},
	{"Unused import", "unused-import"},
	{"Unused class", "unused-class"},
	{"Empty ", "empty-block"},
	{"Condition is always", "constant-condition"},
	{"Both branches", "identical-branches"},
	{"Unreachable code", "unreachable-code"},
	{"Returning a boolean", "simplify-boolean-return"},
	{"High complexity", "high-complexity"},
	{"Long method", "long-method"},
	{"Too many parameters", "too-many-parameters"},
	{"Nested loops", "nested-loops"},
	{"Database call in a loop", "query-in-loop"},
	{"Lookup in a list inside a loop", "list-lookup-in-loop"},
	{"Better method", "better-method"},
	{"Better log", "better-log"},
}

func (c *checker) quality(test bool) {
	f := c.f
	c.comments()
	c.unusedImports()
	for _, n := range findAll(f.root, "if_statement") {
		c.ifStatement(n)
	}
	for _, typ := range []string{"for_statement", "enhanced_for_statement", "while_statement"} {
		for _, n := range findAll(f.root, typ) {
			if b := n.ChildByFieldName("body"); b != nil && isEmptyBlock(b) {
				c.add(n, n, "medium", "bug", "Empty loop body", "The loop does nothing. Either the work inside it was lost or the loop should go.", "Restore the work the loop was meant to do, or delete the loop.")
			}
		}
	}
	for _, n := range findAll(f.root, "block") {
		c.unreachable(n)
	}
	for _, n := range findAll(f.root, "binary_expression") {
		c.referenceEquality(n)
	}
	if !test {
		for _, n := range findAll(f.root, "variable_declarator") {
			c.hardcodedSecret(n)
		}
	}
	for _, m := range findAll(f.root, "method_declaration") {
		c.unusedLocals(m)
		if !test {
			c.cost(m)
		}
	}
	for _, n := range findAll(f.root, "method_invocation") {
		c.betterMethod(n)
		c.betterLog(n)
	}
	for _, typ := range []string{"class_declaration", "enum_declaration"} {
		for _, t := range findAll(f.root, typ) {
			c.unusedMembers(t)
		}
	}
	if !test {
		for _, n := range named(f.root) {
			if _, ok := typeDecls[n.Type()]; ok {
				name := f.text(n.ChildByFieldName("name"))
				for _, route := range c.ctx.FeignUnserved[name] {
					c.add(n, n, "critical", "bug", "Feign route not served: "+route, name+" calls "+route+", but the service it names has no controller for that route. Every call fails with 404 at run time; the code compiles and unit tests with mocks pass. This usually follows a contract change that not every caller picked up.", "Update the client to the route the service now serves, or restore the old route until all callers have moved.")
				}
				if c.ctx.Unused[name] && !hasMainMethod(n) {
					c.add(n, n, "low", "design", "Unused class "+name, name+" is not referenced by any other class in the project and is not a Spring bean. Unused classes keep costing reading and upgrade time.", "Delete it (git keeps the history), or wire it in if it was meant to be used.")
				}
			}
		}
	}
}

var (
	commentMarker = regexp.MustCompile(`^(//+|/\*+|\*+/?)\s?`)
	codeLine      = regexp.MustCompile(`(;\s*$|\{\s*$|^\s*\}|^\s*(if|else|for|while|switch|try|catch|return|throw)\b\s*[\(\{]?.*[\)\{;]\s*$|^\s*(public|private|protected|static|final)\s+[\w<>\[\], ]+\s+\w+\s*[\(=;]|^\s*@[A-Z]\w+(\(.*\))?\s*$|^\s*\w+(\.\w+)*\(.*\);?\s*$)`)
	methodLine    = regexp.MustCompile(`^\s*(public|private|protected|static|final|void|[A-Z]\w*(<.*>)?)\s+.*\w+\s*\(.*\)\s*(throws [\w, .]+)?\s*\{?\s*$`)
	bugWord       = regexp.MustCompile(`\b(BUG|FIXME|HACK|XXX)\b|(?i)\b(known bug|this is a bug|bug here|bug:|workaround for|temporary fix|temp fix|broken|does not work|doesn't work|should not happen)\b`)
	todoWord      = regexp.MustCompile(`\bTODO\b`)
)

// comments reports commented-out code and comments that admit a problem.
func (c *checker) comments() {
	f := c.f
	type chunk struct {
		first, last *sitter.Node
		lines       []string
	}
	var chunks []*chunk
	var cur *chunk
	var all []*sitter.Node
	walk(f.root, func(n *sitter.Node) bool {
		if n.Type() == "line_comment" || n.Type() == "block_comment" {
			all = append(all, n)
		}
		return true
	})
	for _, n := range all {
		text := f.text(n)
		if strings.HasPrefix(text, "/**") { // documentation, including examples in it
			cur = nil
			continue
		}
		var lines []string
		for _, l := range strings.Split(text, "\n") {
			lines = append(lines, strings.TrimSpace(strings.TrimSuffix(commentMarker.ReplaceAllString(strings.TrimSpace(l), ""), "*/")))
		}
		if m := bugWord.FindString(text); m != "" {
			c.add(n, n, "medium", "bug", "Comment flags a known problem: "+strings.TrimSpace(m), "This comment says the code has a problem that is still there: "+oneLineOf(text), "Fix the problem and delete the comment, or track it in an issue with an owner.")
		} else if todoWord.MatchString(text) {
			c.add(n, n, "low", "design", "Comment flags unfinished work: TODO", "Left for later: "+oneLineOf(text), "Do it now, or track it in an issue with an owner.")
		}
		if cur != nil && n.Type() == "line_comment" && startLine(n) == endLine(cur.last)+1 {
			cur.last, cur.lines = n, append(cur.lines, lines...)
			continue
		}
		cur = &chunk{first: n, last: n, lines: lines}
		chunks = append(chunks, cur)
		if n.Type() != "line_comment" {
			cur = nil
		}
	}
	for _, ch := range chunks {
		code, words := 0, 0
		kind := "code"
		for _, l := range ch.lines {
			if strings.TrimSpace(l) == "" {
				continue
			}
			words++
			if codeLine.MatchString(l) {
				code++
				t := strings.TrimSpace(l)
				switch {
				case methodLine.MatchString(l) && kind == "code" && !strings.HasPrefix(t, "if") && !strings.HasPrefix(t, "for") && !strings.HasPrefix(t, "while"):
					kind = "method"
				case strings.HasPrefix(t, "if") || strings.HasPrefix(t, "else") || strings.HasPrefix(t, "} else"):
					if kind == "code" {
						kind = "condition"
					}
				case strings.HasPrefix(t, "for") || strings.HasPrefix(t, "while"):
					if kind == "code" {
						kind = "loop"
					}
				}
			}
		}
		if code == 0 || code*2 < words {
			continue // prose
		}
		c.add(ch.first, ch.last, "low", "design", "Commented-out "+kind,
			fmt.Sprintf("%d line(s) of %s are commented out: %s. Nobody knows whether they are still needed, they go stale, and git already keeps the old version. If this was disabled to get past a bug, the bug is still there.", code, kind, firstCode(ch.lines)),
			"Delete it, or restore it if the behaviour is still needed.")
	}
}

func oneLineOf(s string) string {
	s = strings.Join(strings.Fields(commentMarker.ReplaceAllString(strings.TrimSpace(s), "")), " ")
	s = strings.TrimSuffix(s, "*/")
	if len(s) > 110 {
		s = s[:107] + "…"
	}
	return strings.TrimSpace(s)
}

func firstCode(lines []string) string {
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			return "`" + strings.TrimSpace(l) + "`"
		}
	}
	return ""
}

func (c *checker) unusedImports() {
	f := c.f
	used := map[string]bool{}
	walk(f.root, func(n *sitter.Node) bool {
		if n.Type() == "import_declaration" {
			return false
		}
		if n.Type() == "identifier" || n.Type() == "type_identifier" {
			used[f.text(n)] = true
		}
		return true
	})
	// Javadoc links count as a use.
	docs := string(f.src)
	for _, imp := range findAll(f.root, "import_declaration") {
		text := strings.TrimSuffix(strings.TrimSpace(f.text(imp)), ";")
		if strings.HasSuffix(text, "*") {
			continue
		}
		name := text[strings.LastIndexAny(text, ". ")+1:]
		if !used[name] && !strings.Contains(docs, "{@link "+name) && !strings.Contains(docs, "@see "+name) {
			c.add(imp, imp, "low", "design", "Unused import "+name, "Nothing in this file uses "+name+".", "Delete the import.")
		}
	}
}

func isEmptyBlock(n *sitter.Node) bool {
	return n != nil && n.Type() == "block" && len(named(n)) == 0
}

func (c *checker) ifStatement(n *sitter.Node) {
	f := c.f
	cond, then, alt := n.ChildByFieldName("condition"), n.ChildByFieldName("consequence"), n.ChildByFieldName("alternative")
	text := strings.Trim(strings.TrimSpace(f.text(cond)), "()")
	if strings.TrimSpace(text) == "true" || strings.TrimSpace(text) == "false" {
		c.add(n, cond, "medium", "bug", "Condition is always "+strings.TrimSpace(text), "`if ("+strings.TrimSpace(text)+")` decides nothing: one branch can never run. It is usually left over from debugging.", "Remove the condition and keep the code that should run.")
	}
	if b := findBinary(cond); b != nil && (f.text(b.ChildByFieldName("operator")) == "==" || f.text(b.ChildByFieldName("operator")) == "!=") {
		if l, r := f.text(b.ChildByFieldName("left")), f.text(b.ChildByFieldName("right")); l == r && l != "" {
			c.add(n, cond, "high", "bug", "Condition is always the same: "+l+" compared with itself", "Comparing a value with itself is a copy-paste slip: one side should be something else.", "Compare with the value that was meant.")
		}
	}
	if isEmptyBlock(then) && alt == nil {
		c.add(n, cond, "medium", "bug", "Empty if block", "The condition is evaluated and nothing happens: the handling was removed or never written.", "Add the missing handling, or delete the if.")
	} else if isEmptyBlock(then) {
		c.add(n, cond, "low", "design", "Empty if branch", "Only the else does anything.", "Invert the condition and drop the empty branch.")
	}
	if isEmptyBlock(alt) {
		c.add(alt, alt, "low", "design", "Empty else block", "The else branch does nothing.", "Delete it, or add the missing handling.")
	}
	if alt != nil && then != nil && normalize(f.text(then)) == normalize(f.text(alt)) && !isEmptyBlock(then) {
		c.add(n, cond, "high", "bug", "Both branches of this if do the same thing", "The condition has no effect, so one branch was probably meant to differ.", "Correct the branch that is wrong, or remove the condition.")
	}
	if then != nil && alt != nil {
		a, b := singleReturn(f, then), singleReturn(f, alt)
		if (a == "true" && b == "false") || (a == "false" && b == "true") {
			neg := ""
			if a == "false" {
				neg = "!"
			}
			c.add(n, cond, "low", "style", "Returning a boolean through an if", "The if only turns the condition into true or false.", "return "+neg+"("+text+");")
		}
	}
}

func findBinary(n *sitter.Node) *sitter.Node {
	for _, c := range named(n) {
		if c.Type() == "binary_expression" {
			return c
		}
	}
	return nil
}

func normalize(s string) string { return strings.Join(strings.Fields(s), " ") }

func singleReturn(f *file, n *sitter.Node) string {
	if n.Type() == "block" {
		kids := named(n)
		if len(kids) != 1 {
			return ""
		}
		n = kids[0]
	}
	if n.Type() != "return_statement" || len(named(n)) != 1 {
		return ""
	}
	return f.text(named(n)[0])
}

func (c *checker) unreachable(block *sitter.Node) {
	kids := named(block)
	for i, k := range kids {
		switch k.Type() {
		case "return_statement", "throw_statement", "break_statement", "continue_statement":
			for _, next := range kids[i+1:] {
				if next.Type() == "line_comment" || next.Type() == "block_comment" {
					continue
				}
				c.add(next, next, "high", "bug", "Unreachable code", "This line can never run: the "+strings.TrimSuffix(k.Type(), "_statement")+" on line "+fmt.Sprint(startLine(k))+" always leaves first. (javac rejects most of these, so check that the file is compiled.)", "Delete it, or move it before the "+strings.TrimSuffix(k.Type(), "_statement")+".")
				return
			}
		}
	}
}

// unusedLocals finds local variables that are declared and never read or written again.
func (c *checker) unusedLocals(m *sitter.Node) {
	f := c.f
	body := m.ChildByFieldName("body")
	if body == nil {
		return
	}
	for _, decl := range findAll(body, "local_variable_declaration") {
		for _, v := range named(decl) {
			if v.Type() != "variable_declarator" {
				continue
			}
			nameNode := v.ChildByFieldName("name")
			name := f.text(nameNode)
			if name == "" || name == "_" {
				continue
			}
			uses := 0
			walk(body, func(n *sitter.Node) bool {
				if n.Type() == "identifier" && f.text(n) == name && n.StartByte() != nameNode.StartByte() {
					uses++
				}
				return true
			})
			if uses == 0 {
				c.add(v, v, "low", "bug", "Unused local variable "+name, name+" is assigned and never used. Either the value was meant to be used (a real bug) or the line is dead.", "Use it where it was meant to go, or delete it.")
			}
		}
	}
}

// Fields filled by frameworks and methods called by frameworks are in use even with no caller here.
var frameworkAnnotations = regexp.MustCompile(`^(Autowired|Inject|Value|Resource|Column|Id|JoinColumn|ManyToOne|OneToMany|OneToOne|ManyToMany|Version|Embedded|EmbeddedId|Mock|MockBean|MockitoBean|InjectMocks|Spy|JsonProperty|PersistenceContext|LocalServerPort)$`)
var lombokAccessors = []string{"Data", "Getter", "Setter", "Value", "Builder", "AllArgsConstructor", "RequiredArgsConstructor"}

func (c *checker) unusedMembers(t *sitter.Node) {
	f := c.f
	body := t.ChildByFieldName("body")
	if body == nil || c.anyAnnotation(t, lombokAccessors) || f.hasAnnotation(t, "Entity") {
		return
	}
	count := func(name string, skip *sitter.Node) int {
		uses := 0
		walk(body, func(n *sitter.Node) bool {
			if (n.Type() == "identifier" || n.Type() == "field_identifier") && f.text(n) == name && (skip == nil || n.StartByte() != skip.StartByte()) {
				uses++
			}
			return true
		})
		return uses
	}
	for _, fd := range membersOfType(t, "field_declaration") {
		if !hasModifier(fd, "private") {
			continue
		}
		framework := false
		for _, a := range f.annotationNames(fd) {
			framework = framework || frameworkAnnotations.MatchString(a)
		}
		if framework {
			continue
		}
		for _, v := range named(fd) {
			if v.Type() != "variable_declarator" {
				continue
			}
			nameNode := v.ChildByFieldName("name")
			name := f.text(nameNode)
			if name != "serialVersionUID" && !logReceiver.MatchString(name) && count(name, nameNode) == 0 {
				c.add(fd, fd, "low", "design", "Unused private field "+name, name+" is declared and never used in this class.", "Delete it, or use it where it was meant to go.")
			}
		}
	}
	for _, m := range membersOfType(t, "method_declaration") {
		if !hasModifier(m, "private") || len(f.annotationNames(m)) > 0 {
			continue
		}
		nameNode := m.ChildByFieldName("name")
		name := f.text(nameNode)
		if count(name, nameNode) == 0 && !strings.Contains(string(f.src), "::"+name) {
			c.add(m, m, "low", "design", "Unused private method "+name+"()", name+"() is private and nothing in this class calls it, so it can never run.", "Delete it, or call it where it was meant to run.")
		}
	}
}

func hasMainMethod(t *sitter.Node) bool {
	for _, m := range findAll(t, "method_declaration") {
		if n := m.ChildByFieldName("name"); n != nil && hasModifier(m, "static") && n.EndByte()-n.StartByte() == 4 {
			return true
		}
	}
	return false
}

// Cost is a static estimate of what a method costs to run and to maintain.
type Cost struct {
	Complexity int    `json:"complexity"`  // cyclomatic: branches + 1
	LoopDepth  int    `json:"loopDepth"`   // deepest loop nesting
	DBCalls    int    `json:"dbCalls"`     // repository / JDBC calls
	Remote     int    `json:"remoteCalls"` // HTTP, Feign and other remote calls
	LoopIO     int    `json:"loopIo"`      // of those, how many sit inside a loop
	Params     int    `json:"params"`
	Lines      int    `json:"lines"`
	Estimate   string `json:"estimate"` // e.g. O(n²), O(n) database calls
}

var (
	dbReceiver  = regexp.MustCompile(`(?i)(repository|repo|dao|jdbctemplate|entitymanager)$|^em$`) // "em" alone: item and system are not databases
	dbMethod    = regexp.MustCompile(`^(find|save|delete|exists|count|get(One|ReferenceById)|query|update|persist|merge|execute|createQuery|createNativeQuery)`)
	loopTypes   = map[string]bool{"for_statement": true, "enhanced_for_statement": true, "while_statement": true, "do_statement": true}
	branchTypes = map[string]bool{"if_statement": true, "for_statement": true, "enhanced_for_statement": true, "while_statement": true, "do_statement": true,
		"catch_clause": true, "ternary_expression": true, "switch_label": true}
	streamLoops = map[string]bool{"forEach": true, "map": true, "filter": true, "flatMap": true, "anyMatch": true, "allMatch": true, "noneMatch": true}
)

// MethodCost measures one method.
// runsOnce reports whether kid is evaluated once however many items its parent loops over: what a
// for-each iterates, a for loop's setup, or what a call is made on. repo.findAll().stream().map(f)
// queries once, and a chain of stream steps is one pass rather than nested loops.
func runsOnce(n, kid *sitter.Node) bool {
	field := ""
	switch n.Type() {
	case "enhanced_for_statement":
		field = "value"
	case "for_statement":
		field = "init"
	case "method_invocation":
		field = "object"
	default:
		return false
	}
	once := n.ChildByFieldName(field)
	return once != nil && once.Equal(kid)
}

func MethodCost(f *file, m *sitter.Node) Cost {
	cost := Cost{Complexity: 1, Lines: endLine(m) - startLine(m) + 1}
	if ps := m.ChildByFieldName("parameters"); ps != nil {
		cost.Params = len(named(ps))
	}
	body := m.ChildByFieldName("body")
	if body == nil {
		cost.Estimate = "abstract"
		return cost
	}
	var visit func(n *sitter.Node, depth int)
	visit = func(n *sitter.Node, depth int) {
		t := n.Type()
		if branchTypes[t] {
			cost.Complexity++
		}
		if t == "binary_expression" {
			if op := f.text(n.ChildByFieldName("operator")); op == "&&" || op == "||" {
				cost.Complexity++
			}
		}
		inner := depth
		if loopTypes[t] {
			inner++
		}
		if t == "method_invocation" {
			name := f.text(n.ChildByFieldName("name"))
			obj := f.text(n.ChildByFieldName("object"))
			if streamLoops[name] && strings.Contains(obj, "stream()") {
				inner++
			}
			if i := strings.LastIndex(obj, "."); i >= 0 {
				obj = obj[i+1:]
			}
			io := false
			if dbReceiver.MatchString(obj) && dbMethod.MatchString(name) {
				cost.DBCalls++
				io = true
			} else if obj != "" && (remoteObject.MatchString(obj) || remoteMethods[name] && remoteObject.MatchString(obj)) {
				cost.Remote++
				io = true
			}
			if io && depth > 0 {
				cost.LoopIO++
			}
		}
		cost.LoopDepth = max(cost.LoopDepth, inner)
		for _, k := range named(n) {
			if runsOnce(n, k) {
				visit(k, depth)
			} else {
				visit(k, inner)
			}
		}
	}
	visit(body, 0)
	switch {
	case cost.LoopIO > 0:
		cost.Estimate = "O(n) round trips"
	case cost.LoopDepth >= 3:
		cost.Estimate = "O(n³)"
	case cost.LoopDepth == 2:
		cost.Estimate = "O(n²)"
	case cost.LoopDepth == 1:
		cost.Estimate = "O(n)"
	default:
		cost.Estimate = "O(1)"
	}
	return cost
}

func (c *checker) cost(m *sitter.Node) {
	f := c.f
	cost := MethodCost(f, m)
	name := f.text(m.ChildByFieldName("name"))
	body := m.ChildByFieldName("body")
	if body == nil {
		return
	}
	if cost.Complexity > 12 {
		c.add(m, m, "medium", "design", fmt.Sprintf("High complexity: %s() has %d paths", name, cost.Complexity), fmt.Sprintf("Cyclomatic complexity %d (10 is a common limit). Each path needs its own test, and bugs hide in the rarely taken ones.", cost.Complexity), "Extract the branches into named methods, or replace condition chains with a map or polymorphism.")
	}
	if cost.Lines > 60 {
		c.add(m, m, "low", "design", fmt.Sprintf("Long method: %s() is %d lines", name, cost.Lines), "Long methods usually do several jobs, which makes them hard to test and to change safely.", "Split it by responsibility.")
	}
	if cost.Params > 5 {
		c.add(m, m, "low", "design", fmt.Sprintf("Too many parameters: %s() takes %d", name, cost.Params), "Long parameter lists are easy to call in the wrong order, especially with several of the same type.", "Group related parameters into a record.")
	}
	if cost.LoopDepth >= 2 {
		c.add(m, m, "medium", "performance", fmt.Sprintf("Nested loops: %s() is %s", name, cost.Estimate), "Time grows with the square of the input: 100 items take 10,000 steps, 10,000 items take 100 million.", "Index one side in a Map or Set first, which makes it O(n).")
	}
	// Database calls per item.
	var inLoop func(n *sitter.Node, loop *sitter.Node)
	reported := map[string]bool{}
	inLoop = func(n, outer *sitter.Node) {
		loop := outer
		if loopTypes[n.Type()] {
			loop = n
		}
		if n.Type() == "method_invocation" {
			method := f.text(n.ChildByFieldName("name"))
			obj := f.text(n.ChildByFieldName("object"))
			if streamLoops[method] && strings.Contains(obj, "stream()") && loop == nil {
				loop = n
			}
			if i := strings.LastIndex(obj, "."); i >= 0 {
				obj = obj[i+1:]
			}
			if loop != nil && dbReceiver.MatchString(obj) && dbMethod.MatchString(method) && !reported[obj+method] {
				reported[obj+method] = true
				c.add(n, n, "high", "performance", fmt.Sprintf("Database call in a loop: %s.%s()", obj, method), "One query per item: 10 rows in a test, 10,000 round trips in production, all holding a connection.", "Fetch everything at once (findAllById, a JOIN or an IN query) and write with saveAll and a JDBC batch size.")
			}
			if loop != nil && method == "contains" && strings.Contains(strings.ToLower(obj), "list") {
				c.add(n, n, "medium", "performance", "Lookup in a list inside a loop: "+obj+".contains()", "List.contains scans the whole list each time, which makes the loop O(n²).", "Copy the list into a HashSet once before the loop; each lookup is then O(1).")
			}
		}
		for _, k := range named(n) {
			if runsOnce(n, k) {
				inLoop(k, outer)
			} else {
				inLoop(k, loop)
			}
		}
	}
	inLoop(body, nil)
}

// betterMethod suggests a clearer or cheaper call that does the same thing.
func (c *checker) betterMethod(call *sitter.Node) {
	f := c.f
	name := f.text(call.ChildByFieldName("name"))
	obj := f.text(call.ChildByFieldName("object"))
	parent := call.Parent()
	better := func(from, to, why string) {
		c.add(call, call, "low", "style", "Better method: "+to, from+": "+why, to)
	}
	switch {
	case name == "size" && parent != nil && parent.Type() == "binary_expression":
		other := f.text(parent.ChildByFieldName("right"))
		op := f.text(parent.ChildByFieldName("operator"))
		if other == "0" && (op == "==" || op == ">" || op == "!=") {
			if op == "==" {
				better(f.text(parent), obj+".isEmpty()", "says what it means, and is never slower")
			} else {
				better(f.text(parent), "!"+obj+".isEmpty()", "says what it means, and is never slower")
			}
		}
	case name == "isPresent" && parent != nil && parent.Type() == "unary_expression":
		better(f.text(parent), obj+".isEmpty()", "Optional has isEmpty since Java 11")
	case name == "count" && strings.HasSuffix(obj, ")") && parent != nil && parent.Type() == "binary_expression" && strings.Contains(obj, ".filter("):
		better(f.text(parent), "stream.anyMatch(...)", "anyMatch stops at the first match; filter + count walks everything")
	case name == "findFirst" && strings.Contains(obj, ".sorted("):
		better("sorted(...).findFirst()", "stream.min(comparator)", "min is O(n); sorting everything to take one is O(n log n)")
	case name == "getMessage" && parent != nil && parent.Type() == "argument_list" && parent.Parent() != nil && parent.Parent().Type() == "object_creation_expression" && strings.Contains(f.text(parent.Parent().ChildByFieldName("type")), "Exception") && !strings.Contains(f.text(parent), ", "):
		better("new …Exception(e.getMessage())", "new …Exception(message, e)", "keep the cause: otherwise the original stack trace is lost")
	case name == "printStackTrace":
		// reported by consoleOutput
	case name == "replaceAll" && len(args(call)) == 2 && args(call)[0].Type() == "string_literal" && !strings.ContainsAny(f.text(args(call)[0]), `\.[]()*+?^$|{}`):
		better("replaceAll with a plain string", obj+".replace(...)", "replaceAll compiles a regex; replace does not need one")
	}
}

var logMethods = map[string]bool{"trace": true, "debug": true, "info": true, "warn": true, "error": true}

// betterLog suggests logs that are cheaper and useful when something goes wrong.
func (c *checker) betterLog(call *sitter.Node) {
	f := c.f
	obj := f.text(call.ChildByFieldName("object"))
	level := f.text(call.ChildByFieldName("name"))
	if !logReceiver.MatchString(obj) || !logMethods[level] {
		return
	}
	a := args(call)
	if len(a) == 0 {
		return
	}
	if a[0].Type() == "binary_expression" {
		c.add(call, call, "low", "logging", "Better log: use {} placeholders", "`"+oneLineOf(f.text(a[0]))+"` builds the message even when "+level+" is off, and hides the values from structured logging.", `log.`+level+`("… {}", value);`)
	}
	// An error logged in a catch without the exception loses the stack trace.
	if level == "error" || level == "warn" {
		for p := call.Parent(); p != nil; p = p.Parent() {
			if p.Type() == "catch_clause" {
				param := childOfType(p, "catch_formal_parameter")
				if param == nil {
					break
				}
				ex := f.text(param.ChildByFieldName("name"))
				passed := false
				for _, x := range a[1:] {
					passed = passed || f.text(x) == ex
				}
				if ex != "" && !passed {
					c.add(call, call, "medium", "logging", "Better log: pass the exception", "The catch logs a message but not "+ex+", so the stack trace and the real cause are lost.", `log.`+level+`("what failed and for which id", `+ex+`);`)
				}
				break
			}
		}
	}
	if (level == "error" || level == "warn") && len(a) == 1 && a[0].Type() == "string_literal" && len(strings.Fields(f.text(a[0]))) <= 2 {
		c.add(call, call, "low", "logging", "Better log: say what failed and for what", f.text(a[0])+" will not tell anyone which request or record failed at 3 am.", `log.`+level+`("charging order {} failed", orderId);`)
	}
}

var (
	secretName  = regexp.MustCompile(`(?i)(passw(or)?d|secret|api_?key|access_?key|private_?key|auth_?token|credential)`)
	notAValue   = regexp.MustCompile(`(?i)(header|param|prop(erty)?|field|name|prefix|path|url|env|label)$`)
	secretValue = regexp.MustCompile(`^(sk_(live|test)_|pk_live_|AKIA[0-9A-Z]{16}|gh[pousr]_[A-Za-z0-9]{20}|xox[bpars]-|-----BEGIN )`)
	boxedType   = map[string]bool{"String": true, "Long": true, "Integer": true, "Short": true, "Byte": true, "Double": true, "Float": true, "Boolean": true, "Character": true, "BigDecimal": true, "BigInteger": true}
)

// hardcodedSecret reports a credential written into the code.
func (c *checker) hardcodedSecret(n *sitter.Node) {
	value := n.ChildByFieldName("value")
	if value == nil || value.Type() != "string_literal" {
		return
	}
	f := c.f
	name := f.text(n.ChildByFieldName("name"))
	literal := strings.Trim(f.text(value), `"`)
	known := secretValue.MatchString(literal)
	named := secretName.MatchString(name) && !notAValue.MatchString(name) && len(literal) >= 6 &&
		!strings.ContainsAny(literal, " ${}") // a sentence or a placeholder is not a credential
	if known || named {
		c.add(n, n, "critical", "security", "Hard-coded secret: "+name,
			name+" holds a credential in the source code. Anyone who can read the repository or the jar can use it, and it stays in git history even after it is removed.",
			"Rotate it, then read it from the environment or a secret store.")
	}
}

// referenceEquality reports == and != on objects, which compares identity and not value. It works
// for small boxed numbers because of the cache, so it passes tests and fails in production.
func (c *checker) referenceEquality(n *sitter.Node) {
	f := c.f
	op := f.text(n.ChildByFieldName("operator"))
	if op != "==" && op != "!=" {
		return
	}
	left, right := n.ChildByFieldName("left"), n.ChildByFieldName("right")
	if left == nil || right == nil || left.Type() == "null_literal" || right.Type() == "null_literal" {
		return
	}
	negate := ""
	if op == "!=" {
		negate = "!"
	}
	if left.Type() == "string_literal" || right.Type() == "string_literal" {
		c.add(n, n, "high", "bug", "String compared with "+op,
			f.text(n)+" compares references, not text, so equal strings from a database or a request do not match.",
			"Use "+negate+"\"text\".equals(value), which is also safe when value is null.")
		return
	}
	typeOf := func(side *sitter.Node) string {
		if side.Type() != "method_invocation" {
			return ""
		}
		return c.ctx.Returns[f.text(side.ChildByFieldName("name"))]
	}
	l, r := typeOf(left), typeOf(right)
	if boxedType[l] && l == r {
		c.add(n, n, "high", "bug", l+" compared with "+op,
			f.text(n)+" compares two "+l+" objects by reference. Small numbers can appear to work because of the cache, then larger values stop matching.",
			"Use Objects.equals(a, b).")
	}
}

// DataOnly says a Java file holds no behaviour: fields, getters, setters and assignments. The rules
// see everything that can go wrong in such a file, so it is not worth a model call.
func DataOnly(src []byte) bool {
	f, err := parse(src)
	if err != nil || f.root.HasError() {
		return false
	}
	if strings.Contains(string(src), "@Query") {
		return false // SQL in an annotation is real logic
	}
	for _, typ := range []string{"method_declaration", "constructor_declaration", "compact_constructor_declaration"} {
		for _, m := range findAll(f.root, typ) {
			body := m.ChildByFieldName("body")
			if body == nil {
				continue // an interface method: no behaviour
			}
			for _, typ := range []string{"method_invocation", "object_creation_expression", "if_statement", "for_statement", "enhanced_for_statement",
				"while_statement", "try_statement", "switch_expression", "throw_statement", "ternary_expression", "lambda_expression"} {
				if len(findAll(body, typ)) > 0 {
					return false
				}
			}
		}
	}
	return true
}
