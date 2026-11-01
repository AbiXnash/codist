package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"codist/internal/config"
)

func TestFollowingABranchReadsItWithoutTouchingTheCheckout(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=T", "-c", "user.email=t@t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "A.java"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	write("class A {}\n")
	git("add", ".")
	git("commit", "-qm", "one")
	git("checkout", "-qb", "other")
	write("class B {}\n")
	git("commit", "-qam", "two")
	git("checkout", "-q", "main")
	write("class Local {}\n") // an uncommitted edit on main

	r, err := Open(config.Config{RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	if r.Viewing() {
		t.Fatal("following the checked-out branch is not viewing")
	}
	if text, _, _ := r.ReadWorking("A.java"); text != "class Local {}\n" {
		t.Fatalf("checked out: read %q", text)
	}
	if err := r.SwitchBranch("nope"); err == nil || r.Branch() != "main" {
		t.Fatalf("an unknown branch must be refused and change nothing: %v, %s", err, r.Branch())
	}
	if err := r.SwitchBranch("other"); err != nil {
		t.Fatal(err)
	}
	r.checkout.Store(nil)
	if !r.Viewing() {
		t.Fatal("following another branch is viewing")
	}
	if text, _, _ := r.ReadWorking("A.java"); text != "class B {}\n" {
		t.Errorf("viewing: read %q, want the branch's version", text)
	}
	if got := string(r.JavaFilesWorking()["A.java"]); got != "class B {}\n" {
		t.Errorf("viewing: graph source %q", got)
	}
	if ch, err := r.WorkingChanges(); err != nil || len(ch) != 0 {
		t.Errorf("the local edit belongs to main, got %v %v", ch, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "A.java")); string(b) != "class Local {}\n" {
		t.Errorf("the working tree changed: %q", b)
	}
	names := map[string]bool{}
	for _, b := range r.ListBranches() {
		names[b.Name] = b.CheckedOut
	}
	if checked, ok := names["main"]; !ok || !checked {
		t.Errorf("branches = %v", names)
	}
	if _, ok := names["other"]; !ok {
		t.Errorf("branches = %v", names)
	}
}
