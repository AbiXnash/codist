package review

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"codist/internal/gitx"
	"codist/internal/javaidx"
	"codist/internal/knowledge"
)

// ReleaseReport is the state of the whole codebase at the release commit, for sign-off.
// It does not depend on which commits came before: every file is checked as it is.
type ReleaseReport struct {
	To            string    `json:"to"`
	ToLabel       string    `json:"toLabel"`
	Files         int       `json:"files"`       // Java files checked
	Lines         int       `json:"lines"`       // lines in those files
	Reviewed      int       `json:"reviewed"`    // of the main source files, how many the model has reviewed
	Sources       int       `json:"sources"`     // main source files
	NotReviewed   []string  `json:"notReviewed"` // main source files the model has never reviewed
	Modules       []Module  `json:"modules"`
	Open          []Issue   `json:"open"` // one per root cause, worst first
	Ignored       int       `json:"ignored"`
	BuildFindings []Finding `json:"buildFindings"`
	Risk          string    `json:"risk"`
	Reasons       []string  `json:"reasons"`
	Checklist     []string  `json:"checklist"`
}

type Module struct {
	Name   string `json:"name"`
	Files  int    `json:"files"`
	Issues int    `json:"issues"`
}

// Release checks every Java file at the commit with the rules and knowledge base, adds the model's
// tracked findings whose code is still there, and judges the risk from all of it.
func Release(repo *gitx.Repo, tracker *Tracker, store *Store, ignores *Ignores, kb *knowledge.Base, to, label string) ReleaseReport {
	rep := ReleaseReport{To: to, ToLabel: label, NotReviewed: []string{}, Modules: []Module{}, Open: []Issue{},
		BuildFindings: []Finding{}, Reasons: []string{}, Checklist: []string{}}
	files := repo.JavaFilesAt(to)
	g := javaidx.Index(files, to)
	ctx := javaidx.ProjectContext(&g)
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	r := Review{SHA: to}
	lines := map[string][]string{}
	for _, p := range paths {
		src := files[p]
		ls := strings.Split(string(src), "\n")
		lines[p] = ls
		rep.Files++
		rep.Lines += len(ls)
		fr := FileReview{Path: p, Findings: RuleScan(p, src, ctx, kb)}
		r.Files = append(r.Files, fr)
	}

	// The model's findings: rules cannot rediscover them, so keep the tracked ones whose code is still there.
	tracked := map[string]Issue{}
	for _, is := range tracker.Issues() {
		tracked[is.ID] = is
		if is.State == "ignored" || is.Source == "rule" {
			continue
		}
		ls, ok := lines[is.Path]
		if !ok {
			continue
		}
		line := lineOf(ls, is.Snippet)
		if line == 0 {
			continue
		}
		for k := range r.Files {
			if r.Files[k].Path == is.Path && !hasFinding(r.Files[k].Findings, line, is.Ref) {
				r.Files[k].Findings = append(r.Files[k].Findings, Finding{Line: line, EndLine: line, Severity: is.Severity, Category: is.Category,
					Title: is.Title, Detail: is.Detail, Suggestion: is.Suggestion, Source: is.Source, Ref: is.Ref, Snippet: is.Snippet})
			}
		}
	}

	r = AssignGroups(ignores.Decorate(r))
	perModule := map[string]*Module{}
	for _, p := range paths {
		m := moduleOf(p)
		if m == "" {
			continue
		}
		if perModule[m] == nil {
			perModule[m] = &Module{Name: m}
		}
		perModule[m].Files++
	}
	blamed := 0
	for _, fr := range r.Files {
		for _, f := range fr.Findings {
			if f.Ignored {
				rep.Ignored++
				continue
			}
			if f.Group != "" && !f.GroupRoot {
				continue
			}
			is := Issue{ID: Fingerprint(fr.Path, f), State: "open", Severity: f.Severity, Category: f.Category, Title: f.Title,
				Detail: f.Detail, Suggestion: f.Suggestion, Ref: f.Ref, Source: f.Source, Path: fr.Path, Line: f.Line,
				Snippet: f.Snippet, Group: f.Group, GroupRoot: f.GroupRoot, Commits: []string{}}
			if t, ok := tracked[is.ID]; ok {
				is.FirstSeen, is.FirstTime, is.FirstBy, is.FirstMsg = t.FirstSeen, t.FirstTime, t.FirstBy, t.FirstMsg
			} else if blamed < 100 { // untracked: whoever last wrote the line; capped, it is one git call each
				blamed++
				if c, ok := repo.BlameAt(to, fr.Path, f.Line); ok {
					is.FirstSeen, is.FirstTime, is.FirstBy, is.FirstMsg = c.SHA, c.Time, c.Author, c.Summary
				}
			}
			rep.Open = append(rep.Open, is)
			if m := perModule[moduleOf(fr.Path)]; m != nil {
				m.Issues++
			}
		}
	}
	sort.SliceStable(rep.Open, func(a, b int) bool { return sevRank(rep.Open[a].Severity) < sevRank(rep.Open[b].Severity) })
	for _, m := range perModule {
		rep.Modules = append(rep.Modules, *m)
	}
	sort.Slice(rep.Modules, func(i, j int) bool {
		a, b := rep.Modules[i], rep.Modules[j]
		if a.Issues != b.Issues {
			return a.Issues > b.Issues
		}
		if a.Files != b.Files {
			return a.Files > b.Files
		}
		return a.Name < b.Name
	})

	// Model coverage: which main source files has a finished review ever looked at.
	seen := map[string]bool{}
	for _, rv := range store.All() {
		if rv.Status != StatusDone {
			continue
		}
		for _, fr := range rv.Files {
			if fr.Error == "" && (fr.Skipped == "" || strings.Contains(fr.Skipped, "cache")) { // a trivial change is not a review
				seen[fr.Path] = true
			}
		}
	}
	for _, p := range paths {
		if !strings.Contains(p, "src/main/") {
			continue
		}
		rep.Sources++
		if seen[p] {
			rep.Reviewed++
		} else {
			rep.NotReviewed = append(rep.NotReviewed, p)
		}
	}

	if b, ok := buildAt(repo, to); ok {
		for _, p := range []string{"pom.xml", "build.gradle.kts", "build.gradle"} {
			if src, ok := repo.ReadFile(to, p); ok {
				rep.BuildFindings = append(rep.BuildFindings, BuildFindings(b, src, nil)...)
				break
			}
		}
	}

	// Risk and reasons.
	worst := "low"
	raise := func(level, reason string) {
		if riskRank[level] > riskRank[worst] {
			worst = level
		}
		rep.Reasons = append(rep.Reasons, reason)
	}
	sev := map[string]int{}
	for _, is := range rep.Open {
		sev[is.Severity]++
	}
	if n := sev["critical"] + sev["high"]; n > 0 {
		raise("high", fmt.Sprintf("%d open critical/high issue(s) in the code", n))
	}
	var buildBlockers []Finding
	for _, f := range rep.BuildFindings {
		if f.Severity == "critical" || f.Severity == "high" {
			buildBlockers = append(buildBlockers, f)
			raise("high", "build: "+f.Title)
		}
	}
	if sev["medium"] > 0 {
		raise("medium", fmt.Sprintf("%d open medium issue(s)", sev["medium"]))
	}
	if n := len(rep.NotReviewed); n > 0 && rep.Sources > 0 && n*2 > rep.Sources {
		raise("medium", fmt.Sprintf("only %d of %d source files reviewed by the model; the rest had rule checks only", rep.Reviewed, rep.Sources))
	}
	rep.Risk = worst

	// The checklist depends only on the code at the release commit, so the sign-off stays stable.
	if n := sev["critical"] + sev["high"]; n > 0 {
		rep.Checklist = append(rep.Checklist, fmt.Sprintf("Fix or explicitly accept the %d critical/high issue(s) before release", n))
	}
	for _, f := range buildBlockers {
		rep.Checklist = append(rep.Checklist, "Resolve the build issue: "+f.Title)
	}
	if len(rep.NotReviewed) > 0 {
		rep.Checklist = append(rep.Checklist, fmt.Sprintf("Spot-check the %d source file(s) the model has not reviewed", len(rep.NotReviewed)))
	}
	tested := 0
	for _, m := range rep.Modules {
		if tested == 3 || m.Issues == 0 || strings.HasPrefix(m.Name, "tests: ") || strings.Contains(m.Name, "/") || m.Name == "build" {
			continue
		}
		rep.Checklist = append(rep.Checklist, "Run the regression tests for "+m.Name)
		tested++
	}
	rep.Checklist = append(rep.Checklist, "Run the full test suite and smoke tests")
	return rep
}

var riskRank = map[string]int{"low": 0, "medium": 1, "high": 2}

func hasFinding(fs []Finding, line int, ref string) bool {
	for _, x := range fs {
		if x.Ref == ref && x.Line == line {
			return true
		}
	}
	return false
}

// lineOf finds the line holding the snippet, ignoring whitespace; 0 when the code is gone.
func lineOf(lines []string, snippet string) int {
	want := normalizeCode(snippet)
	if want == "" {
		return 0
	}
	for i, l := range lines {
		if normalizeCode(l) == want {
			return i + 1
		}
	}
	return 0
}

// moduleOf names the part of the system a file belongs to: its Java package, "tests: <package>",
// "build" for build files, or its folder. IDE and dot files are not part of any module.
func moduleOf(path string) string {
	base := filepath.Base(path)
	if strings.HasPrefix(path, ".") || strings.Contains(path, "/.") || base == ".classpath" || base == ".project" {
		return ""
	}
	switch base {
	case "pom.xml", "build.gradle", "build.gradle.kts", "settings.gradle", "mvnw", "mvnw.cmd":
		return "build"
	}
	if i := strings.Index(path, "src/main/java/"); i >= 0 {
		return strings.ReplaceAll(filepath.Dir(path[i+len("src/main/java/"):]), "/", ".")
	}
	if i := strings.Index(path, "src/test/java/"); i >= 0 {
		return "tests: " + strings.ReplaceAll(filepath.Dir(path[i+len("src/test/java/"):]), "/", ".")
	}
	if d := filepath.Dir(path); d != "." {
		return d
	}
	return path
}

func buildAt(repo *gitx.Repo, sha string) (Build, bool) {
	if sha == "" {
		return Build{}, false
	}
	for _, p := range []string{"pom.xml", "build.gradle.kts", "build.gradle"} {
		if src, ok := repo.ReadFile(sha, p); ok {
			return ParseBuild(p, src)
		}
	}
	return Build{}, false
}

// ReleaseMarkdown renders the report for sharing, with the sign-off state.
func ReleaseMarkdown(r ReleaseReport, so SignOff) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Release risk report\n\n_%s · whole codebase: %d files, %d lines · %s · risk: **%s**_\n\n",
		orDefault(r.ToLabel, shortSHA(r.To)), r.Files, r.Lines, time.Now().Format("2006-01-02"), r.Risk)
	list := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		b.WriteString("## " + title + "\n\n")
		for _, it := range items {
			b.WriteString("- " + it + "\n")
		}
		b.WriteString("\n")
	}
	list("Why", r.Reasons)
	var open []string
	for _, is := range r.Open {
		since := ""
		if is.FirstSeen != "" {
			since = fmt.Sprintf(", since %s by %s", shortSHA(is.FirstSeen), orDefault(is.FirstBy, "unknown"))
		}
		open = append(open, fmt.Sprintf("[%s] %s (%s:%d%s)", is.Severity, is.Title, filepath.Base(is.Path), is.Line, since))
	}
	list(fmt.Sprintf("Open issues (%d)", len(r.Open)), open)
	var build []string
	for _, f := range r.BuildFindings {
		build = append(build, fmt.Sprintf("[%s] %s", f.Severity, f.Title))
	}
	list("Build", build)
	var mods []string
	for _, m := range r.Modules {
		mods = append(mods, fmt.Sprintf("%s (%d files, %d issues)", m.Name, m.Files, m.Issues))
	}
	list("Modules", mods)
	fmt.Fprintf(&b, "Model review coverage: %d of %d source files.\n\n", r.Reviewed, r.Sources)
	fmt.Fprintf(&b, "## Sign-off checklist (%d / %d)\n\n", so.Done, so.Total)
	for _, it := range so.Items {
		if !it.Current {
			continue
		}
		mark := " "
		who := ""
		if it.Done {
			mark = "x"
			who = fmt.Sprintf(" — %s, %s", orDefault(it.By, "someone"), time.UnixMilli(it.At).Format("2006-01-02 15:04"))
		}
		fmt.Fprintf(&b, "- [%s] %s%s\n", mark, it.Text, who)
	}
	return b.String()
}

// DependencyReport is the current state of the build file.
type DependencyReport struct {
	Tool       string    `json:"tool"`
	Java       string    `json:"java"`
	SpringBoot string    `json:"springBoot"`
	Support    string    `json:"support"`
	Deps       []DepRow  `json:"deps"`
	Findings   []Finding `json:"findings"`
	File       string    `json:"file"`
}

type DepRow struct {
	Group    string `json:"group"`
	Artifact string `json:"artifact"`
	Version  string `json:"version"`
	Scope    string `json:"scope"`
	Managed  bool   `json:"managed"`
	Status   string `json:"status"` // ok, warn, bad
	Note     string `json:"note"`
}

// Dependencies checks the current build file (working tree, or the tracked head for mirrors).
func Dependencies(repo *gitx.Repo) DependencyReport {
	rep := DependencyReport{Deps: []DepRow{}, Findings: []Finding{}}
	var src, path string
	for _, p := range []string{"pom.xml", "build.gradle.kts", "build.gradle"} {
		var ok bool
		if repo.Remote {
			src, ok = repo.ReadFile(repo.Head(), p)
		} else {
			src, ok, _ = repo.ReadWorking(p)
		}
		if ok {
			path = p
			break
		}
	}
	if path == "" {
		return rep
	}
	b, ok := ParseBuild(path, src)
	if !ok {
		return rep
	}
	rep.File, rep.Tool, rep.Java, rep.SpringBoot = path, b.Tool, b.Java, b.SpringBoot
	switch m := major(b.SpringBoot); {
	case b.SpringBoot == "":
		rep.Support = ""
	case m == 2:
		rep.Support = "Spring Boot 2.x is end of life: no open-source security fixes since November 2023."
	case m == 3 && minor(b.SpringBoot) < 4:
		rep.Support = "Spring Boot " + b.SpringBoot + " is out of open-source support; move to the latest 3.x."
	default:
		rep.Support = "" // only known end-of-life lines are reported
	}
	rep.Findings = BuildFindings(b, src, nil)
	byArtifact := map[string]Finding{}
	for _, f := range rep.Findings {
		for _, d := range b.Deps {
			if strings.Contains(f.Title, d.Artifact) {
				byArtifact[d.Artifact] = f
			}
		}
	}
	for _, d := range b.Deps {
		row := DepRow{Group: d.Group, Artifact: d.Artifact, Version: d.Version, Scope: orDefault(d.Scope, "compile"),
			Managed: d.Version == "" && b.SpringBoot != "", Status: "ok"}
		if row.Managed {
			row.Version = "managed by Spring Boot " + b.SpringBoot
		}
		if f, ok := byArtifact[d.Artifact]; ok {
			row.Note = f.Title + ". " + f.Suggestion
			row.Status = "warn"
			if f.Severity == "critical" || f.Severity == "high" {
				row.Status = "bad"
			}
		}
		rep.Deps = append(rep.Deps, row)
	}
	sort.SliceStable(rep.Deps, func(i, j int) bool { return statusRank[rep.Deps[i].Status] > statusRank[rep.Deps[j].Status] })
	return rep
}

var statusRank = map[string]int{"ok": 0, "warn": 1, "bad": 2}

func minor(v string) int {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return 0
	}
	return atoi(parts[1])
}
