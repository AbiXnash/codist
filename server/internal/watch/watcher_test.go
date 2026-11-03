package watch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codist/internal/config"
	"codist/internal/gitx"
)

func TestMissedCommitsWhenSwitchingBranches(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=T", "-c", "user.email=t@t"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
		return strings.TrimSpace(string(out))
	}
	commit := func(name string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
		git("add", ".")
		git("commit", "-qm", name)
		return git("rev-parse", "HEAD")
	}
	git("init", "-q", "-b", "main")
	base := commit("base")
	old := commit("old")
	git("checkout", "-qb", "feature")
	made := time.Now()
	first := commit("first")
	second := commit("second")

	r, err := gitx.Open(config.Config{RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	w := &Watcher{repo: r}
	shas := func(cs []gitx.Commit) string {
		var s []string
		for _, c := range cs {
			s = append(s, c.SHA)
		}
		return strings.Join(s, " ")
	}

	if got := shas(w.missed(old, "", second, time.Time{})); got != first+" "+second {
		t.Errorf("everything since the branch was last followed: %q", got)
	}
	if got := shas(w.missed("", old, second, made)); got != first+" "+second {
		t.Errorf("a new branch's new commits: %q", got)
	}
	if got := shas(w.missed("", old, second, time.Time{})); got != "" {
		t.Errorf("only looking at a branch reviews nothing: %q", got)
	}
	if got := shas(w.missed("", base, second, made.Add(time.Hour))); got != "" {
		t.Errorf("commits older than the last check are history: %q", got)
	}
	if got := shas(w.missed(second, old, second, made)); got != "" {
		t.Errorf("nothing new: %q", got)
	}
}
