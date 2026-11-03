package review

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"codist/internal/config"
	"codist/internal/gitx"
)

func fixRepo(t *testing.T) (*Fixer, string) {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=T", "-c", "user.email=t@t"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	os.MkdirAll(filepath.Join(dir, "svc/src"), 0o755)
	os.WriteFile(filepath.Join(dir, "pom.xml"), []byte("<project/>"), 0o644)
	os.WriteFile(filepath.Join(dir, "svc/pom.xml"), []byte("<project/>"), 0o644)
	os.WriteFile(filepath.Join(dir, "svc/src/A.java"), []byte("class A { int x = 1 }\n"), 0o644)
	git("init", "-q", "-b", "main")
	git("add", "-A")
	git("commit", "-qm", "one")
	repo, err := gitx.Open(config.Config{RepoPath: dir})
	if err != nil {
		t.Fatal(err)
	}
	x := &Fixer{repo: repo, proposals: map[string]*Proposal{"f": {ID: "f", edits: []edit{{path: "svc/src/A.java", original: "class A { int x = 1 }\n", proposed: "class A { int x = 2; }\n"}}}}}
	return x, dir
}

func TestCheckBuildsTheFixInACopyAndLeavesYourFilesAlone(t *testing.T) {
	x, dir := fixRepo(t)
	if _, err := exec.LookPath("mvn"); err != nil {
		t.Skip("needs mvn on the PATH to choose the Maven steps")
	}
	var ran [][]string
	x.Build = func(_ context.Context, tree, name string, args ...string) (string, error) {
		ran = append(ran, args)
		got, _ := os.ReadFile(filepath.Join(tree, "svc/src/A.java"))
		if !strings.Contains(string(got), "x = 2") {
			t.Errorf("the build must see the fix, got %q", got)
		}
		return "", nil
	}
	c, err := x.Check(context.Background(), "f")
	if err != nil || c.Status != "passed" {
		t.Fatalf("%+v %v", c, err)
	}
	if len(ran) != 2 || !strings.Contains(strings.Join(ran[0], " "), "-pl svc -am") {
		t.Errorf("compile then tests, only for the touched module: %v", ran)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "svc/src/A.java")); strings.Contains(string(got), "x = 2") {
		t.Error("your working tree must not change")
	}
	if out, _ := exec.Command("git", "-C", dir, "worktree", "list").Output(); strings.Count(string(out), "\n") != 1 {
		t.Errorf("the copy must be cleaned up: %s", out)
	}
}

func TestCheckReportsAFailedStepAndRetriesOnlineWhenOfflineLacksSomething(t *testing.T) {
	x, _ := fixRepo(t)
	if _, err := exec.LookPath("mvn"); err != nil {
		t.Skip("needs mvn on the PATH to choose the Maven steps")
	}
	calls := 0
	x.Build = func(_ context.Context, _, _ string, args ...string) (string, error) {
		calls++
		if strings.Contains(strings.Join(args, " "), "-o ") {
			return "Could not resolve plugin in offline mode", errors.New("exit 1")
		}
		return "A.java:[1,20] ';' expected", errors.New("exit 1")
	}
	c, err := x.Check(context.Background(), "f")
	if err != nil || c.Status != "failed" || c.Step != "compile" || !strings.Contains(c.Output, "expected") || calls != 2 {
		t.Fatalf("%+v %v calls=%d", c, err, calls)
	}
}
