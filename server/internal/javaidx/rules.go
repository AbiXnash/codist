package javaidx

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

var (
	beanAnnotations      = []string{"Service", "Component", "RestController", "Controller", "Repository"}
	injectionAnnotations = []string{"Autowired", "Inject", "Resource", "Value", "PersistenceContext"}
	sqlCalls             = map[string]bool{
		"update": true, "query": true, "queryForObject": true, "queryForList": true, "queryForMap": true,
		"execute": true, "batchUpdate": true, "createQuery": true, "createNativeQuery": true,
		"prepareStatement": true, "executeQuery": true, "executeUpdate": true,
	}
	optionalSource = regexp.MustCompile(`^(find\w*|\w*Optional\w*|\w*ById|findFirst|findAny|max|min|reduce)$`)
	optionalVar    = regexp.MustCompile(`^(opt|maybe)\w*$`)
)

// Constant is a static final field with a literal value, e.g. DataConstant.LOG_PREFIX = "CUSTOMER".
type Constant struct {
	Owner string // fully qualified name of the declaring type
	Value string // literal as written, quotes included
}

// Context is what the rules know about the rest of the project.
type Context struct {
	Path      string
	IsEntity  func(simpleName string) bool
	Constants map[string]Constant // keyed by "Type.FIELD" (simple type name)
	Domains   map[string]bool     // business areas: package segments such as customer, product, order
	Beans     map[string]string   // simple type name -> stereotype, for project types
}

// ProjectContext derives rule context from the graph.
func ProjectContext(g *Graph) Context {
	ctx := Context{Constants: map[string]Constant{}, Domains: map[string]bool{}, Beans: map[string]string{}}
	entities := map[string]bool{}
	pkgs := map[string]bool{}
	for _, n := range g.Nodes {
		if n.Stereotype == "entity" {
			entities[n.Name] = true
		}
		pkgs[n.Pkg] = true
		ctx.Beans[n.Name] = n.Stereotype
		for _, m := range n.Members {
			if m.Value != "" {
				ctx.Constants[n.Name+"."+m.Name] = Constant{Owner: n.ID, Value: m.Value}
			}
		}
	}
	prefix := commonPackage(pkgs)
	for p := range pkgs {
		for _, seg := range strings.Split(strings.TrimPrefix(strings.TrimPrefix(p, prefix), "."), ".") {
			if seg != "" && !genericPackages[seg] {
				ctx.Domains[strings.ToLower(seg)] = true
			}
		}
	}
	ctx.IsEntity = func(s string) bool { return entities[s] }
	return ctx
}

var genericPackages = map[string]bool{
	"common": true, "shared": true, "core": true, "config": true, "configuration": true, "util": true, "utils": true,
	"web": true, "api": true, "rest": true, "controller": true, "controllers": true, "service": true, "services": true,
	"repository": true, "repositories": true, "model": true, "models": true, "domain": true, "entity": true, "entities": true,
	"dto": true, "dtos": true, "exception": true, "exceptions": true, "infra": true, "infrastructure": true, "impl": true,
	"security": true, "support": true, "internal": true, "constants": true, "app": true, "application": true,
}

func commonPackage(pkgs map[string]bool) string {
	var parts [][]string
	for p := range pkgs {
		parts = append(parts, strings.Split(p, "."))
	}
	if len(parts) < 2 {
		return ""
	}
	var out []string
	for i := 0; ; i++ {
		if i >= len(parts[0]) {
			break
		}
		seg := parts[0][i]
		for _, p := range parts[1:] {
			if i >= len(p) || p[i] != seg {
				return strings.Join(out, ".")
			}
		}
		out = append(out, seg)
	}
	return strings.Join(out, ".")
}

// Check runs the rule checks on one Java file and keeps findings that touch a changed line.
func Check(src []byte, changed map[int]bool, ctx Context) []RuleFinding {
	f, err := parse(src)
	if err != nil {
		return nil
	}
	c := &checker{f: f, ctx: ctx, isEntity: ctx.IsEntity}
	test := strings.Contains(ctx.Path, "src/test/")

	for _, call := range findAll(f.root, "method_invocation") {
		c.optionalGet(call)
		c.sqlConcatenation(call)
		if !test {
			c.consoleOutput(call)
		}
	}
	for _, t := range named(f.root) {
		if _, ok := typeDecls[t.Type()]; ok {
			c.foreignDomainConstants(t)
		}
	}
	c.loops(f.root)
	for _, n := range findAll(f.root, "object_creation_expression") {
		c.bigDecimalFromDouble(n)
	}
	for _, n := range findAll(f.root, "catch_clause") {
		c.emptyCatch(n)
	}
	for _, t := range findAll(f.root, "class_declaration") {
		c.transactional(t)
		if c.anyAnnotation(t, beanAnnotations) || f.hasAnnotation(t, "Configuration") {
			c.mutableBeanState(t)
			c.fieldInjection(t)
			c.uninjectedDependencies(t)
		}
		c.literalsDuplicatingConstants(t)
		c.duplicateLogs(t)
		c.logHelpers(t)
		if f.hasAnnotation(t, "RestController") || f.hasAnnotation(t, "Controller") {
			c.controller(t)
		}
	}

	var out []RuleFinding
	for _, x := range c.out {
		for l := x.Line; l <= x.EndLine; l++ {
			if changed[l] {
				out = append(out, x)
				break
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out
}

type checker struct {
	f        *file
	ctx      Context
	isEntity func(string) bool
	out      []RuleFinding
}

// consoleOutput flags System.out / System.err printing and printStackTrace in production code.
func (c *checker) consoleOutput(call *sitter.Node) {
	f := c.f
	name := f.text(call.ChildByFieldName("name"))
	obj := call.ChildByFieldName("object")
	if obj == nil {
		return
	}
	stream := f.text(obj)
	switch {
	case (stream == "System.out" || stream == "System.err") && (name == "println" || name == "print" || name == "printf"):
		msg := "log.info(…)"
		if a := args(call); len(a) == 1 {
			msg = "log.info(" + c.placeholders(a[0]) + ")"
		}
		c.add(call, call, "low", "style", "Use a logger instead of "+stream+"."+name,
			stream+" bypasses log levels, formatting and log shipping, and string concatenation runs even when nobody reads it.",
			"private static final Logger log = LoggerFactory.getLogger("+c.enclosingType(call)+".class);\n"+msg+";")
	case name == "printStackTrace" && len(args(call)) == 0:
		c.add(call, call, "low", "style", "printStackTrace() instead of logging",
			"The stack trace goes to stderr without context or log level.",
			"log.error(\"…\", "+collapse(f.text(obj))+");")
	}
}

// enclosingType is the simple name of the type declaring n.
func (c *checker) enclosingType(n *sitter.Node) string {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if _, ok := typeDecls[p.Type()]; ok {
			return c.f.text(p.ChildByFieldName("name"))
		}
	}
	return "getClass()"
}

// placeholders turns "a" + x + "b" + y into "a{}b{}", x, y for a parameterised log call.
func (c *checker) placeholders(arg *sitter.Node) string {
	var parts []*sitter.Node
	var flatten func(n *sitter.Node)
	flatten = func(n *sitter.Node) {
		if n.Type() == "binary_expression" && c.f.text(n.ChildByFieldName("operator")) == "+" {
			flatten(n.ChildByFieldName("left"))
			flatten(n.ChildByFieldName("right"))
			return
		}
		parts = append(parts, n)
	}
	flatten(arg)
	var format strings.Builder
	var values []string
	for _, p := range parts {
		if p.Type() == "string_literal" {
			format.WriteString(strings.Trim(c.f.text(p), `"`))
		} else {
			format.WriteString("{}")
			values = append(values, c.f.text(p))
		}
	}
	if len(values) == 0 {
		return c.f.text(arg)
	}
	return `"` + format.String() + `", ` + strings.Join(values, ", ")
}

// foreignDomainConstants flags a constant whose value names one business area (e.g. "CUSTOMER")
// used in a class that belongs to another (e.g. ProductService): a typical copy-paste slip.
func (c *checker) foreignDomainConstants(t *sitter.Node) {
	f := c.f
	if len(c.ctx.Constants) == 0 || len(c.ctx.Domains) == 0 {
		return
	}
	typeName := f.text(t.ChildByFieldName("name"))
	own := c.domainsOf(c.ctx.Path + " " + typeName)
	if len(own) == 0 {
		return
	}
	walk(t, func(n *sitter.Node) bool {
		if n.Type() != "field_access" {
			return true
		}
		obj := n.ChildByFieldName("object")
		if obj == nil || obj.Type() != "identifier" {
			return true
		}
		ref := f.text(obj) + "." + f.text(n.ChildByFieldName("field"))
		k, ok := c.ctx.Constants[ref]
		if !ok || !strings.HasPrefix(k.Value, `"`) {
			return true
		}
		named := c.domainsOf(k.Value)
		for d := range named {
			if own[d] {
				return true // the value matches where it is used
			}
		}
		for d := range named {
			ownList := make([]string, 0, len(own))
			for o := range own {
				ownList = append(ownList, o)
			}
			sort.Strings(ownList)
			c.add(n, n, "medium", "bug", fmt.Sprintf("%s is %s but used in %s", ref, k.Value, typeName),
				fmt.Sprintf("%s refers to the %q area, while %s belongs to %q. Output, keys or routing tagged with it will be attributed to the wrong area. This is usually a copy-paste slip.",
					ref, d, typeName, strings.Join(ownList, ", ")),
				fmt.Sprintf("Use a constant for this area, e.g. %s.%s_%s = \"%s\", or derive the prefix from the class: getClass().getSimpleName().",
					f.text(obj), strings.ToUpper(ownList[0]), f.text(n.ChildByFieldName("field")), strings.ToUpper(ownList[0])))
			break
		}
		return true
	})
}

var wordSplit = regexp.MustCompile(`[A-Z]?[a-z]+|[A-Z]+(?:[^a-z]|$)|[a-z]+`)

// domainsOf finds the project's business areas mentioned in a path, name or literal.
func (c *checker) domainsOf(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range regexp.MustCompile(`[A-Za-z]+`).FindAllString(s, -1) {
		for _, part := range wordSplit.FindAllString(w, -1) {
			p := strings.ToLower(strings.Trim(part, "_"))
			if c.ctx.Domains[p] {
				out[p] = true
			} else if strings.HasSuffix(p, "s") && c.ctx.Domains[strings.TrimSuffix(p, "s")] {
				out[strings.TrimSuffix(p, "s")] = true
			}
		}
	}
	return out
}

// add anchors a finding on `at`, spanning to the end of `span` (capped for long declarations).
func (c *checker) add(at, span *sitter.Node, severity, category, title, detail, suggestion string) {
	line, end := startLine(at), endLine(span)
	if end < line {
		end = line
	}
	switch span.Type() {
	case "method_declaration", "field_declaration", "class_declaration":
		end = min(end, line+3)
	}
	c.out = append(c.out, RuleFinding{ID: ruleID(title), Line: line, EndLine: end, Severity: severity, Category: category,
		Title: title, Detail: detail, Suggestion: suggestion})
}

func (c *checker) anyAnnotation(n *sitter.Node, names []string) bool {
	a := c.f.annotations(n)
	for _, name := range names {
		if _, ok := a[name]; ok {
			return true
		}
	}
	return false
}

func args(call *sitter.Node) []*sitter.Node {
	return named(call.ChildByFieldName("arguments"))
}

func (c *checker) optionalGet(call *sitter.Node) {
	f := c.f
	obj := call.ChildByFieldName("object")
	if f.text(call.ChildByFieldName("name")) != "get" || obj == nil || len(args(call)) != 0 {
		return
	}
	looksOptional := obj.Type() == "method_invocation" && optionalSource.MatchString(f.text(obj.ChildByFieldName("name"))) ||
		strings.Contains(strings.ToLower(f.text(obj)), "optional") ||
		obj.Type() == "identifier" && optionalVar.MatchString(f.text(obj))
	if !looksOptional {
		return
	}
	c.add(call, call, "high", "bug", "Optional.get() without a presence check",
		"If nothing is found this throws NoSuchElementException, which surfaces as an HTTP 500 instead of a clear error.",
		collapse(f.text(obj))+".orElseThrow(() -> new NotFoundException(\"…\"))")
}

func (c *checker) sqlConcatenation(call *sitter.Node) {
	f := c.f
	if !sqlCalls[f.text(call.ChildByFieldName("name"))] {
		return
	}
	a := args(call)
	if len(a) == 0 {
		return
	}
	sql := a[0]
	concatenated := sql.Type() == "binary_expression" && f.text(sql.ChildByFieldName("operator")) == "+" &&
		len(findAll(sql, "string_literal")) > 0 && len(findAll(sql, "identifier")) > 0
	formatted := sql.Type() == "method_invocation" && (f.text(sql.ChildByFieldName("name")) == "formatted" ||
		f.text(sql.ChildByFieldName("object")) == "String" && f.text(sql.ChildByFieldName("name")) == "format")
	if concatenated || formatted {
		c.add(call, call, "critical", "security", "SQL built by string concatenation",
			"Values concatenated into SQL/JPQL allow injection and defeat statement caching. Use bind parameters.",
			"jdbc.update(\"UPDATE … WHERE id = ?\", id);  // or :named parameters in a JPA query")
	}
}

func (c *checker) bigDecimalFromDouble(n *sitter.Node) {
	f := c.f
	if f.baseType(n.ChildByFieldName("type")) != "BigDecimal" {
		return
	}
	a := args(n)
	if len(a) != 1 || a[0].Type() != "decimal_floating_point_literal" {
		return
	}
	lit := f.text(a[0])
	clean := strings.TrimRight(lit, "dDfF")
	c.add(n, n, "medium", "bug", "new BigDecimal(double) is imprecise",
		fmt.Sprintf("new BigDecimal(%s) stores the exact binary value of the double, not %s (0.1 becomes 0.1000000000000000055…), which leaks into money calculations.", lit, clean),
		fmt.Sprintf("new BigDecimal(\"%s\")  // or BigDecimal.valueOf(%s)", clean, lit))
}

func (c *checker) emptyCatch(n *sitter.Node) {
	body := n.ChildByFieldName("body")
	if body == nil || body.NamedChildCount() != 0 { // comments count as named children, so a commented catch is fine
		return
	}
	typ := "Exception"
	if p := childOfType(n, "catch_formal_parameter"); p != nil {
		if t := childOfType(p, "catch_type"); t != nil {
			typ = c.f.text(t)
		}
	}
	c.add(n, n, "medium", "bug", "Exception swallowed by an empty catch block",
		"Failures disappear silently and the code carries on in an inconsistent state.",
		"catch ("+typ+" e) {\n    log.warn(\"…\", e);\n    throw e; // or handle it\n}")
}

func (c *checker) transactional(t *sitter.Node) {
	f := c.f
	tx := map[string]bool{}
	for _, m := range membersOfType(t, "method_declaration") {
		a, ok := f.annotations(m)["Transactional"]
		if !ok {
			continue
		}
		tx[f.text(m.ChildByFieldName("name"))] = true
		for _, kw := range []string{"private", "final", "static"} {
			if hasModifier(m, kw) {
				c.add(a, m, "high", "spring", "@Transactional on a "+kw+" method is ignored",
					"Spring applies transactions through a proxy, which cannot intercept "+kw+" methods. This code runs without a transaction.",
					"Make the method public and call it from another bean, or put @Transactional on the public entry point.")
				break
			}
		}
	}
	if len(tx) == 0 || f.hasAnnotation(t, "Transactional") {
		return
	}
	for _, caller := range membersOfType(t, "method_declaration") {
		if f.hasAnnotation(caller, "Transactional") {
			continue
		}
		callerName := f.text(caller.ChildByFieldName("name"))
		for _, call := range findAll(caller, "method_invocation") {
			obj := call.ChildByFieldName("object")
			name := f.text(call.ChildByFieldName("name"))
			if (obj == nil || obj.Type() == "this") && tx[name] {
				c.add(call, call, "high", "spring", "Self-invocation bypasses @Transactional",
					callerName+"() calls "+name+"() on 'this', so the transactional proxy is skipped and no transaction starts.",
					"Annotate "+callerName+"() with @Transactional, or move "+name+"() to another bean.")
			}
		}
	}
}

func (c *checker) mutableBeanState(t *sitter.Node) {
	f := c.f
	methods := membersOfType(t, "method_declaration")
	for _, fd := range membersOfType(t, "field_declaration") {
		if hasModifier(fd, "static") || hasModifier(fd, "final") || c.anyAnnotation(fd, injectionAnnotations) {
			continue
		}
		for _, v := range named(fd) {
			if v.Type() != "variable_declarator" {
				continue
			}
			name := f.text(v.ChildByFieldName("name"))
			if c.writtenIn(methods, name) {
				c.add(fd, fd, "high", "bug", "Mutable state in a singleton bean",
					"'"+name+"' is shared by every request thread. Concurrent updates race and lose writes.",
					"Remove the field and keep the state in the database, or use a thread-safe type such as AtomicInteger or LongAdder.")
			}
		}
	}
}

func (c *checker) writtenIn(methods []*sitter.Node, field string) bool {
	f := c.f
	targets := func(n *sitter.Node) bool {
		switch n.Type() {
		case "identifier":
			return f.text(n) == field
		case "field_access":
			obj := n.ChildByFieldName("object")
			return obj != nil && obj.Type() == "this" && f.text(n.ChildByFieldName("field")) == field
		}
		return false
	}
	for _, m := range methods {
		for _, a := range findAll(m, "assignment_expression") {
			if targets(a.ChildByFieldName("left")) {
				return true
			}
		}
		for _, u := range findAll(m, "update_expression") {
			for _, ch := range named(u) {
				if targets(ch) {
					return true
				}
			}
		}
	}
	return false
}

func (c *checker) fieldInjection(t *sitter.Node) {
	for _, fd := range membersOfType(t, "field_declaration") {
		if c.anyAnnotation(fd, []string{"Autowired", "Inject"}) {
			suggestion := "Declare the field private final and inject it through the constructor."
			typ, cls := c.f.text(fd.ChildByFieldName("type")), c.f.text(t.ChildByFieldName("name"))
			if d := fd.ChildByFieldName("declarator"); d != nil && typ != "" && cls != "" {
				name := c.f.text(d.ChildByFieldName("name"))
				suggestion = fmt.Sprintf("private final %s %s;\n\npublic %s(%s %s) {\n    this.%s = %s;\n}", typ, name, cls, typ, name, name, name)
			}
			c.add(fd, fd, "low", "spring", "Field injection",
				"Field injection hides dependencies, prevents final fields and makes unit testing harder. With constructor injection the field can be final, Spring fails fast when the bean is missing, and tests can pass a mock without reflection.",
				suggestion)
		}
	}
}

func (c *checker) controller(t *sitter.Node) {
	f := c.f
	for _, m := range membersOfType(t, "method_declaration") {
		mapped := false
		for name := range f.annotations(m) {
			if strings.HasSuffix(name, "Mapping") {
				mapped = true
			}
		}
		if !mapped {
			continue
		}
		for _, p := range named(m.ChildByFieldName("parameters")) {
			if p.Type() != "formal_parameter" {
				continue
			}
			if f.hasAnnotation(p, "RequestBody") && !f.hasAnnotation(p, "Valid") && !f.hasAnnotation(p, "Validated") {
				typ := f.text(p.ChildByFieldName("type"))
				c.add(p, p, "low", "spring", "@RequestBody is not validated",
					"Without @Valid, bean-validation constraints on "+typ+" are never checked.",
					"@Valid @RequestBody "+typ+" "+f.text(p.ChildByFieldName("name")))
			}
		}
		if rt := m.ChildByFieldName("type"); rt != nil && c.isEntity != nil {
			for _, id := range findAll(rt, "type_identifier") {
				if name := f.text(id); c.isEntity(name) {
					c.add(rt, rt, "medium", "design", "JPA entity "+name+" returned from a controller",
						"Serialising entities exposes internal fields, triggers lazy loading (N+1 or LazyInitializationException) and couples the API to the schema.",
						"Return a DTO, e.g. record "+name+"Response(...) mapped from the entity.")
					break
				}
			}
		}
	}
}

// ruleID maps a finding title to its knowledge-base id, so one ignore covers the rule and the model.
func ruleID(title string) string {
	for _, m := range []struct{ prefix, id string }{
		{"Optional.get()", "optional-get-unchecked"},
		{"SQL built", "sql-injection"},
		{"new BigDecimal(double)", "bigdecimal-double"},
		{"Exception swallowed", "swallowed-exception"},
		{"@Transactional on a", "tx-on-proxy-bypassed-method"},
		{"Self-invocation", "tx-self-invocation"},
		{"Mutable state", "singleton-mutable-state"},
		{"Field injection", "field-injection"},
		{"@RequestBody is not validated", "request-body-validation"},
		{"JPA entity", "entity-in-api"},
		{"Use a logger", "console-output"},
		{"printStackTrace", "console-output"},
		{"Logging in a loop", "logging-in-loop"},
		{"Remote call", "remote-call-in-loop"},
		{"Same message logged twice", "duplicate-log"},
	} {
		if strings.HasPrefix(title, m.prefix) {
			return m.id
		}
	}
	switch {
	case strings.Contains(title, " but used in "):
		return "copy-paste-wrong-constant"
	case strings.Contains(title, " is never injected"):
		return "uninjected-dependency"
	case strings.Contains(title, " duplicates constant "):
		return "literal-instead-of-constant"
	case strings.HasSuffix(title, " is a log-only helper"):
		return "log-helper-method"
	}
	return ""
}

var (
	logReceiver   = regexp.MustCompile(`^(?i)(log|logger|LOG|LOGGER|slf4jLogger)$`)
	loudLevels    = map[string]bool{"info": true, "warn": true, "error": true}
	remoteObject  = regexp.MustCompile(`(?i)(client|resttemplate|webclient|restclient|feign|gateway|api|http|template|stub|proxy)$`)
	remoteMethods = map[string]bool{"getForObject": true, "getForEntity": true, "postForObject": true, "postForEntity": true,
		"exchange": true, "retrieve": true, "execute": true, "send": true, "call": true, "invoke": true}
	repoObject = regexp.MustCompile(`(?i)(repo|repository|dao|jdbc|jdbctemplate|entitymanager|em)$`)
	repoMethod = regexp.MustCompile(`^(find|get|save|delete|exists|count|query|update|insert|persist|merge|load)`)
	loopCalls  = map[string]bool{"forEach": true, "map": true, "flatMap": true, "peek": true, "filter": true, "forEachOrdered": true}
)

// loops flags per-iteration remote calls and per-iteration logging: the shape of batch jobs such as
// payment reconciliation, where 10,000 records turn into 10,000 HTTP calls and 30,000 log lines.
func (c *checker) loops(root *sitter.Node) {
	f := c.f
	walk(root, func(n *sitter.Node) bool {
		var body *sitter.Node
		switch n.Type() {
		case "for_statement", "enhanced_for_statement", "while_statement", "do_statement":
			body = n.ChildByFieldName("body")
		case "method_invocation":
			if loopCalls[f.text(n.ChildByFieldName("name"))] {
				for _, a := range args(n) {
					if a.Type() == "lambda_expression" {
						body = a.ChildByFieldName("body")
					}
				}
			}
		}
		if body == nil {
			return true
		}

		var logs, loud int
		var remote, repo []string
		walk(body, func(m *sitter.Node) bool {
			// Nested loops are reported on their own.
			if m != body && (m.Type() == "for_statement" || m.Type() == "enhanced_for_statement" || m.Type() == "while_statement") {
				return false
			}
			if m.Type() != "method_invocation" {
				return true
			}
			name := f.text(m.ChildByFieldName("name"))
			obj := m.ChildByFieldName("object")
			objText := f.text(obj)
			last := objText[strings.LastIndex(objText, ".")+1:]
			switch {
			case obj != nil && logReceiver.MatchString(last) && (loudLevels[name] || name == "debug" || name == "trace"):
				logs++
				if loudLevels[name] {
					loud++
				}
			case objText == "System.out" || objText == "System.err":
				logs++
				loud++
			case obj != nil && (remoteMethods[name] || remoteObject.MatchString(last)) && !logReceiver.MatchString(last):
				remote = append(remote, collapse(objText)+"."+name+"()")
			case obj != nil && repoObject.MatchString(last) && repoMethod.MatchString(name):
				repo = append(repo, collapse(objText)+"."+name+"()")
			}
			return true
		})

		head := n
		if len(remote) > 0 || len(repo) > 0 {
			calls := append(remote, repo...)
			what := "HTTP"
			if len(remote) == 0 {
				what = "database"
			}
			c.addLoop(head, "high", "performance", fmt.Sprintf("Remote call per item: %s", strings.Join(dedupe(calls), ", ")),
				fmt.Sprintf("Each iteration makes a %s round trip. For 10,000 records that is 10,000 sequential calls: minutes of latency, rate limits, and a half-done batch if one call fails.", what),
				remoteLoopAdvice(len(remote) > 0))
		}
		if loud >= 2 || (loud >= 1 && len(remote)+len(repo) > 0) {
			c.addLoop(head, "medium", "logging", fmt.Sprintf("Logging in a loop: %d log statement(s) per iteration (%d at INFO or above)", logs, loud),
				"Per-item INFO logs multiply with the data: 10,000 records x 3 lines = 30,000 lines per run. They drown real problems, cost money in log storage, and slow the loop down.",
				loopLogAdvice)
		}
		return true
	})
}

// addLoop anchors on the loop header and spans the body, so any change inside the loop counts.
func (c *checker) addLoop(loop *sitter.Node, severity, category, title, detail, suggestion string) {
	c.out = append(c.out, RuleFinding{ID: ruleID(title), Line: startLine(loop), EndLine: endLine(loop),
		Severity: severity, Category: category, Title: title, Detail: detail, Suggestion: suggestion})
}

func dedupe(s []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range s {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

const loopLogAdvice = `Pick one or combine them:

// 1. Summarise: count outcomes in the loop, log once at the end
int matched = 0, mismatched = 0, failed = 0;
for (Payment p : payments) { ... matched++; }
log.info("Recon {}: {} matched, {} mismatched, {} failed in {} ms", runId, matched, mismatched, failed, ms);

// 2. Keep per-item detail at DEBUG (off in production)
log.debug("Payment {} -> {}", p.getId(), status);

// 3. Log only what needs action, with the ids someone will search for
if (!expected.equals(actual)) log.warn("Mismatch payment={} expected={} actual={}", p.getId(), expected, actual);

// 4. Collect problems (bounded) and log them in one line
List<String> mismatches = new ArrayList<>();
... mismatches.add(p.getId() + ":" + actual);
log.warn("{} mismatches, first 50: {}", mismatches.size(), mismatches.subList(0, Math.min(50, mismatches.size())));
// A StringBuilder works too, but keep it bounded: an unbounded one is a memory leak for large batches.`

func remoteLoopAdvice(http bool) string {
	if !http {
		return `// Load everything in one query instead of one per item
Map<Long, Account> accounts = accountRepo.findAllById(ids).stream()
        .collect(Collectors.toMap(Account::getId, a -> a));
for (Payment p : payments) { Account a = accounts.get(p.getAccountId()); ... }`
	}
	return `// 1. Prefer a batch endpoint: one call for many ids
List<Status> statuses = gateway.statuses(payments.stream().map(Payment::getId).toList());

// 2. Otherwise call in parallel with a bound, a timeout and per-item error handling
try (var pool = Executors.newVirtualThreadPerTaskExecutor()) {
    Semaphore limit = new Semaphore(10);   // respect the provider's rate limit
    List<Future<Result>> results = payments.stream().map(p -> pool.submit(() -> {
        limit.acquire();
        try { return gateway.status(p.getId()); } finally { limit.release(); }
    })).toList();
    ...
}

// 3. Process in chunks (e.g. 500) and record progress, so a failure can resume instead of restarting`
}

var dependencyType = regexp.MustCompile(`(Repository|Service|Client|Gateway|Template|Mapper|Publisher|Producer|Dao|DAO|Facade|Manager|Provider|Handler|Sender|Api|Validator|Factory)$`)

// uninjectedDependencies finds bean fields that nothing ever sets: no constructor parameter,
// no @Autowired/@Inject, no Lombok-generated constructor (which only covers final fields).
// Spring leaves them null, so the first call through them throws NullPointerException.
func (c *checker) uninjectedDependencies(t *sitter.Node) {
	f := c.f
	lombokAll := f.hasAnnotation(t, "AllArgsConstructor")
	lombokFinal := f.hasAnnotation(t, "RequiredArgsConstructor")
	assigned := map[string]bool{}
	for _, ctor := range membersOfType(t, "constructor_declaration") {
		for _, a := range findAll(ctor, "assignment_expression") {
			left := a.ChildByFieldName("left")
			if left == nil {
				continue
			}
			name := f.text(left)
			if left.Type() == "field_access" {
				name = f.text(left.ChildByFieldName("field"))
			}
			assigned[name] = true
		}
	}
	for _, m := range membersOfType(t, "method_declaration") {
		if c.anyAnnotation(m, []string{"Autowired", "Inject", "PostConstruct"}) {
			for _, a := range findAll(m, "assignment_expression") {
				if left := a.ChildByFieldName("left"); left != nil {
					name := f.text(left)
					if left.Type() == "field_access" {
						name = f.text(left.ChildByFieldName("field"))
					}
					assigned[name] = true
				}
			}
		}
	}
	for _, fd := range membersOfType(t, "field_declaration") {
		if hasModifier(fd, "static") || c.anyAnnotation(fd, injectionAnnotations) || c.anyAnnotation(fd, []string{"MockBean", "MockitoBean", "Mock"}) {
			continue
		}
		typ := f.baseType(fd.ChildByFieldName("type"))
		_, isBean := c.ctx.Beans[typ]
		if !isBean && !dependencyType.MatchString(typ) {
			continue
		}
		if st := c.ctx.Beans[typ]; st == "entity" || st == "dto" || st == "exception" {
			continue
		}
		if lombokAll || (lombokFinal && hasModifier(fd, "final")) {
			continue
		}
		for _, v := range named(fd) {
			if v.Type() != "variable_declarator" || v.ChildByFieldName("value") != nil {
				continue
			}
			name := f.text(v.ChildByFieldName("name"))
			if assigned[name] {
				continue
			}
			cls := f.text(t.ChildByFieldName("name"))
			c.add(fd, fd, "critical", "bug", fmt.Sprintf("%s is never injected: %s.%s stays null", typ, cls, name),
				fmt.Sprintf("No constructor parameter, @Autowired or Lombok constructor sets '%s', so Spring leaves it null and every method that uses it throws NullPointerException.", name),
				fmt.Sprintf("private final %s %s;\n\npublic %s(%s %s) {\n    this.%s = %s;\n}", typ, name, cls, typ, name, name, name))
		}
	}
}

// literalsDuplicatingConstants flags string literals that repeat the value of an existing constant
// (same class first, then project constants), e.g. "CUSTOMER" next to LOG_PREFIX = "CUSTOMER".
func (c *checker) literalsDuplicatingConstants(t *sitter.Node) {
	f := c.f
	local := map[string]string{} // value -> constant name
	for _, fd := range membersOfType(t, "field_declaration") {
		if !(hasModifier(fd, "static") && hasModifier(fd, "final")) {
			continue
		}
		if v := f.constantValue(fd, false); strings.HasPrefix(v, `"`) {
			for _, d := range named(fd) {
				if d.Type() == "variable_declarator" {
					local[v] = f.text(d.ChildByFieldName("name"))
				}
			}
		}
	}
	project := map[string]string{}
	for ref, k := range c.ctx.Constants {
		if strings.HasPrefix(k.Value, `"`) {
			project[k.Value] = ref
		}
	}
	for _, body := range append(membersOfType(t, "method_declaration"), membersOfType(t, "constructor_declaration")...) {
		for _, lit := range findAll(body, "string_literal") {
			value := f.text(lit)
			if len(value) < 5 { // "" and very short strings are rarely meant as the constant
				continue
			}
			name, ok := local[value]
			if !ok {
				if name, ok = project[value]; !ok {
					continue
				}
			}
			c.add(lit, lit, "low", "design", fmt.Sprintf("%s duplicates constant %s", value, name),
				fmt.Sprintf("The literal repeats %s. When the constant changes, this copy silently stays behind.", name),
				"Use "+name+" instead of "+value+".")
		}
	}
}

// logCall describes a print or log call for duplicate detection: constants are replaced by their
// value so LOG_PREFIX + "x" and "CUSTOMER" + "x" compare equal.
func (c *checker) logCall(call *sitter.Node, consts map[string]string) (string, bool) {
	f := c.f
	name := f.text(call.ChildByFieldName("name"))
	obj := f.text(call.ChildByFieldName("object"))
	last := obj[strings.LastIndex(obj, ".")+1:]
	isPrint := (obj == "System.out" || obj == "System.err") && (name == "println" || name == "print")
	isLog := logReceiver.MatchString(last) && (loudLevels[name] || name == "debug" || name == "trace")
	if !isPrint && !isLog {
		return "", false
	}
	var parts []string
	for _, a := range args(call) {
		walk(a, func(n *sitter.Node) bool {
			switch n.Type() {
			case "string_literal":
				parts = append(parts, strings.Trim(f.text(n), `"`))
				return false
			case "identifier":
				if v, ok := consts[f.text(n)]; ok {
					parts = append(parts, strings.Trim(v, `"`))
				} else {
					parts = append(parts, "{"+f.text(n)+"}")
				}
				return false
			case "field_access":
				ref := f.text(n)
				if k, ok := c.ctx.Constants[ref]; ok {
					parts = append(parts, strings.Trim(k.Value, `"`))
				} else {
					parts = append(parts, "{"+ref+"}")
				}
				return false
			}
			return true
		})
	}
	return strings.Join(parts, ""), true
}

func (c *checker) classConstants(t *sitter.Node) map[string]string {
	f := c.f
	out := map[string]string{}
	for _, fd := range membersOfType(t, "field_declaration") {
		if v := f.constantValue(fd, false); v != "" {
			for _, d := range named(fd) {
				if d.Type() == "variable_declarator" {
					out[f.text(d.ChildByFieldName("name"))] = v
				}
			}
		}
	}
	return out
}

// duplicateLogs flags the same message logged twice in one method.
func (c *checker) duplicateLogs(t *sitter.Node) {
	consts := c.classConstants(t)
	for _, m := range membersOfType(t, "method_declaration") {
		seen := map[string]*sitter.Node{}
		for _, call := range findAll(m, "method_invocation") {
			sig, ok := c.logCall(call, consts)
			if !ok || !strings.Contains(sig, "{") { // only messages that carry data
				continue
			}
			if first, dup := seen[sig]; dup {
				c.add(call, call, "low", "logging", fmt.Sprintf("Same message logged twice in %s()", c.f.text(m.ChildByFieldName("name"))),
					fmt.Sprintf("Line %d already logs this message with the same data. Duplicate lines double log volume and make it look like two events happened.", startLine(first)),
					"Keep one of the two log statements.")
				continue
			}
			seen[sig] = call
		}
	}
}

// logHelpers flags small private methods that only print or log a fixed line.
func (c *checker) logHelpers(t *sitter.Node) {
	f := c.f
	consts := c.classConstants(t)
	for _, m := range membersOfType(t, "method_declaration") {
		if !hasModifier(m, "private") {
			continue
		}
		body := m.ChildByFieldName("body")
		stmts := named(body)
		if len(stmts) == 0 || len(stmts) > 3 {
			continue
		}
		informative := false
		for _, s := range stmts {
			if s.Type() != "expression_statement" {
				return
			}
			calls := findAll(s, "method_invocation")
			if len(calls) == 0 {
				return
			}
			sig, ok := c.logCall(calls[0], consts)
			if !ok {
				return
			}
			informative = informative || strings.Contains(sig, "{")
		}
		name := f.text(m.ChildByFieldName("name"))
		detail := name + "() only writes a log line. A helper for one log statement adds indirection and hides the call site from the log (the logger reports the helper, not the caller)."
		if !informative {
			detail += " The message carries no data (no id, count or duration), so it says nothing a reader can act on."
		}
		c.add(m, m, "low", "design", fmt.Sprintf("%s() is a log-only helper", name), detail,
			"Remove "+name+"() and its calls; if the event matters, log it where it happens with context:\nlog.info(\"Customer {} created\", customer.getId());")
	}
}
