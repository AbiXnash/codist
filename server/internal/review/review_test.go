package review

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"codist/internal/config"
	"codist/internal/gitx"
	"codist/internal/javaidx"
	"codist/internal/knowledge"
)

func TestAssembleTrimsLowPriorityFirst(t *testing.T) {
	big := strings.Repeat("background line\n", 400) // ~6.4k chars
	out := assemble([]section{
		{title: "## Overview", body: big, priority: 7, keep: 0},
		{title: "## Diff", body: "+ changed\n", priority: 1, keep: 100},
		{title: "## File", body: strings.Repeat("code\n", 200), priority: 1, keep: 1000},
	}, 2000)
	if !strings.Contains(out, "## Diff") || !strings.Contains(out, "## File") {
		t.Fatal("high-priority sections must survive")
	}
	if len(out) > 2200 {
		t.Fatalf("over budget: %d", len(out))
	}
	if strings.Count(out, "background line") > 70 || !strings.Contains(out, strings.Repeat("code\n", 200)) {
		t.Fatal("low-priority section should have been trimmed first")
	}
}

func TestIgnoreScopes(t *testing.T) {
	x := NewIgnores(t.TempDir(), "repo", t.TempDir())
	f := Finding{Line: 23, Title: "Use a logger instead of System.out.println", Ref: "console-output", Category: "style",
		Snippet: `System.out.println(DataConstant.LOG_PREFIX + "Request: " + req);`}
	if _, err := x.Add(Ignore{Scope: "code", Path: "A.java", Key: f.Key(), Snippet: f.Snippet}); err != nil {
		t.Fatal(err)
	}
	all := x.All()
	moved := f
	moved.Line = 40 // same code, different line in a later commit
	if _, ok := x.Match(all, "sha2", "A.java", moved); !ok {
		t.Error("code-scope ignore should follow the line")
	}
	if _, ok := x.Match(all, "sha2", "B.java", f); ok {
		t.Error("code-scope ignore must not leak to other files")
	}
	if _, err := x.Add(Ignore{Scope: "rule", Key: "field-injection", Paths: []string{"src/legacy/"}}); err != nil {
		t.Fatal(err)
	}
	fi := Finding{Ref: "field-injection"}
	if _, ok := x.Match(x.All(), "s", "src/legacy/Old.java", fi); !ok {
		t.Error("rule ignore should apply under its path")
	}
	if _, ok := x.Match(x.All(), "s", "src/main/New.java", fi); ok {
		t.Error("rule ignore must not apply outside its path")
	}
	r := x.Decorate(Review{SHA: "sha2", Files: []FileReview{{Path: "A.java", Findings: []Finding{moved, {Title: "Other", Severity: "high"}}}}})
	if !r.Files[0].Findings[0].Ignored || r.Counts()["high"] != 1 {
		t.Errorf("decorate/counts: %+v %v", r.Files[0].Findings, r.Counts())
	}
}

func TestBuildFindings(t *testing.T) {
	pom := `<project><parent><artifactId>spring-boot-starter-parent</artifactId><version>2.7.18</version></parent>
<properties><java.version>11</java.version><log4j.version>2.14.1</log4j.version></properties>
<dependencies>
<dependency><groupId>org.apache.logging.log4j</groupId><artifactId>log4j-core</artifactId><version>${log4j.version}</version></dependency>
<dependency><groupId>com.h2database</groupId><artifactId>h2</artifactId></dependency>
</dependencies></project>`
	b, ok := ParseBuild("pom.xml", pom)
	if !ok || b.SpringBoot != "2.7.18" || b.Java != "11" {
		t.Fatalf("parse: %+v", b)
	}
	refs := map[string]bool{}
	for _, f := range BuildFindings(b, pom, nil) {
		refs[f.Ref] = true
	}
	for _, want := range []string{"eol-spring-boot-2", "cve-log4shell", "dep-dev-only"} {
		if !refs[want] {
			t.Errorf("missing %s in %v", want, refs)
		}
	}
}

func TestAssignGroups(t *testing.T) {
	r := Review{SHA: "s", Files: []FileReview{
		{Path: "src/main/java/a/product/ProductService.java", Findings: []Finding{
			{Line: 23, Severity: "medium", Title: `DataConstant.LOG_PREFIX is "CUSTOMER" but used in ProductService`, Ref: "copy-paste-wrong-constant",
				Snippet: `System.out.println(DataConstant.LOG_PREFIX + "Request: " + req);`},
			{Line: 23, Severity: "low", Title: "Use a logger instead of System.out.println", Ref: "console-output",
				Snippet: `System.out.println(DataConstant.LOG_PREFIX + "Request: " + req);`},
		}},
		{Path: "src/main/java/a/customer/CustomerService.java", Findings: []Finding{
			{Line: 21, Severity: "low", Title: "Use a logger instead of System.out.println", Ref: "console-output"},
		}},
		{Path: "src/main/java/a/common/DataConstant.java", Findings: []Finding{
			{Line: 4, Severity: "medium", Title: "Domain-specific constant in a shared constants class", Ref: "domain-constants-in-common",
				Snippet: `public static final String LOG_PREFIX = "CUSTOMER";`},
		}},
		{Path: "src/main/java/a/Other.java", Findings: []Finding{{Line: 1, Severity: "high", Title: "Unrelated", Ref: "x"}}},
	}}
	g := AssignGroups(r)
	pfx, log := g.Files[0].Findings[0], g.Files[0].Findings[1]
	if pfx.Group == "" || pfx.Group != g.Files[2].Findings[0].Group {
		t.Fatalf("constant usage and declaration should share a group: %+v / %+v", pfx, g.Files[2].Findings[0])
	}
	if !g.Files[2].Findings[0].GroupRoot {
		t.Error("the declaration in DataConstant.java should be the root")
	}
	if log.Group == "" || log.Group != g.Files[1].Findings[0].Group || log.Group == pfx.Group {
		t.Errorf("console-output findings should form their own group: %+v %+v", log, g.Files[1].Findings[0])
	}
	if g.Files[3].Findings[0].Group != "" {
		t.Error("unrelated finding must stay alone")
	}
	c := g.Counts()
	if c["medium"] != 1 || c["low"] != 1 || c["high"] != 1 {
		t.Errorf("counts should be per issue, got %v", c)
	}
}

func TestBrokenReferences(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	run("init", "-q", "-b", "main")
	write := func(p, s string) {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755)
		os.WriteFile(filepath.Join(dir, p), []byte(s), 0o644)
	}
	before := "package a;\npublic final class DataConstant {\n    public static final String LOG_PREFIX = \"CUSTOMER\";\n}\n"
	write("src/DataConstant.java", before)
	write("src/CustomerService.java", "package a;\nclass CustomerService { void x() { log(DataConstant.LOG_PREFIX); } }\n")
	write("src/ProductService.java", "package a;\nclass ProductService { void x() { log(DataConstant.LOG_PREFIX); } }\n")
	run("add", "-A")
	run("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "init")
	repo, err := gitx.Open(config.Config{RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	x := &Fixer{repo: repo}
	renamed := strings.Replace(before, "LOG_PREFIX", "PRODUCT_LOG_PREFIX", 1)
	got := x.brokenReferences("src/DataConstant.java", before, renamed, []string{"src/DataConstant.java", "src/ProductService.java"})
	if len(got) != 1 || !strings.Contains(got[0], "CustomerService.java") {
		t.Fatalf("expected CustomerService to be reported, got %v", got)
	}
	added := strings.Replace(before, "}\n", "    public static final String PRODUCT_LOG_PREFIX = \"PRODUCT\";\n}\n", 1)
	if got := x.brokenReferences("src/DataConstant.java", before, added, nil); len(got) != 0 {
		t.Fatalf("adding a constant breaks nothing, got %v", got)
	}
}

const userCustomer = `package com.demo.shop.customer;

import com.demo.shop.common.NotFoundException;
import com.demo.shop.customer.CustomerDto.CreateCustomerRequest;

import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service
public class CustomerService {
    private static final String LOG_PREFIX = "CUSTOMER";

    private CustomerRepository customers;

    @Transactional
    public Customer create(CreateCustomerRequest req) {
        System.out.println(LOG_PREFIX + "Request: " + req);
        if (customers.findByEmail(req.email()).isPresent()) {
            throw new IllegalStateException("Email already registered: " + req.email());
        }
        System.out.println("CUSTOMER" + "Request: " + req);
        endLog();
        return customers.save(new Customer(req.email(), req.name()));
    }

    @Transactional(readOnly = true)
    public Customer get(Long id) {
        return customers.findById(id).orElseThrow(() -> new NotFoundException("No customer " + id));
    }

    private static void endLog() {
        System.out.println("CUSTOMER" + "Service Ended");
    }
}
`

func TestMechanicalFixes(t *testing.T) {
	const path = "src/main/java/com/demo/shop/customer/CustomerService.java"
	all := map[int]bool{}
	for i := 1; i <= 40; i++ {
		all[i] = true
	}
	byRule := map[string][]Finding{}
	for _, x := range javaidx.Check([]byte(userCustomer), all, javaidx.Context{Path: path, Beans: map[string]string{"CustomerRepository": "repository"}}) {
		byRule[x.ID] = append(byRule[x.ID], Finding{Line: x.Line, EndLine: x.EndLine, Title: x.Title, Suggestion: x.Suggestion, Ref: x.ID})
	}
	check := func(rule string, wants, gone []string) {
		t.Helper()
		out, ok := applyMechanical(path, userCustomer, byRule[rule])
		if !ok {
			t.Fatalf("%s: no mechanical fix", rule)
		}
		if !javaidx.Valid([]byte(out)) {
			t.Fatalf("%s: result does not parse:\n%s", rule, out)
		}
		for _, w := range wants {
			if !strings.Contains(out, w) {
				t.Errorf("%s: missing %q in\n%s", rule, w, out)
			}
		}
		for _, g := range gone {
			if strings.Contains(out, g) {
				t.Errorf("%s: still contains %q in\n%s", rule, g, out)
			}
		}
	}
	check("uninjected-dependency", []string{"private final CustomerRepository customers;", "public CustomerService(CustomerRepository customers) {", "this.customers = customers;"}, nil)
	check("console-output", []string{"import org.slf4j.LoggerFactory;", "private static final Logger log = LoggerFactory.getLogger(CustomerService.class);", `log.info("{}Request: {}", LOG_PREFIX, req);`}, []string{"System.out"})
	check("literal-instead-of-constant", []string{`System.out.println(LOG_PREFIX + "Service Ended");`}, []string{`"CUSTOMER" +`})
	check("duplicate-log", nil, []string{`System.out.println("CUSTOMER" + "Request: " + req);`})
	check("log-helper-method", nil, []string{"endLog", "Service Ended"})
}

// An issue reported in commit 1 and removed in commit 2 is resolved, credited to commit 2's
// author, and listed as fixed by commit 2.
func TestIssueLifecycle(t *testing.T) {
	dir, data := t.TempDir(), t.TempDir()
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=a@b", "-c", "user.name=Dev"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	p := "src/main/java/a/A.java"
	os.MkdirAll(filepath.Join(dir, "src/main/java/a"), 0o755)
	os.WriteFile(filepath.Join(dir, p), []byte("class A {\n  void x() { System.out.println(\"hi\"); }\n}\n"), 0o644)
	git("add", "-A")
	git("commit", "-qm", "one")
	c1 := git("rev-parse", "HEAD")
	// A commit in between touches the file but keeps the flagged line.
	os.WriteFile(filepath.Join(dir, p), []byte("class A {\n  void x() { System.out.println(\"hi\"); }\n  void y() {}\n}\n"), 0o644)
	git("commit", "-qam", "add y")
	os.WriteFile(filepath.Join(dir, p), []byte("class A {\n  void x() { log.info(\"hi\"); }\n  void y() {}\n}\n"), 0o644)
	git("commit", "-qam", "use the logger")
	c2 := git("rev-parse", "HEAD")

	repo, err := gitx.Open(config.Config{RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	store, _ := NewStore(data, "r")
	store.Save(Review{SHA: c1, Status: StatusDone, Files: []FileReview{{Path: p, Findings: []Finding{
		{Line: 2, Severity: "low", Title: "Use a logger instead of System.out.println", Ref: "console-output",
			Snippet: `void x() { System.out.println("hi"); }`}}}}})
	tr := NewTracker(repo, store, NewIgnores(data, "r", dir))
	issues := tr.Issues()
	if len(issues) != 1 {
		t.Fatalf("issues: %+v", issues)
	}
	is := issues[0]
	if is.State != "resolved" || is.FixedIn != c2 || is.FixedBy != "Dev" || is.FixedMsg != "use the logger" || is.FirstSeen != c1 {
		t.Fatalf("lifecycle: %+v", is)
	}
	if got := FixedInCommit(issues, c2); len(got) != 1 {
		t.Fatalf("commit 2 should list the fix, got %v", got)
	}
	r, _ := store.Get(c1)
	f := tr.Annotate(r, issues).Files[0].Findings[0]
	if f.State != "resolved" || f.FixedIn != c2 || f.FixedBy != "Dev" {
		t.Fatalf("annotation on commit 1: %+v", f)
	}
}

func TestModelClaimsCheckedAgainstRules(t *testing.T) {
	if ruleCovered("", "Uninjected dependency") != "uninjected-dependency" || ruleCovered("console-output", "") != "console-output" {
		t.Error("deterministic claims should be recognised")
	}
	if ruleCovered("", "Race condition on stock") != "" {
		t.Error("judgement findings must stay with the model")
	}
	lines := []string{"class A {", "", "  int x;", "}"}
	if snapToCode(lines, 2) != 3 || snapToCode([]string{"", "", "", "", "", ""}, 3) != 0 {
		t.Error("snapToCode")
	}
}

func TestCheckImportsAndTestProblems(t *testing.T) {
	x := &Fixer{Known: func() map[string]bool {
		return map[string]bool{"com.demo.shop.customer.CustomerRepository": true, "com.demo.shop.common.NotFoundException": true}
	}}
	before := "package com.demo.shop.customer;\n\nimport com.demo.shop.common.NotFoundException;\n\nclass S {}\n"
	after := "package com.demo.shop.customer;\n\nimport com.demo.shop.customer.repository.CustomerRepository;\nimport com.demo.shop.common.NotFoundException;\n\nclass S {}\n"
	out, bad := x.checkImports(before, after)
	if len(bad) != 0 || strings.Contains(out, "customer.repository") {
		t.Fatalf("same-package import should be dropped: %v\n%s", bad, out)
	}
	_, bad = x.checkImports(before, strings.Replace(after, "customer.repository.CustomerRepository", "billing.Invoice", 1))
	if len(bad) != 1 {
		t.Fatalf("unknown project import should be reported, got %v", bad)
	}
	if p := testProblems(`assertThatThrownBy(() -> s.create(r)).isInstanceOf(NullPointerException.class);`, "NullPointerException"); len(p) != 1 {
		t.Fatalf("asserting the defect must be rejected: %v", p)
	}
	if p := testProblems(`assertThat(s.create(r)).isNotNull();`, "NullPointerException"); len(p) != 0 {
		t.Fatalf("a correct test was rejected: %v", p)
	}
}

func TestSignOffSurvivesChangesAndKeysByText(t *testing.T) {
	so := NewSignOffs(t.TempDir(), "r")
	to := "abcdef1234567"
	gen := []string{"Review the unreviewed commits", "Run the regression tests for com.demo.order"}
	if err := so.Set(to, gen[1], true, false, "Dev"); err != nil {
		t.Fatal(err)
	}
	// A different "changes since" range generates a different list, in a different order.
	got := so.For(to, []string{"Check the h2 change", gen[1]})
	if got.Total != 2 || got.Done != 1 || !got.Items[1].Done || got.Items[1].By != "Dev" {
		t.Fatalf("tick must follow its item, not its position: %+v", got)
	}
	// An item that is no longer generated but was ticked stays visible, marked not current.
	got = so.For(to, []string{"Check the h2 change"})
	if len(got.Items) != 2 || got.Items[1].Current || !got.Items[1].Done {
		t.Fatalf("a past sign-off must not be dropped: %+v", got)
	}
	if err := so.Set(to, "Notify the support team", false, true, "Dev"); err != nil {
		t.Fatal(err)
	}
	if got = so.For(to, nil); got.Total != 1 || !got.Items[len(got.Items)-1].Custom {
		t.Fatalf("custom items count: %+v", got)
	}
}

func TestReleaseChecksTheWholeCodebase(t *testing.T) {
	dir, data := t.TempDir(), t.TempDir()
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=a@b", "-c", "user.name=Dev"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	os.MkdirAll(filepath.Join(dir, "src/main/java/shop/order"), 0o755)
	os.MkdirAll(filepath.Join(dir, "src/main/java/shop/user"), 0o755)
	// The bug is in a file no later commit touches.
	os.WriteFile(filepath.Join(dir, "src/main/java/shop/order/Old.java"), []byte("package shop.order;\nclass Old {\n  void x() { System.out.println(\"hi\"); }\n}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "src/main/java/shop/user/New.java"), []byte("package shop.user;\nclass New {}\n"), 0o644)
	git("add", "-A")
	git("commit", "-qm", "start")
	os.WriteFile(filepath.Join(dir, "src/main/java/shop/user/New.java"), []byte("package shop.user;\nclass New {\n  int n;\n}\n"), 0o644)
	git("commit", "-qam", "touch new only")
	head := git("rev-parse", "HEAD")

	repo, err := gitx.Open(config.Config{RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	store, _ := NewStore(data, "r")
	store.Save(Review{SHA: head, Status: StatusDone, Files: []FileReview{{Path: "src/main/java/shop/user/New.java", Findings: []Finding{}}}})
	ig := NewIgnores(data, "r", dir)
	kb, _ := knowledge.Load()
	rep := Release(repo, NewTracker(repo, store, ig), store, ig, kb, head, "HEAD")

	if rep.Files != 2 || rep.Sources != 2 || rep.Reviewed != 1 {
		t.Fatalf("coverage: files=%d sources=%d reviewed=%d", rep.Files, rep.Sources, rep.Reviewed)
	}
	if len(rep.NotReviewed) != 1 || !strings.HasSuffix(rep.NotReviewed[0], "Old.java") {
		t.Fatalf("not reviewed: %v", rep.NotReviewed)
	}
	var found *Issue
	for i, is := range rep.Open {
		if strings.HasSuffix(is.Path, "Old.java") && is.Ref == "console-output" {
			found = &rep.Open[i]
		}
	}
	if found == nil || found.Line != 3 || found.FirstBy != "Dev" || found.FirstMsg != "start" {
		t.Fatalf("issue in the untouched file should be reported with its author: %+v", rep.Open)
	}
	if !slices.Contains(rep.Checklist, "Run the regression tests for shop.order") {
		t.Fatalf("checklist: %v", rep.Checklist)
	}
}

func TestRuleIssueFixedElsewhereIsResolved(t *testing.T) {
	dir, data := t.TempDir(), t.TempDir()
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=a@b", "-c", "user.name=Dev"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(p, s string) {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755)
		os.WriteFile(filepath.Join(dir, p), []byte(s), 0o644)
	}
	constPath, usePath := "src/main/java/shop/common/DataConstant.java", "src/main/java/shop/product/ProductService.java"
	write(constPath, "package shop.common;\npublic final class DataConstant {\n    public static final String LOG_PREFIX = \"CUSTOMER\";\n}\n")
	write(usePath, "package shop.product;\nimport shop.common.DataConstant;\nclass ProductService {\n    void x() { log.info(DataConstant.LOG_PREFIX + \"x\"); }\n}\n")
	write("src/main/java/shop/customer/CustomerService.java", "package shop.customer;\nclass CustomerService {}\n")
	git("init", "-q", "-b", "main")
	git("add", "-A")
	git("commit", "-qm", "one")
	c1 := git("rev-parse", "HEAD")

	repo, err := gitx.Open(config.Config{RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := knowledge.Load()
	rules := func() (javaidx.Context, *knowledge.Base, bool) {
		g := javaidx.Index(repo.JavaFilesWorking(), "wt")
		return javaidx.ProjectContext(&g), kb, true
	}
	ctx, _, _ := rules()
	src, _ := repo.ReadFile(c1, usePath)
	var flagged []Finding
	for _, f := range RuleScan(usePath, []byte(src), ctx, kb) {
		if f.Ref == "copy-paste-wrong-constant" {
			flagged = append(flagged, f)
		}
	}
	if len(flagged) != 1 {
		t.Fatalf("the rule should flag the wrong prefix at commit 1: %+v", flagged)
	}
	store, _ := NewStore(data, "r")
	// The model made the same claim on the constant's class line, which never changes.
	model := Finding{Line: 2, Severity: "medium", Title: "Copy-paste wrong constant", Source: "llm", Ref: "copy-paste-wrong-constant",
		Detail: "LOG_PREFIX is CUSTOMER but used in ProductService", Snippet: "public final class DataConstant {"}
	store.Save(Review{SHA: c1, Status: StatusDone, Files: []FileReview{{Path: usePath, Findings: flagged}, {Path: constPath, Findings: []Finding{model}}}})

	// The fix is in another file; the flagged line is unchanged.
	write(constPath, "package shop.common;\npublic final class DataConstant {\n    public static final String LOG_PREFIX = \"PRODUCT\";\n}\n")
	git("commit", "-qam", "updated log prefix")
	c2 := git("rev-parse", "HEAD")

	ig := NewIgnores(data, "r", dir)
	tr := NewTracker(repo, store, ig)
	tr.Rules = rules
	issues := tr.Issues()
	if len(issues) != 2 {
		t.Fatalf("issues: %+v", issues)
	}
	for _, is := range issues {
		if is.State != "resolved" || is.FixedIn != c2 || is.FixedMsg != "updated log prefix" {
			t.Fatalf("both the rule's and the model's finding should be resolved by commit 2: %+v", is)
		}
	}

	x := &Fixer{repo: repo, store: store, Ignores: ig, Rules: rules}
	if _, err := x.plan(c1, usePath, flagged[0].Line, flagged[0].Title); err == nil || !strings.Contains(err.Error(), "already fixed") {
		t.Fatalf("no fix should be proposed for a resolved issue, got %v", err)
	}
	if _, err := x.plan(c1, constPath, 2, model.Title); err == nil || !strings.Contains(err.Error(), "already fixed") {
		t.Fatalf("no fix should be proposed for the model's copy of the claim, got %v", err)
	}
}

func TestDuplicateConstants(t *testing.T) {
	before := "final class C {\n    public static final String LOG_PREFIX = \"PRODUCT\";\n}\n"
	after := "final class C {\n    public static final String LOG_PREFIX = \"PRODUCT\";\n    public static final String PRODUCT_LOG_PREFIX = \"PRODUCT\";\n}\n"
	if d := duplicateConstants(before, after); len(d) != 1 || !strings.Contains(d[0], "PRODUCT_LOG_PREFIX") || !strings.Contains(d[0], "LOG_PREFIX") {
		t.Fatalf("duplicate not caught: %v", d)
	}
	ok := "final class C {\n    public static final String LOG_PREFIX = \"CUSTOMER\";\n    public static final String PRODUCT_LOG_PREFIX = \"PRODUCT\";\n}\n"
	if d := duplicateConstants(before, ok); len(d) != 0 {
		t.Fatalf("distinct values flagged: %v", d)
	}
}

func TestWorkingChangesReviewAndConstructorInjection(t *testing.T) {
	dir, data := t.TempDir(), t.TempDir()
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=a@b", "-c", "user.name=Dev"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
		return strings.TrimSpace(string(out))
	}
	p := "src/main/java/shop/customer/CustomerService.java"
	os.MkdirAll(filepath.Join(dir, filepath.Dir(p)), 0o755)
	before := "package shop.customer;\n\nimport org.springframework.stereotype.Service;\n\n@Service\npublic class CustomerService {\n    private CustomerRepository customers;\n\n    public void x() { customers.findAll(); }\n}\n"
	os.WriteFile(filepath.Join(dir, p), []byte(before), 0o644)
	git("init", "-q", "-b", "main")
	git("add", "-A")
	git("commit", "-qm", "one")
	// Uncommitted: the developer adds field injection.
	edited := "package shop.customer;\n\nimport org.springframework.beans.factory.annotation.Autowired;\nimport org.springframework.stereotype.Service;\n\n@Service\npublic class CustomerService {\n    @Autowired private CustomerRepository customers;\n\n    public void x() { customers.findAll(); }\n}\n"
	os.WriteFile(filepath.Join(dir, p), []byte(edited), 0o644)

	repo, err := gitx.Open(config.Config{RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := knowledge.Load()
	rules := func() (javaidx.Context, *knowledge.Base, bool) {
		g := javaidx.Index(repo.JavaFilesWorking(), "wt")
		return javaidx.ProjectContext(&g), kb, true
	}
	ig := NewIgnores(data, "r", dir)
	w := WorkingReview(repo, rules, ig)
	var fi *Finding
	for _, fr := range w.Files {
		for k, f := range fr.Findings {
			if f.Ref == "field-injection" {
				fi = &fr.Findings[k]
			}
		}
	}
	if fi == nil || fi.Line != 8 {
		t.Fatalf("the uncommitted @Autowired line should be flagged: %+v", w.Files)
	}

	store, _ := NewStore(data, "r")
	x := &Fixer{repo: repo, store: store, Ignores: ig, Rules: rules, proposals: map[string]*Proposal{}, prefetched: map[string]*Proposal{}}
	prop, err := x.Propose(WorkingSHA, p, fi.Line, fi.Title)
	if err != nil {
		t.Fatal(err)
	}
	got := prop.edits[0].proposed
	for _, want := range []string{"    private final CustomerRepository customers;", "    public CustomerService(CustomerRepository customers) {", "        this.customers = customers;"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Autowired") || !prop.Working {
		t.Fatalf("annotation and import should go, proposal should be a working-tree fix:\n%s", got)
	}
	if !javaidx.Valid([]byte(got)) {
		t.Fatalf("fix does not parse:\n%s", got)
	}
	if sha, err := x.Apply(prop.ID, ""); err != nil || sha != "" {
		t.Fatalf("a working-tree fix is applied, not committed: %q %v", sha, err)
	}
	if git("rev-list", "--count", "HEAD") != "1" {
		t.Fatal("nothing should have been committed")
	}
}

func TestHeadsUpIsNotFixable(t *testing.T) {
	if _, ok := notFixable["shared-constant-changed"]; !ok {
		t.Fatal("shared-constant-changed must not offer a fix: fixing it reverts the change")
	}
}

func TestGuidedFixOnlyAcceptsSuggestionsAboutTheIssue(t *testing.T) {
	dir, data := t.TempDir(), t.TempDir()
	git := func(args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=a@b", "-c", "user.name=Dev"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	p := "src/main/java/shop/customer/CustomerService.java"
	os.MkdirAll(filepath.Join(dir, filepath.Dir(p)), 0o755)
	src := "package shop.customer;\n\nclass CustomerService {\n    void x() { System.out.println(\"hi\"); }\n}\n"
	os.WriteFile(filepath.Join(dir, p), []byte(src), 0o644)
	git("init", "-q", "-b", "main")
	git("add", "-A")
	git("commit", "-qm", "one")

	// A stand-in for Ollama: the relevance check says "unrelated" for anything about orders.
	var asked []string
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		asked = append(asked, string(b))
		answer := `{"related":true,"reason":"about logging"}`
		if strings.Contains(string(b), "discount") {
			answer = `{"related":false,"reason":"it asks for a new discount feature, not this logging fix"}`
		}
		if strings.Contains(string(b), "fenced code block") { // the fix itself
			answer = "```java\npackage shop.customer;\n\nclass CustomerService {\n    private static final org.slf4j.Logger log = org.slf4j.LoggerFactory.getLogger(CustomerService.class);\n    void x() { log.debug(\"hi\"); }\n}\n```"
		}
		json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": answer}})
	}))
	defer llm.Close()

	repo, _ := gitx.Open(config.Config{RepoPath: dir})
	store, _ := NewStore(data, "r")
	head := repo.Head()
	store.Save(Review{SHA: head, Status: StatusDone, Files: []FileReview{{Path: p, Findings: []Finding{
		{Line: 4, EndLine: 4, Severity: "low", Title: "Use a logger instead of System.out.println", Ref: "console-output", Source: "rule",
			Snippet: `void x() { System.out.println("hi"); }`}}}}})
	x := NewFixer(repo, NewOllama(llm.URL, "m", 2048, 10*time.Second), nil, store)
	x.Known = func() map[string]bool {
		return map[string]bool{"shop.customer.CustomerService": true, "shop.order.OrderService": true}
	}

	// Names another class of the project: blocked without asking the model.
	_, err := x.ProposeGuided(head, p, 4, "Use a logger instead of System.out.println", "also fix the null check in OrderService")
	var off ErrOffTopic
	if !errors.As(err, &off) || !strings.Contains(off.Reason, "OrderService") || len(asked) != 0 {
		t.Fatalf("a suggestion about another class must be refused before the model runs: %v (asked %d)", err, len(asked))
	}
	// Off topic by intent: the model check refuses it.
	_, err = x.ProposeGuided(head, p, 4, "Use a logger instead of System.out.println", "add a discount code to checkout")
	if !errors.As(err, &off) || !strings.Contains(off.Reason, "discount") {
		t.Fatalf("an unrelated suggestion must be refused: %v", err)
	}
	// On topic: the fix follows it, and the suggestion reaches the fix model.
	prop, err := x.ProposeGuided(head, p, 4, "Use a logger instead of System.out.println", "log it at debug level")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prop.edits[0].proposed, `log.debug("hi")`) || !strings.Contains(asked[len(asked)-1], "log it at debug level") {
		t.Fatalf("the guided fix should use the suggestion:\n%s", prop.edits[0].proposed)
	}
}

func TestTeamHistoryRevertMergeAndConflicts(t *testing.T) {
	dir, data := t.TempDir(), t.TempDir()
	run := func(who string, args ...string) (string, error) {
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=" + who + "@x", "-c", "user.name=" + who}, args...)...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	git := func(who string, args ...string) string {
		out, err := run(who, args...)
		if err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
		return out
	}
	write := func(p, s string) {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755)
		os.WriteFile(filepath.Join(dir, p), []byte(s), 0o644)
	}
	a := "src/main/java/shop/A.java"
	bug := "class A {\n    void x() { System.out.println(\"hi\"); }\n}\n"
	write(a, bug)
	write("src/main/java/shop/B.java", "class B {\n    int v() { return 1; }\n    int w() { return 2; }\n}\n")
	git("Ann", "init", "-q", "-b", "main")
	git("Ann", "add", "-A")
	git("Ann", "commit", "-qm", "start")
	c1 := git("Ann", "rev-parse", "HEAD")
	write(a, "class A {\n    void x() { log.info(\"hi\"); }\n}\n")
	git("Bob", "commit", "-qam", "use the logger")
	c2 := git("Bob", "rev-parse", "HEAD")
	git("Ann", "revert", "--no-edit", "HEAD")
	c3 := git("Ann", "rev-parse", "HEAD")

	repo, _ := gitx.Open(config.Config{RepoPath: dir})
	store, _ := NewStore(data, "r")
	store.Save(Review{SHA: c1, Status: StatusDone, Files: []FileReview{{Path: a, Findings: []Finding{
		{Line: 2, Severity: "low", Title: "Use a logger instead of System.out.println", Ref: "console-output", Source: "llm",
			Snippet: `void x() { System.out.println("hi"); }`}}}}})
	ig := NewIgnores(data, "r", dir)
	tr := NewTracker(repo, store, ig)
	is := tr.Issues()
	if len(is) != 1 || is[0].State != "open" || is[0].WasFixedIn != c2 || is[0].ReopenedIn != c3 || is[0].ReopenedBy != "Ann" {
		t.Fatalf("the revert should reopen the issue Bob fixed: %+v", is)
	}

	// A merge: the feature branch changes v(), main changes w() (clean) and both change the same line of v() (conflict).
	git("Ann", "checkout", "-qb", "feature")
	write("src/main/java/shop/B.java", "class B {\n    int v() { return 10; }\n    int w() { return 2; }\n}\n")
	git("Ann", "commit", "-qam", "feature v")
	git("Ann", "checkout", "-q", "main")
	write("src/main/java/shop/B.java", "class B {\n    int v() { return 20; }\n    int w() { return 3; }\n}\n")
	git("Bob", "commit", "-qam", "main v and w")
	if _, err := run("Bob", "merge", "feature"); err == nil {
		t.Fatal("expected a conflict")
	}
	if op := repo.Operation(); op != "merge" {
		t.Fatalf("operation = %q", op)
	}
	if c := repo.Conflicts(); len(c) != 1 || c[0] != "src/main/java/shop/B.java" {
		t.Fatalf("conflicts = %v", c)
	}
	x := &Fixer{repo: repo, store: store, Ignores: ig}
	if err := x.ready(); err == nil || !strings.Contains(err.Error(), "merge is in progress") {
		t.Fatalf("fixes must be refused during a merge: %v", err)
	}
	src, _, _ := repo.ReadWorking("src/main/java/shop/B.java")
	if m := ConflictMarkers(strings.Split(src, "\n"), nil); len(m) != 1 || m[0].Severity != "critical" {
		t.Fatalf("conflict markers not found: %+v", m)
	}
	// Resolve it with an extra edit no parent had.
	write("src/main/java/shop/B.java", "class B {\n    int v() { return 10 + 20; }\n    int w() { return 3; }\n}\n")
	git("Bob", "add", "-A")
	git("Bob", "commit", "-qm", "merge feature")
	merge, _ := repo.Commit(git("Bob", "rev-parse", "HEAD"))
	files, err := repo.Diff(merge)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || len(files[0].AddedLines) != 1 || files[0].AddedLines[0] != 2 {
		t.Fatalf("a merge should be reviewed only for its conflict resolution (line 2 of B): %+v", files)
	}
}
