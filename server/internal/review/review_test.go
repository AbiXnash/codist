package review

import (
	"context"
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
	"sync"
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

	// The release report agrees with the tracker: a model finding that is fixed stays fixed.
	for _, is := range Release(repo, tr, store, ig, kb, c2, "HEAD").Open {
		if is.Ref == "copy-paste-wrong-constant" {
			t.Fatalf("a resolved finding came back in the release report: %+v", is)
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
	prop, err := x.Propose(context.Background(), WorkingSHA, p, fi.Line, fi.Title)
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
	_, err := x.ProposeGuided(context.Background(), head, p, 4, "Use a logger instead of System.out.println", "also fix the null check in OrderService")
	var off ErrOffTopic
	if !errors.As(err, &off) || !strings.Contains(off.Reason, "OrderService") || len(asked) != 0 {
		t.Fatalf("a suggestion about another class must be refused before the model runs: %v (asked %d)", err, len(asked))
	}
	// Off topic by intent: the model check refuses it.
	_, err = x.ProposeGuided(context.Background(), head, p, 4, "Use a logger instead of System.out.println", "add a discount code to checkout")
	if !errors.As(err, &off) || !strings.Contains(off.Reason, "discount") {
		t.Fatalf("an unrelated suggestion must be refused: %v", err)
	}
	// On topic: the fix follows it, and the suggestion reaches the fix model.
	prop, err := x.ProposeGuided(context.Background(), head, p, 4, "Use a logger instead of System.out.println", "log it at debug level")
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

	// A merge: both branches change v() and w(), which are next to each other, so they conflict as one.
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
	// Both sides changed v() and w(), so git conflicts on both: line 2 is new and line 3 is the side Bob kept.
	if len(files) != 1 || !slices.Equal(files[0].AddedLines, []int{2, 3}) {
		t.Fatalf("a merge should be reviewed only for its conflict resolution (lines 2 and 3 of B): %+v", files)
	}
}

func TestFixCoversEveryPlaceAndAnalyserFindingsStayOpen(t *testing.T) {
	dir, data := t.TempDir(), t.TempDir()
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=a@b", "-c", "user.name=Dev"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
		return strings.TrimSpace(string(out))
	}
	a, b := "src/main/java/x/A.java", "src/main/java/x/B.java"
	os.MkdirAll(filepath.Join(dir, "src/main/java/x"), 0o755)
	os.WriteFile(filepath.Join(dir, a), []byte("package x;\nclass A {\n    void a() { }\n    void x() {\n        System.out.println(\"a\");\n    }\n}\n"), 0o644)
	git("init", "-q", "-b", "main")
	git("add", "-A")
	git("commit", "-qm", "one")
	c1 := git("rev-parse", "HEAD")
	os.WriteFile(filepath.Join(dir, b), []byte("package x;\nclass B {\n    void x() {\n        System.out.println(\"b\");\n    }\n}\n"), 0o644)
	git("add", "-A")
	git("commit", "-qm", "two")
	c2 := git("rev-parse", "HEAD")

	repo, _ := gitx.Open(config.Config{RepoPath: dir})
	kb, _ := knowledge.Load()
	rules := func() (javaidx.Context, *knowledge.Base, bool) {
		g := javaidx.Index(repo.JavaFilesWorking(), "wt")
		return javaidx.ProjectContext(&g), kb, true
	}
	scan := func(path string, rev string) []Finding {
		src, _ := repo.ReadFile(rev, path)
		ctx, _, _ := rules()
		var out []Finding
		for _, f := range RuleScan(path, []byte(src), ctx, kb) {
			if f.Ref == "console-output" {
				out = append(out, f)
			}
		}
		return out
	}
	fa, fb := scan(a, c1), scan(b, c2)
	if len(fa) != 1 || len(fb) != 1 {
		t.Fatalf("scan: %+v %+v", fa, fb)
	}
	store, _ := NewStore(data, "r")
	store.Save(Review{SHA: c1, Status: StatusDone, Files: []FileReview{{Path: a, Findings: append(fa,
		Finding{Line: 3, EndLine: 3, Severity: "medium", Title: "Unwritten field (SpotBugs)", Source: "rule", Ref: "spotbugs:UWF_UNWRITTEN_FIELD", Snippet: "void a() { }"})}}})
	store.Save(Review{SHA: c2, Status: StatusDone, Files: []FileReview{{Path: b, Findings: fb}}})
	ig := NewIgnores(data, "r", dir)

	tr := NewTracker(repo, store, ig)
	tr.Rules = rules
	for _, is := range tr.Issues() {
		if is.Ref == "spotbugs:UWF_UNWRITTEN_FIELD" && is.State != "open" {
			t.Fatalf("an analyser finding must stay open while its code is there: %+v", is)
		}
	}

	x := &Fixer{repo: repo, store: store, Ignores: ig, Rules: rules, proposals: map[string]*Proposal{}, prefetched: map[string]*Proposal{}}
	p, err := x.ProposeGuided(context.Background(), c1, a, fa[0].Line, fa[0].Title, "", FixRef{SHA: c2, Path: b, Line: fb[0].Line, Title: fb[0].Title})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Paths) != 2 {
		t.Fatalf("the fix must cover both places, got %v", p.Paths)
	}
	for _, e := range p.edits {
		if strings.Contains(e.proposed, "System.out") {
			t.Fatalf("%s still prints:\n%s", e.path, e.proposed)
		}
	}
}

// Issues the developer fixed must close even when the model's anchor line stays.
func TestFixedIssuesClose(t *testing.T) {
	dir, data := t.TempDir(), t.TempDir()
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=a@b", "-c", "user.name=Dev"}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
		return strings.TrimSpace(string(out))
	}
	p := "src/main/java/x/A.java"
	os.MkdirAll(filepath.Join(dir, "src/main/java/x"), 0o755)
	os.WriteFile(filepath.Join(dir, p), []byte("class A {\n    void x(Req req) {\n        if (req.id() == 12) {\n            req = null;\n        }\n        log(req);\n    }\n}\n"), 0o644)
	git("init", "-q", "-b", "main")
	git("add", "-A")
	git("commit", "-qm", "one")
	c1 := git("rev-parse", "HEAD")
	os.WriteFile(filepath.Join(dir, p), []byte("class A {\n    void x(Req req) {\n        if (req.id() == 12) {\n            audit(req);\n        }\n    }\n}\n"), 0o644)
	git("commit", "-qam", "stop nulling the request")
	c2 := git("rev-parse", "HEAD")

	repo, _ := gitx.Open(config.Config{RepoPath: dir})
	store, _ := NewStore(data, "r")
	store.Save(Review{SHA: c1, Status: StatusDone, Files: []FileReview{{Path: p, Findings: []Finding{
		{Line: 3, EndLine: 3, Severity: "high", Title: "Null assignment", Source: "llm", Ref: "x", Snippet: "if (req.id() == 12) {"}, // anchor survives
		{Line: 6, EndLine: 6, Severity: "info", Title: "No snippet", Source: "llm"},                                                  // saved without code
		{Line: 2, EndLine: 2, Severity: "low", Title: "Elsewhere", Source: "llm", Ref: "y", Snippet: "void x(Req req) {"},            // really still open
		{Line: 1, EndLine: 1, Severity: "medium", Title: "A constant changed", Source: "rule", Ref: "shared-constant-changed", Snippet: "class A {"},
	}}}})
	store.Save(Review{SHA: c2, Status: StatusDone, Files: []FileReview{{Path: p, Findings: []Finding{}}}})
	tr := NewTracker(repo, store, NewIgnores(data, "r", dir))
	tr.Resolved = NewResolutions(data, "r")
	state := map[string]Issue{}
	for _, is := range tr.Issues() {
		state[is.Title] = is
	}
	if s := state["Null assignment"]; s.State != "resolved" || s.FixedIn != c2 {
		t.Fatalf("anchor survived but the code changed: %+v", s)
	}
	if s := state["No snippet"]; s.State != "resolved" {
		t.Fatalf("a finding saved without a snippet must be able to close: %+v", s)
	}
	if _, listed := state["A constant changed"]; listed {
		t.Fatal("a heads-up is not an open issue")
	}
	el := state["Elsewhere"]
	if el.State != "open" {
		t.Fatalf("untouched code stays open: %+v", el)
	}
	if err := tr.Resolved.Set([]string{el.ID}, Resolution{By: "Dev", Manual: true}); err != nil {
		t.Fatal(err)
	}
	for _, is := range tr.Issues() {
		if is.ID == el.ID && (is.State != "resolved" || !is.Manual || is.FixedBy != "Dev") {
			t.Fatalf("mark as fixed: %+v", is)
		}
	}
	tr.Resolved.Remove(el.ID)
	for _, is := range tr.Issues() {
		if is.ID == el.ID && is.State != "open" {
			t.Fatalf("reopen: %+v", is)
		}
	}
}

func TestCancelStopsTheModelRequest(t *testing.T) {
	started := make(chan struct{})
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body) // the server only notices a disconnect once the body is read
		close(started)
		<-r.Context().Done()
	}))
	defer llm.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := NewOllama(llm.URL, "m", 2048, time.Minute).With(ctx).ChatText("s", "u")
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not stop the request")
	}
}

func TestActivityRecordsWhatWentInAndOut(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": `{"ok":true}`}, "prompt_eval_count": 11, "eval_count": 4})
	}))
	defer llm.Close()
	dir := t.TempDir()
	var seen []Activity
	Activities = NewRecorder(dir, "r", func(a Activity) { seen = append(seen, a) })
	defer func() { Activities = nil }()
	if _, err := NewOllama(llm.URL, "m", 2048, time.Minute).For(Label{Kind: "fix", Title: "Fix it", SHA: "abc", Path: "A.java", Knowledge: []string{"k1"}}).ChatJSON("SYSTEM TEXT", "USER TEXT"); err != nil {
		t.Fatal(err)
	}
	list := Activities.List(10)
	if len(list) != 1 || list[0].Status != "done" || list[0].Kind != "fix" || list[0].User != "" || list[0].PromptTok != 11 || list[0].ReplyTok != 4 {
		t.Fatalf("list: %+v", list)
	}
	full, ok := Activities.Get(list[0].ID)
	if !ok || full.System != "SYSTEM TEXT" || full.User != "USER TEXT" || full.Response != `{"ok":true}` || full.Knowledge[0] != "k1" {
		t.Fatalf("detail: %+v", full)
	}
	if len(seen) != 2 || seen[0].Status != "running" || seen[0].User != "" {
		t.Fatalf("published: %+v", seen)
	}
	Activities.Step("Rule checks", "abc", "A.java", "2 finding(s)", time.Millisecond)
	if reloaded := NewRecorder(dir, "r", nil).List(10); len(reloaded) != 2 || reloaded[1].User != "" {
		t.Fatalf("reload: %+v", reloaded)
	}
}

func TestClassify(t *testing.T) {
	for _, c := range []struct {
		f    Finding
		want Class
	}{
		{Finding{Ref: "uninjected-dependency", Severity: "critical", Category: "spring"}, Class{"Bug", "Hotfix", "Minor"}},
		{Finding{Ref: "console-output", Severity: "low", Category: "logging"}, Class{"Better log", "Quick fix", "Minor"}},
		{Finding{Ref: "query-in-loop", Severity: "high", Category: "performance"}, Class{"Optimization", "Fix soon", "Major"}},
		{Finding{Ref: "commented-out-code", Severity: "low", Category: "design"}, Class{"Dead code", "Quick fix", "Minor"}},
		{Finding{Ref: "better-method", Severity: "low", Category: "style"}, Class{"Readability", "Cleanup", "Minor"}},
		{Finding{Ref: "x", Severity: "high", Category: "security"}, Class{"Security", "Hotfix", "Minor"}},
		{Finding{Ref: "high-complexity", Severity: "medium", Category: "design"}, Class{"Poor coding", "Fix soon", "Major"}},
	} {
		if got := Classify(c.f, 1); got != c.want {
			t.Errorf("%s: got %+v want %+v", c.f.Ref, got, c.want)
		}
	}
}

func newRepoT(t *testing.T) (string, func(who string, args ...string) string) {
	dir := t.TempDir()
	git := func(who string, args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=" + who + "@x", "-c", "user.name=" + who}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("Ann", "init", "-q", "-b", "main")
	return dir, git
}

func TestRewordKeepsContentAndRefusesPushed(t *testing.T) {
	dir, git := newRepoT(t)
	for i, m := range []string{"one", "wip", "three"} {
		os.WriteFile(filepath.Join(dir, "f.txt"), []byte(strings.Repeat("x\n", i+1)), 0o644)
		git("Ann", "add", "-A")
		git("Ann", "commit", "-qm", m)
	}
	os.WriteFile(filepath.Join(dir, "local.txt"), []byte("uncommitted\n"), 0o644)
	treeBefore := git("Ann", "rev-parse", "HEAD^{tree}")
	authorBefore := git("Ann", "log", "--format=%an %ae %at", "-3")
	target := git("Ann", "rev-parse", "HEAD~1")
	repo, _ := gitx.Open(config.Config{RepoPath: dir})
	mapping, err := repo.Reword(target, "fix(core): say what it does")
	if err != nil {
		t.Fatal(err)
	}
	if len(mapping) != 2 {
		t.Fatalf("the commit and the one after it are recreated: %v", mapping)
	}
	if got := git("Ann", "log", "--format=%s", "-3"); got != "three\nfix(core): say what it does\none" {
		t.Fatalf("messages:\n%s", got)
	}
	if git("Ann", "rev-parse", "HEAD^{tree}") != treeBefore || git("Ann", "log", "--format=%an %ae %at", "-3") != authorBefore {
		t.Fatal("content, authors and dates must not change")
	}
	if st := git("Ann", "status", "--short"); st != "?? local.txt" {
		t.Fatalf("local changes must survive: %q", st)
	}
	remote := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("%s", out)
	}
	git("Ann", "remote", "add", "origin", remote)
	git("Ann", "push", "-q", "origin", "main")
	if _, err := repo.Reword(repo.Head(), "nope"); err == nil || !strings.Contains(err.Error(), "already pushed") {
		t.Fatalf("pushed commits must be refused: %v", err)
	}
}

func TestBranchAlerts(t *testing.T) {
	dir, git := newRepoT(t)
	write := func(p, s string) { os.WriteFile(filepath.Join(dir, p), []byte(s), 0o644) }
	write("a.txt", "1\n2\n3\n")
	write("b.txt", "b\n")
	git("Ann", "add", "-A")
	git("Ann", "commit", "-qm", "start")
	git("Ann", "checkout", "-qb", "ana/one")
	write("a.txt", "1\nANA\n3\n")
	git("Ana", "commit", "-qam", "ana")
	git("Ann", "checkout", "-q", "main")
	git("Ann", "checkout", "-qb", "ravi/two")
	write("a.txt", "1\nRAVI\n3\n")
	git("Ravi", "commit", "-qam", "ravi")
	git("Ann", "checkout", "-q", "main")
	git("Ann", "checkout", "-qb", "sam/three")
	write("b.txt", "sam\n")
	git("Sam", "commit", "-qam", "sam")
	git("Ann", "checkout", "-q", "main")
	git("Ann", "branch", "old")
	write("a.txt", "1\nMAIN\n3\n")
	git("Ann", "commit", "-qam", "main moves")
	repo, _ := gitx.Open(config.Config{RepoPath: dir})
	rep, err := Branches(repo, []string{"b.txt"})
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, a := range rep.Alerts {
		titles = append(titles, a.Severity+": "+a.Title+" "+strings.Join(a.People, ","))
	}
	all := strings.Join(titles, "\n")
	for _, want := range []string{"conflict: ana/one conflicts with main Ana", "conflict: ravi/two conflicts with main Ravi", "conflict: ana/one and ravi/two conflict Ana,Ravi", "overlap: Your local changes touch files changed on sam/three Sam"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in:\n%s", want, all)
		}
	}
	if strings.Contains(all, "sam/three conflicts") {
		t.Errorf("sam/three merges cleanly:\n%s", all)
	}
	for _, b := range rep.Branches {
		if b.Name == "old" && !b.Merged {
			t.Errorf("old has nothing to merge: %+v", b)
		}
	}
}

func TestCommitPlanGroupsRelatedFiles(t *testing.T) {
	w := Working{Changes: []gitx.FileChange{
		{ChangeType: "MODIFY", NewPath: "order-service/src/main/java/o/OrderService.java", Path: "order-service/src/main/java/o/OrderService.java"},
		{ChangeType: "MODIFY", NewPath: "order-service/src/main/java/o/OrderController.java", Path: "order-service/src/main/java/o/OrderController.java"},
		{ChangeType: "ADD", NewPath: "order-service/src/test/java/o/OrderServiceTest.java", Path: "order-service/src/test/java/o/OrderServiceTest.java"},
		{ChangeType: "MODIFY", NewPath: "product-service/src/main/java/p/Price.java", Path: "product-service/src/main/java/p/Price.java"},
		{ChangeType: "MODIFY", NewPath: "README.md", Path: "README.md"},
		{ChangeType: "MODIFY", NewPath: "pom.xml", Path: "pom.xml"},
	}}
	g := &javaidx.Graph{Nodes: []javaidx.Node{{ID: "o.OrderController", File: "order-service/src/main/java/o/OrderController.java"}, {ID: "o.OrderService", File: "order-service/src/main/java/o/OrderService.java"}},
		Edges: []javaidx.Edge{{Source: "o.OrderController", Target: "o.OrderService", Kind: "injects"}}}
	plan := CommitPlan(g, w)
	if len(plan) != 4 {
		t.Fatalf("expected build, order (with its test), product and docs: %+v", plan)
	}
	if !strings.HasPrefix(plan[0].Message, "build") || !strings.HasPrefix(plan[len(plan)-1].Message, "docs") {
		t.Fatalf("order: %+v", plan)
	}
	for _, c := range plan {
		if strings.Contains(c.Message, "(order)") && len(c.Files) != 3 {
			t.Fatalf("the service, controller and test belong together: %+v", c)
		}
	}
}

func TestMemoryReachesPromptsAndCanBeShared(t *testing.T) {
	data, root := t.TempDir(), t.TempDir()
	m := NewMemory(data, "r", root)
	a, err := m.Add(MemoryItem{Text: "We keep field injection in legacy controllers", Ref: "field-injection", Title: "Field injection", Kind: "rejected-fix", By: "Ann"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Add(MemoryItem{Text: "Never log card tokens", By: "Ann"}); err != nil {
		t.Fatal(err)
	}
	if got := m.ForPrompt("A.java", "field-injection"); !strings.Contains(got, "legacy controllers") || !strings.Contains(got, "card tokens") {
		t.Fatalf("prompt: %s", got)
	}
	if got := m.ForPrompt("A.java", "console-output"); strings.Contains(got, "legacy controllers") || !strings.Contains(got, "card tokens") {
		t.Fatalf("another rule must not get this item: %s", got)
	}
	if err := m.Share(a.ID, true); err != nil {
		t.Fatal(err)
	}
	other := NewMemory(t.TempDir(), "r", root) // a teammate's checkout
	if got := other.All(); len(got) != 1 || !got[0].Team {
		t.Fatalf("teammate sees: %+v", got)
	}
	if err := m.Share(a.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := m.Remove(a.ID); err != nil {
		t.Fatal(err)
	}
	if len(m.All()) != 1 {
		t.Fatalf("after remove: %+v", m.All())
	}
}

func TestCheckImportsCorrectsWrongPackageAndOffersRealClasses(t *testing.T) {
	x := &Fixer{Known: func() map[string]bool {
		return map[string]bool{
			"com.demo.shop.customer.CustomerService":  true,
			"com.demo.shop.error.NotFoundException":   true,
			"com.demo.shop.error.BadRequestException": true,
		}
	}}
	before := "package com.demo.shop.customer;\n\nclass A {}\n"
	after := "package com.demo.shop.customer;\n\nimport com.demo.shop.common.NotFoundException;\nimport com.demo.shop.common.ConflictException;\n\nclass A {}\n"
	out, bad := x.checkImports(before, after)
	if !strings.Contains(out, "import com.demo.shop.error.NotFoundException;") {
		t.Errorf("a real class in the wrong package should be corrected:\n%s", out)
	}
	if len(bad) != 1 || bad[0] != "com.demo.shop.common.ConflictException" {
		t.Fatalf("bad = %v", bad)
	}
	got := x.importCandidates(bad)
	if len(got) != 2 || got[0] != "com.demo.shop.error.BadRequestException" {
		t.Errorf("candidates = %v, want the project's exceptions", got)
	}
}

func TestSameCodeIgnoresOnlySpacing(t *testing.T) {
	if !sameCode("import a;\n\nimport b;\nclass A {}\n", "import a;\nimport b;\nclass A  {}\n") {
		t.Error("removing a blank line is not a change")
	}
	if sameCode("int a = 1;", "int a = 2;") {
		t.Error("a changed value is a change")
	}
}

// A model answers with what it is scripted to, one reply per call.
func scriptedFixer(t *testing.T, known map[string]bool, replies ...string) (*Fixer, *Ollama, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		reply := replies[min(calls, len(replies)-1)]
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": "```java\n" + reply + "\n```"}, "done": true})
	}))
	t.Cleanup(srv.Close)
	return &Fixer{Known: func() map[string]bool { return known }}, &Ollama{URL: srv.URL, Model: "m", HTTP: srv.Client()}, &calls
}

func TestGenerateKeepsAskingUntilTheFixIsReal(t *testing.T) {
	known := map[string]bool{"com.demo.A": true, "com.demo.B": true}
	current := "package com.demo;\n\nimport com.demo.B;\n\nclass A { int x = 1; }\n"
	invented := "package com.demo;\n\nimport com.demo.nope.Missing;\nimport com.demo.B;\n\nclass A { int x = 2; }"
	blank := "package com.demo;\nimport com.demo.B;\n\nclass A { int x = 1; }"
	fixed := "package com.demo;\n\nimport com.demo.B;\n\nclass A { int x = 2; }"

	x, llm, calls := scriptedFixer(t, known, invented, blank, fixed)
	got, _, err := x.generate(llm, "s", "t", "A.java", current)
	if err != nil || !strings.Contains(got, "x = 2") || *calls != 3 {
		t.Fatalf("got %q, err %v, calls %d", got, err, *calls)
	}

	x, llm, calls = scriptedFixer(t, known, blank)
	// The second retry is the same prompt as the first, so it is answered without another call.
	if _, _, err = x.generate(llm, "s", "t", "A.java", current); err == nil || *calls != 2 {
		t.Fatalf("a whitespace-only fix must be refused after retries: err %v, calls %d", err, *calls)
	}
}

func TestIdenticalModelCallsAreMadeOnce(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		calls++
		mu.Unlock()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": "answer"}})
	}))
	defer srv.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	o := &Ollama{URL: srv.URL, Model: "m", HTTP: srv.Client()}
	count := func() int { mu.Lock(); defer mu.Unlock(); return calls }

	// The first caller cancels: the one that joined it gets its own call, and nothing is lost.
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := o.With(ctx).ChatText("s", "u"); first <- err }()
	for count() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	var wg sync.WaitGroup
	answers := make([]string, 4)
	for i := range answers {
		wg.Add(1)
		go func() { defer wg.Done(); answers[i], _ = o.ChatText("s", "u") }()
	}
	time.Sleep(50 * time.Millisecond)
	if n := count(); n != 1 {
		t.Fatalf("joined callers made their own calls: %d", n)
	}
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("the cancelled caller should see it: %v", err)
	}
	for count() < 2 {
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	wg.Wait()
	for _, a := range answers {
		if a != "answer" {
			t.Fatalf("answers = %v", answers)
		}
	}
	if n := count(); n != 2 {
		t.Errorf("calls = %d, want the cancelled one and one more", n)
	}

	// Asked again later: no call. Asked something else: a call.
	if a, _ := o.ChatText("s", "u"); a != "answer" || count() != 2 {
		t.Errorf("a repeat should reuse the answer: %q, %d calls", a, count())
	}
	if _, _ = o.ChatText("s", "different"); count() != 3 {
		t.Errorf("a different prompt needs its own call: %d", count())
	}
}

func TestWeighClaim(t *testing.T) {
	code := strings.Split(`class A {
  String a(Map<String, B> m, String k) {
    B b = m.get(k);
    return b.name();
  }
  String c(C c) {
    return c.name();
  }
  String d(D d) {
    if (d == null) { log(); }
    return d.name();
  }
}`, "\n")
	cases := []struct {
		name, severity, title, ref, evidence, patch string
		line                                        int
		want                                        string
		doubted                                     bool
	}{
		{"value from a map lookup", "high", "Null dereference on some path", "null-dereference", "return b.name();", "", 4, "high", false},
		{"value tested for null nearby", "high", "Null dereference on some path", "", "return d.name();", "", 11, "high", false},
		{"nothing makes it null", "high", "Null dereference on some path", "null-dereference", "return c.name();", "", 7, "low", true},
		{"no quote to check", "critical", "Potential NullPointerException", "", "", "", 7, "low", true},
		{"the change removed a null check", "high", "Null header not handled", "", "return c.name();", "--- a\n+++ b\n-  if (c == null) return;\n", 7, "high", false},
		{"a +++ header is not a removal", "high", "Potential NPE", "", "return c.name();", "--- null\n+++ b\n", 7, "low", true},
		{"only describes the change", "medium", "Improved variable naming for clarity", "", "return c.name();", "", 7, "low", true},
		{"already low stays low", "info", "Simplified the method", "", "return c.name();", "", 7, "info", true},
		{"a removed check is a real claim", "high", "Removed br tag check", "", "return c.name();", "", 7, "high", false},
		{"an ordinary claim is untouched", "high", "Wrong index used for the last item", "", "return c.name();", "", 7, "high", false},
		{"a word holding null is not a null claim", "high", "Annulled order still charged", "", "return c.name();", "", 7, "high", false},
	}
	for _, c := range cases {
		got, doubt := weighClaim(c.severity, c.title, c.ref, c.evidence, code, c.line, c.patch)
		if got != c.want || (doubt != "") != c.doubted {
			t.Errorf("%s: got %q, doubt %q", c.name, got, doubt)
		}
	}
}

func TestTypicalFileMs(t *testing.T) {
	r := &Recorder{}
	add := func(kind, status, model string, ms int64) {
		r.items = append(r.items, &Activity{Kind: kind, Status: status, Model: model, TookMs: ms})
	}
	add("review-file", "done", "m", 100)
	add("review-file", "done", "m", 300)
	if got := r.TypicalFileMs("m"); got != 0 {
		t.Errorf("two reviews are too few: %d", got)
	}
	add("review-file", "done", "m", 200)
	add("review-file", "failed", "m", 9000)
	add("review-commit", "done", "m", 9000)
	add("review-file", "done", "other", 9000)
	if got := r.TypicalFileMs("m"); got != 200 {
		t.Errorf("median of the same model's finished file reviews: %d", got)
	}
}
