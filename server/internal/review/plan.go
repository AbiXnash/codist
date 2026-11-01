package review

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"codist/internal/gitx"
	"codist/internal/javaidx"
)

// PlannedCommit is one suggested commit for local changes.
type PlannedCommit struct {
	Message string        `json:"message"`
	Why     string        `json:"why"`
	Files   []PlannedFile `json:"files"`
}

type PlannedFile struct {
	Path    string `json:"path"`
	Change  string `json:"change"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Issues  int    `json:"issues"`
}

// CommitPlan splits the working tree's changes into commits that each do one thing: files that
// reference each other stay together, tests stay with their class, and unrelated areas are apart.
func CommitPlan(g *javaidx.Graph, w Working) []PlannedCommit {
	changes := w.Changes
	if len(changes) == 0 {
		return []PlannedCommit{}
	}
	parent := make([]int, len(changes))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(a, b int) { parent[find(a)] = find(b) }
	index := map[string]int{}
	pathOf := func(c gitx.FileChange) string {
		if c.NewPath != "" {
			return c.NewPath
		}
		return c.Path
	}
	for i, c := range changes {
		index[pathOf(c)] = i
	}
	// Files whose classes reference each other.
	file := map[string]string{}
	if g != nil {
		for _, n := range g.Nodes {
			file[n.ID] = n.File
		}
		for _, e := range g.Edges {
			a, okA := index[file[e.Source]]
			b, okB := index[file[e.Target]]
			if okA && okB && a != b {
				union(a, b)
			}
		}
	}
	// A test goes with the class it is named after; build, config and docs group per area.
	bucket := map[string]int{}
	for i, c := range changes {
		p := pathOf(c)
		base := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		if strings.Contains(p, "src/test/") {
			subject := strings.TrimSuffix(strings.TrimSuffix(base, "Tests"), "Test")
			for q, j := range index {
				if q != p && strings.TrimSuffix(filepath.Base(q), ".java") == subject {
					union(i, j)
				}
			}
		}
		if !c.IsJava() {
			key := javaidx.ModuleOf(p) + "|" + areaOf(p)
			if j, ok := bucket[key]; ok {
				union(i, j)
			} else {
				bucket[key] = i
			}
		}
	}
	counts := map[string]int{}
	for _, fr := range w.Files {
		counts[fr.Path] = len(fr.Findings)
	}
	groups := map[int][]gitx.FileChange{}
	for i, c := range changes {
		groups[find(i)] = append(groups[find(i)], c)
	}
	roots := make([]int, 0, len(groups))
	for r := range groups {
		roots = append(roots, r)
	}
	sort.Ints(roots)
	out := make([]PlannedCommit, 0, len(groups))
	for _, r := range roots {
		cs := groups[r]
		pc := PlannedCommit{Files: []PlannedFile{}}
		for _, c := range cs {
			pc.Files = append(pc.Files, PlannedFile{Path: pathOf(c), Change: c.ChangeType, Added: len(c.AddedLines), Removed: len(c.DeletedAt), Issues: counts[pathOf(c)]})
		}
		sort.Slice(pc.Files, func(i, j int) bool { return pc.Files[i].Path < pc.Files[j].Path })
		pc.Message, pc.Why = planMessage(cs, pathOf)
		out = append(out, pc)
	}
	// Infrastructure first: what later commits may build on.
	sort.SliceStable(out, func(i, j int) bool { return planRank(out[i]) < planRank(out[j]) })
	return out
}

func areaOf(p string) string {
	base := filepath.Base(p)
	switch {
	case base == "pom.xml" || strings.HasPrefix(base, "build.gradle") || base == "settings.gradle" || base == "Makefile" || base == "Dockerfile":
		return "build"
	case strings.HasSuffix(p, ".md") || strings.Contains(p, "docs/"):
		return "docs"
	case strings.Contains(p, "src/main/resources") || strings.HasSuffix(p, ".yml") || strings.HasSuffix(p, ".yaml") || strings.HasSuffix(p, ".properties"):
		return "config"
	}
	return "other"
}

func planRank(c PlannedCommit) int {
	switch {
	case strings.HasPrefix(c.Message, "build"):
		return 0
	case strings.HasPrefix(c.Message, "docs"):
		return 3
	case strings.HasPrefix(c.Message, "test"):
		return 2
	}
	return 1
}

func planMessage(cs []gitx.FileChange, pathOf func(gitx.FileChange) string) (string, string) {
	allNew, allTests, allDocs, allBuild, allConfig := true, true, true, true, true
	scopes := map[string]int{}
	var names []string
	for _, c := range cs {
		p := pathOf(c)
		allNew = allNew && c.ChangeType == "ADD"
		allTests = allTests && strings.Contains(p, "src/test/")
		allDocs = allDocs && areaOf(p) == "docs"
		allBuild = allBuild && areaOf(p) == "build"
		allConfig = allConfig && areaOf(p) == "config"
		scope := javaidx.ModuleOf(p)
		if scope == "" {
			scope = filepath.Base(filepath.Dir(p))
		}
		if scope == "." {
			scope = "repo"
		}
		scopes[strings.TrimSuffix(scope, "-service")]++
		names = append(names, strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)))
	}
	sort.Strings(names)
	kind, verb := "refactor", "update"
	switch {
	case allDocs:
		kind, verb = "docs", "update"
	case allBuild:
		kind, verb = "build", "update"
	case allConfig:
		kind, verb = "chore", "update config in"
	case allTests:
		kind, verb = "test", "cover"
	case allNew:
		kind, verb = "feat", "add"
	}
	scope, best := "", 0
	for s, n := range scopes {
		if n > best || (n == best && s < scope) {
			scope, best = s, n
		}
	}
	what := strings.Join(names, ", ")
	if len(names) > 3 {
		what = fmt.Sprintf("%s and %d more", strings.Join(names[:2], ", "), len(names)-2)
	}
	why := "These files reference each other or belong to one area."
	if len(cs) == 1 {
		why = "Nothing else you changed depends on this file."
	}
	return fmt.Sprintf("%s(%s): %s %s", kind, scope, verb, what), why
}
