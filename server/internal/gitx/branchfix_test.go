package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"codist/internal/config"
)

func TestFixReachesOnlyTheBranchesWithTheBug(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	buggy := "class A {\n  int a = 1;\n  boolean bug = x == y;\n  int b = 2;\n}\n"
	run("init", "-q", "-b", "main")
	run("config", "user.name", "T")
	run("config", "user.email", "t@t")
	write("A.java", buggy)
	run("add", ".")
	run("commit", "-qm", "base")

	// Has the bug, and other work.
	run("checkout", "-qb", "same")
	write("B.java", "class B {}\n")
	run("add", ".")
	run("commit", "-qm", "unrelated work")
	// The line around the bug changed, so the fix cannot apply on its own.
	run("checkout", "-q", "-b", "clash", "main")
	write("A.java", strings.Replace(buggy, "x == y;", "x == y || z;", 1))
	run("commit", "-qam", "changes the buggy line")
	// Rewrote the code, so the bug is not there.
	run("checkout", "-q", "-b", "clean", "main")
	write("A.java", "class A {\n  int total = 3;\n}\n")
	run("commit", "-qam", "rewrite")
	// Fixed it the same way already.
	run("checkout", "-q", "-b", "done", "main")
	write("A.java", strings.Replace(buggy, "x == y", "x.equals(y)", 1))
	run("commit", "-qam", "fixed independently")
	// Moved the file to another folder, and still has the bug.
	run("checkout", "-q", "-b", "moved", "main")
	if err := os.MkdirAll(filepath.Join(dir, "svc"), 0o755); err != nil {
		t.Fatal(err)
	}
	run("mv", "A.java", "svc/A.java")
	run("commit", "-qm", "move into a service")
	// Has the bug, but someone is working there.
	run("checkout", "-q", "-b", "busy", "main")
	wt := filepath.Join(t.TempDir(), "busy")
	run("checkout", "-q", "main")
	run("worktree", "add", "-q", wt, "busy")

	write("A.java", strings.Replace(buggy, "x == y", "x.equals(y)", 1))
	run("commit", "-qam", "fix: compare by value")
	fix := run("rev-parse", "HEAD")
	write("local.txt", "uncommitted\n")

	r, err := Open(config.Config{RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	list, err := r.BranchFixes(fix)
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range list {
		got[b.Branch] = b.Status
	}
	want := map[string]string{"same": "ready", "clash": "conflict", "clean": "unaffected", "done": "already", "busy": "skipped", "moved": "ready"}
	for b, s := range want {
		if got[b] != s {
			t.Errorf("%s: got %q, want %q (all %v)", b, got[b], s, got)
		}
	}

	before := run("rev-parse", "clash")
	results, err := r.ApplyToBranches(fix, []string{"same", "moved", "clash", "busy", "main", "nope"})
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]BranchFix{}
	for _, b := range results {
		status[b.Branch] = b
	}
	if status["same"].Status != "fixed" || status["same"].Commit == "" {
		t.Fatalf("same = %+v", status["same"])
	}
	if src, ok := r.ReadFile("refs/heads/moved", "svc/A.java"); !ok || !strings.Contains(src, "x.equals(y)") {
		t.Errorf("the moved file was not fixed: %q", src)
	}
	if _, ok := r.ReadFile("refs/heads/moved", "A.java"); ok {
		t.Error("the fix must not bring the old path back")
	}
	for _, b := range []string{"clash", "busy", "main", "nope"} {
		if status[b].Status == "fixed" {
			t.Errorf("%s must not change: %+v", b, status[b])
		}
	}
	if run("rev-parse", "clash") != before {
		t.Error("a branch with a conflict moved")
	}
	if src, _ := r.ReadFile("refs/heads/same", "A.java"); !strings.Contains(src, "x.equals(y)") {
		t.Errorf("same was not fixed: %s", src)
	}
	if src, ok := r.ReadFile("refs/heads/same", "B.java"); !ok || src == "" {
		t.Error("the branch lost its own work")
	}
	if run("rev-parse", "same^") == fix {
		t.Error("the fix should be a new commit on top of the branch")
	}
	if !strings.Contains(run("log", "-1", "--format=%B", "same"), "cherry picked from commit "+fix) {
		t.Error("the commit should say where it came from")
	}
	if run("rev-parse", "--abbrev-ref", "HEAD") != "main" {
		t.Error("nothing should be checked out")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "local.txt")); string(b) != "uncommitted\n" {
		t.Error("the working tree changed")
	}
	// Asking again finds nothing left to do on that branch.
	again, _ := r.BranchFixes(fix)
	for _, b := range again {
		if b.Branch == "same" && b.Status == "ready" {
			t.Errorf("same is fixed already: %+v", b)
		}
	}
}
