package review

import (
	"fmt"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"codist/internal/gitx"
)

// BranchInfo is one branch compared with the tracked one.
type BranchInfo struct {
	Name      string   `json:"name"`
	Remote    bool     `json:"remote"`
	SHA       string   `json:"sha"`
	Ahead     int      `json:"ahead"`
	Behind    int      `json:"behind"`
	Authors   []string `json:"authors"`
	LastBy    string   `json:"lastBy"`
	LastAt    int64    `json:"lastAt"`
	LastMsg   string   `json:"lastMsg"`
	Files     []string `json:"files"`
	Conflicts []string `json:"conflicts"` // files that conflict when merged into the tracked branch
	Merged    bool     `json:"merged"`
}

// BranchAlert warns about two lines of work colliding.
type BranchAlert struct {
	Severity string   `json:"severity"` // conflict, overlap
	Title    string   `json:"title"`
	Detail   string   `json:"detail"`
	A        string   `json:"a"`
	B        string   `json:"b"`
	People   []string `json:"people"`
	Files    []string `json:"files"`
}

type BranchReport struct {
	Base     string        `json:"base"`
	Branches []BranchInfo  `json:"branches"`
	Alerts   []BranchAlert `json:"alerts"`
	Local    []string      `json:"local"` // files you have changed and not committed
	TookMs   int64         `json:"tookMs"`
}

// parallel runs fn over n items on every core.
func parallel(n int, fn func(i int)) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	for i := 0; i < n; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i)
		}(i)
	}
	wg.Wait()
}

// mergeConflicts asks git what merging b into a would conflict on, without touching anything.
func mergeConflicts(root, a, b string) ([]string, error) {
	cmd := exec.Command("git", "-C", root, "merge-tree", "--write-tree", "--name-only", "--no-messages", a, b)
	out, err := cmd.Output()
	if err == nil {
		return nil, nil // merges cleanly
	}
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) > 1 {
			return lines[1:], nil // the first line is the tree id
		}
		return nil, nil
	}
	return nil, fmt.Errorf("git merge-tree: %w", err)
}

// Branches analyses every branch against the tracked one and against each other, concurrently.
func Branches(repo *gitx.Repo, local []string) (BranchReport, error) {
	base := repo.Tracking()
	rep := BranchReport{Base: base, Branches: []BranchInfo{}, Alerts: []BranchAlert{}, Local: local}
	if rep.Local == nil {
		rep.Local = []string{}
	}
	out, err := repo.Run("for-each-ref", "--format=%(refname)%1f%(refname:short)%1f%(objectname)", "refs/heads", "refs/remotes")
	if err != nil {
		return rep, err
	}
	baseRef := repo.TrackedRef()
	baseSHA := repo.Head()
	var bs []BranchInfo
	seen := map[string]bool{}
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(l, "\x1f")
		if len(f) != 3 || f[0] == baseRef || strings.HasSuffix(f[0], "/HEAD") {
			continue
		}
		if seen[f[2]] {
			continue // the same commit under another name (a branch and its remote)
		}
		seen[f[2]] = true
		bs = append(bs, BranchInfo{Name: f[1], Remote: strings.HasPrefix(f[0], "refs/remotes/"), SHA: f[2], Authors: []string{}, Files: []string{}, Conflicts: []string{}})
	}
	parallel(len(bs), func(i int) {
		b := &bs[i]
		if c, err := repo.Run("rev-list", "--left-right", "--count", baseSHA+"..."+b.SHA); err == nil {
			if f := strings.Fields(c); len(f) == 2 {
				b.Behind, _ = strconv.Atoi(f[0])
				b.Ahead, _ = strconv.Atoi(f[1])
			}
		}
		b.Merged = b.Ahead == 0
		if s, err := repo.Run("log", "-1", "--format=%an%x1f%at%x1f%s", b.SHA); err == nil {
			if f := strings.SplitN(strings.TrimSpace(s), "\x1f", 3); len(f) == 3 {
				b.LastBy, b.LastMsg = f[0], f[2]
				at, _ := strconv.ParseInt(f[1], 10, 64)
				b.LastAt = at * 1000
			}
		}
		if b.Merged {
			return
		}
		if s, err := repo.Run("log", "--format=%an", baseSHA+".."+b.SHA); err == nil {
			who := map[string]bool{}
			for _, a := range strings.Split(strings.TrimSpace(s), "\n") {
				if a != "" && !who[a] {
					who[a] = true
					b.Authors = append(b.Authors, a)
				}
			}
			sort.Strings(b.Authors)
		}
		if s, err := repo.Run("diff", "--name-only", baseSHA+"..."+b.SHA); err == nil {
			b.Files = strings.Fields(s)
		}
		if c, err := mergeConflicts(repo.Root, baseSHA, b.SHA); err == nil && c != nil {
			b.Conflicts = c
		}
	})

	var mu sync.Mutex
	alert := func(a BranchAlert) {
		mu.Lock()
		rep.Alerts = append(rep.Alerts, a)
		mu.Unlock()
	}
	for _, b := range bs {
		if len(b.Conflicts) > 0 {
			alert(BranchAlert{Severity: "conflict", Title: fmt.Sprintf("%s conflicts with %s", b.Name, base), A: b.Name, B: base, People: b.Authors, Files: b.Conflicts,
				Detail: fmt.Sprintf("Merging it will conflict in %d file(s). %s should merge %s into it first.", len(b.Conflicts), strings.Join(b.Authors, ", "), base)})
		}
	}
	// Branch against branch: both may be fine with the base and still collide with each other.
	type pair struct{ a, b int }
	var pairs []pair
	active := []int{}
	for i := range bs {
		if !bs[i].Merged {
			active = append(active, i)
		}
	}
	for x := 0; x < len(active); x++ {
		for y := x + 1; y < len(active); y++ {
			pairs = append(pairs, pair{active[x], active[y]})
		}
	}
	parallel(len(pairs), func(i int) {
		a, b := &bs[pairs[i].a], &bs[pairs[i].b]
		shared := intersect(a.Files, b.Files)
		if len(shared) == 0 {
			return
		}
		people := union(a.Authors, b.Authors)
		if c, err := mergeConflicts(repo.Root, a.SHA, b.SHA); err == nil && len(c) > 0 {
			alert(BranchAlert{Severity: "conflict", Title: fmt.Sprintf("%s and %s conflict", a.Name, b.Name), A: a.Name, B: b.Name, People: people, Files: c,
				Detail: fmt.Sprintf("Whichever merges second will conflict in %d file(s). %s and %s should talk before merging.", len(c), strings.Join(a.Authors, ", "), strings.Join(b.Authors, ", "))})
			return
		}
		alert(BranchAlert{Severity: "overlap", Title: fmt.Sprintf("%s and %s change the same files", a.Name, b.Name), A: a.Name, B: b.Name, People: people, Files: shared,
			Detail: "They merge cleanly today, but both change this code, so check that the behaviour still fits together."})
	})
	// Your uncommitted work against everyone's branches.
	for _, b := range bs {
		if shared := intersect(rep.Local, b.Files); len(shared) > 0 && !b.Merged {
			alert(BranchAlert{Severity: "overlap", Title: "Your local changes touch files changed on " + b.Name, A: "local changes", B: b.Name, People: b.Authors, Files: shared,
				Detail: "Commit and merge soon, or talk to " + strings.Join(b.Authors, ", ") + " before this turns into a conflict."})
		}
	}
	sort.SliceStable(rep.Alerts, func(i, j int) bool {
		if rep.Alerts[i].Severity != rep.Alerts[j].Severity {
			return rep.Alerts[i].Severity == "conflict"
		}
		return rep.Alerts[i].Title < rep.Alerts[j].Title
	})
	sort.SliceStable(bs, func(i, j int) bool {
		if bs[i].Merged != bs[j].Merged {
			return !bs[i].Merged
		}
		return bs[i].LastAt > bs[j].LastAt
	})
	if bs != nil {
		rep.Branches = bs
	}
	return rep, nil
}

func intersect(a, b []string) []string {
	in := map[string]bool{}
	for _, x := range a {
		in[x] = true
	}
	out := []string{}
	for _, x := range b {
		if in[x] {
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}

func union(a, b []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, x := range append(append([]string{}, a...), b...) {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}
