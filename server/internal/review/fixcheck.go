package review

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// FixCheck is what happened when a prepared fix was built and tested in a throwaway copy of the project.
type FixCheck struct {
	Status string `json:"status"` // passed, failed or unavailable
	Step   string `json:"step"`   // the step that decided it
	Output string `json:"output,omitempty"`
	TookMs int64  `json:"tookMs"`
}

const fixCheckLimit = 6 * time.Minute

// buildRunner runs one command in a directory and returns what it printed. Tests replace it.
type buildRunner func(ctx context.Context, dir, name string, args ...string) (string, error)

func runBuild(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

type buildStep struct {
	label string
	name  string
	args  []string
	retry []string // the same step online, when offline lacks something
}

// buildSteps finds how the project builds: its wrapper if it has one, then the tool on the PATH. Tests run
// only for the modules the fix touches, plus what they need.
func buildSteps(dir string, paths []string) []buildStep {
	has := func(f string) bool { _, err := os.Stat(filepath.Join(dir, f)); return err == nil }
	wrapper := func(unix, windows, tool string) string {
		name := unix
		if runtime.GOOS == "windows" {
			name = windows
		}
		if has(name) {
			return filepath.Join(dir, name)
		}
		if found, err := exec.LookPath(tool); err == nil {
			return found
		}
		return ""
	}
	switch {
	case has("pom.xml"):
		tool := wrapper("mvnw", "mvnw.cmd", "mvn")
		if tool == "" {
			return nil
		}
		scope := []string{}
		if mods := mavenModules(dir, paths); len(mods) > 0 {
			scope = []string{"-pl", strings.Join(mods, ","), "-am"}
		}
		both := func(goal ...string) ([]string, []string) {
			online := append(append([]string{"-q"}, scope...), goal...)
			return append([]string{"-o"}, online...), online
		}
		cOff, cOn := both("-DskipTests", "test-compile")
		tOff, tOn := both("-Dsurefire.failIfNoSpecifiedTests=false", "test")
		return []buildStep{{"compile", tool, cOff, cOn}, {"tests", tool, tOff, tOn}}
	case has("build.gradle") || has("build.gradle.kts"):
		tool := wrapper("gradlew", "gradlew.bat", "gradle")
		if tool == "" {
			return nil
		}
		return []buildStep{{"compile", tool, []string{"--offline", "-q", "testClasses"}, []string{"-q", "testClasses"}}, {"tests", tool, []string{"--offline", "-q", "test"}, []string{"-q", "test"}}}
	}
	return nil
}

// mavenModules are the modules holding the changed files, by the nearest pom.xml above each one.
func mavenModules(dir string, paths []string) []string {
	seen := map[string]bool{}
	var mods []string
	for _, p := range paths {
		for d := path.Dir(p); d != "." && d != "/"; d = path.Dir(d) {
			if _, err := os.Stat(filepath.Join(dir, d, "pom.xml")); err == nil {
				if !seen[d] {
					seen[d] = true
					mods = append(mods, d)
				}
				break
			}
		}
	}
	return mods
}

var missingOffline = []string{"offline mode", "Could not resolve", "cannot access", "No cached version", "plugin could not be resolved"}

// Check builds and tests a prepared fix in a throwaway copy, so your working tree is never touched. It
// reports what happened: you still decide, and a fix that fails can still be read and rejected.
func (x *Fixer) Check(ctx context.Context, id string) (FixCheck, error) {
	x.mu.Lock()
	p, ok := x.proposals[id]
	x.mu.Unlock()
	if !ok {
		return FixCheck{}, errors.New("this fix expired; generate it again")
	}
	began := time.Now()
	done := func(c FixCheck) (FixCheck, error) { c.TookMs = time.Since(began).Milliseconds(); return c, nil }
	dir, err := os.MkdirTemp("", "codist-fix-")
	if err != nil {
		return FixCheck{}, err
	}
	defer os.RemoveAll(dir)
	tree := filepath.Join(dir, "copy")
	if err := x.repo.AddCopy(tree); err != nil {
		return done(FixCheck{Status: "unavailable", Step: "copy", Output: err.Error()})
	}
	defer x.repo.RemoveCopy(tree)
	for _, e := range p.edits {
		rel := filepath.Clean(e.path)
		if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return FixCheck{}, fmt.Errorf("%s is outside the project", e.path)
		}
		full := filepath.Join(tree, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return FixCheck{}, err
		}
		if err := os.WriteFile(full, []byte(e.proposed), 0o644); err != nil {
			return FixCheck{}, err
		}
	}
	paths := make([]string, 0, len(p.edits))
	for _, e := range p.edits {
		paths = append(paths, filepath.ToSlash(e.path))
	}
	steps := buildSteps(tree, paths)
	if len(steps) == 0 {
		return done(FixCheck{Status: "unavailable", Step: "build tool", Output: "No Maven or Gradle was found for this project."})
	}
	run := x.Build
	if run == nil {
		run = runBuild
	}
	ctx, cancel := context.WithTimeout(ctx, fixCheckLimit)
	defer cancel()
	for _, st := range steps {
		out, err := run(ctx, tree, st.name, st.args...)
		if err != nil && st.retry != nil && needsOnline(out) {
			out, err = run(ctx, tree, st.name, st.retry...)
		}
		if ctx.Err() != nil {
			return done(FixCheck{Status: "unavailable", Step: st.label, Output: "It took longer than the limit of " + fixCheckLimit.String() + "."})
		}
		if err != nil {
			return done(FixCheck{Status: "failed", Step: st.label, Output: tail(out, 2500)})
		}
	}
	return done(FixCheck{Status: "passed", Step: "tests"})
}

func needsOnline(out string) bool {
	for _, m := range missingOffline {
		if strings.Contains(out, m) {
			return true
		}
	}
	return false
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return "…" + s[len(s)-n:]
	}
	return s
}
