package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
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
	all := r.History(nil, 50)
	if len(all) != 2 || len(all[0].Parents) != 1 || all[0].Subject != "two" || len(all[0].Refs) != 1 || all[0].Refs[0] != "other" {
		t.Errorf("all branches: %+v", all)
	}
	if only := r.History([]string{"main"}, 50); len(only) != 1 || only[0].Subject != "one" {
		t.Errorf("one branch: %+v", only)
	}
	if evil := r.History([]string{"--all", "nope"}, 50); len(evil) != 2 {
		t.Errorf("an unknown name must be ignored: %+v", evil)
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

func TestAuthorHistoryAndBlameCountLines(t *testing.T) {
	dir := t.TempDir()
	git := func(name string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=" + name, "-c", "user.email=t@t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	file := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("Ana", "init", "-q", "-b", "main")
	file("A.java", "one\ntwo\nthree\n")
	file("logo.png", "binary")
	git("Ana", "add", ".")
	git("Ana", "commit", "-qm", "a")
	file("A.java", "one\nTWO\nthree\nfour\n")
	git("Ravi", "commit", "-qam", "b")

	r, err := Open(config.Config{RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	totals := map[string]AuthorTotal{}
	for _, a := range r.AuthorHistory(func(p string) bool { return p == "A.java" }) {
		totals[a.Author] = a
	}
	if a := totals["Ana"]; a.Commits != 1 || a.Added != 3 || a.Removed != 0 || a.Files != 1 {
		t.Errorf("Ana = %+v", a)
	}
	if b := totals["Ravi"]; b.Commits != 1 || b.Added != 2 || b.Removed != 1 {
		t.Errorf("Ravi = %+v", b)
	}
	owned := r.BlameCounts(r.Head(), "A.java")
	if owned["Ana"] != 2 || owned["Ravi"] != 2 {
		t.Errorf("owned = %v", owned)
	}
}

func TestMergeReviewsOnlyWhatAPersonDecided(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=T", "-c", "user.email=t@t"}, args...)...)
		out, _ := cmd.CombinedOutput() // a conflicting merge exits non-zero on purpose
		return string(out)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	write("A.java", "a\nb\nc\n")
	write("B.java", "1\n2\n3\n")
	git("add", ".")
	git("commit", "-qm", "base")
	git("checkout", "-qb", "side")
	write("A.java", "a\nside\nc\n")
	write("B.java", "1\n2\n3\n4\n")
	git("commit", "-qam", "side")
	git("checkout", "-q", "main")
	write("A.java", "a\nmine\nc\n")
	git("commit", "-qam", "mine")
	git("merge", "-q", "side")
	write("A.java", "a\nmine\nside\nc\n") // keeps both sides, so no line is new to both parents
	git("add", ".")
	git("commit", "-qm", "merge with a resolved conflict")

	r, err := Open(config.Config{RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := r.Commit(strings.TrimSpace(git("rev-parse", "main")))
	if err != nil {
		t.Fatal(err)
	}
	files, err := r.Diff(resolved)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "A.java" || !reflect.DeepEqual(files[0].AddedLines, []int{2, 3}) {
		t.Fatalf("a resolved conflict must be reviewed on the lines kept, and only there: %+v", files)
	}

	git("checkout", "-q", "main")
	git("checkout", "-qb", "plain", "main~1")
	write("D.java", "d\n")
	git("add", ".")
	git("commit", "-qm", "d")
	git("checkout", "-qb", "plain2", "main~1")
	write("E.java", "e\n")
	git("add", ".")
	git("commit", "-qm", "e")
	git("merge", "-q", "--no-ff", "-m", "clean", "plain")
	clean, err := r.Commit(strings.TrimSpace(git("rev-parse", "plain2")))
	if err != nil {
		t.Fatal(err)
	}
	if files, err := r.Diff(clean); err != nil || len(files) != 0 {
		t.Fatalf("a clean merge has nothing of its own: %+v %v", files, err)
	}
}
