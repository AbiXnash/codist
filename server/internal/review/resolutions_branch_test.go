package review

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"codist/internal/config"
	"codist/internal/gitx"
)

func TestClosedIssuesStayOnTheirBranch(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "-q", "-b", "main")
	run("config", "user.name", "T")
	run("config", "user.email", "t@t")
	if err := os.WriteFile(filepath.Join(dir, "A.java"), []byte("class A {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-qm", "base")
	run("branch", "other")
	run("commit", "-q", "--allow-empty", "-m", "fix on main")
	fix := run("rev-parse", "HEAD")

	repo, err := gitx.Open(config.Config{RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	tr := &Tracker{repo: repo}
	if !tr.closedHere(Resolution{Commit: fix}) {
		t.Error("closed by a commit on this branch")
	}
	if !tr.closedHere(Resolution{Manual: true, Branch: "main"}) {
		t.Error("marked fixed on this branch")
	}
	if err := repo.SwitchBranch("other"); err != nil {
		t.Fatal(err)
	}
	if tr.closedHere(Resolution{Commit: fix}) {
		t.Error("the fix is not on this branch, so it still has the bug")
	}
	if tr.closedHere(Resolution{Manual: true, Branch: "main"}) {
		t.Error("marked fixed on another branch")
	}
	if !tr.closedHere(Resolution{Manual: true}) {
		t.Error("older records apply everywhere")
	}
}
