package review

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"codist/internal/gitx"
	"codist/prompts"
)

// A profile describes work, and a little evidence says little. Below these, it says so and draws no
// conclusions about quality.
const (
	minReviewedCommits = 5
	minReviewedLines   = 200
	bigCommitLines     = 400
)

// Share is one line of a breakdown: lines in a skill, or in an area of the code.
type Share struct {
	Name    string `json:"name"`
	Lines   int    `json:"lines"`
	Commits int    `json:"commits"`
}

// Count is a named number.
type Count struct {
	Name string `json:"name"`
	N    int    `json:"n"`
}

// Point is something a developer does well or could improve, always with the numbers behind it.
type Point struct {
	Title  string   `json:"title"`
	Detail string   `json:"detail"`
	Advice string   `json:"advice,omitempty"`
	Issues []string `json:"issues,omitempty"` // examples, by issue id
}

// DevProfile is what git and the reviews show about one person's work on this branch.
type DevProfile struct {
	Name           string  `json:"name"`
	Commits        int     `json:"commits"`
	Reviewed       int     `json:"reviewed"`
	Added          int     `json:"added"`
	Removed        int     `json:"removed"`
	Owned          int     `json:"owned"`
	Files          int     `json:"files"`
	TestShare      int     `json:"testShare"`    // percent of the lines they added
	MedianCommit   int     `json:"medianCommit"` // lines changed
	BigCommits     int     `json:"bigCommits"`   // over bigCommitLines
	BadMessages    int     `json:"badMessages"`  // of reviewed commits
	Skills         []Share `json:"skills"`       // what kind of code they work on
	Areas          []Share `json:"areas"`        // where in the code
	Introduced     int     `json:"introduced"`   // issues from their commits, ignored ones excluded
	OpenNow        int     `json:"openNow"`      // of those, still open
	Serious        int     `json:"serious"`      // critical, high or medium
	Severity       []Count `json:"severity"`     // open and closed, worst first
	Kinds          []Count `json:"kinds"`        // Bug, Security and so on
	Rate           float64 `json:"rate"`         // serious issues per 1000 reviewed lines; 0 when not enough evidence
	Fixed          int     `json:"fixed"`        // issues they closed
	FixedOwn       int     `json:"fixedOwn"`     // from their own commits
	FixedOthers    int     `json:"fixedOthers"`  // from other people's
	AvgFixDays     float64 `json:"avgFixDays"`   // issues they fixed, from report to fix
	EnoughEvidence bool    `json:"enoughEvidence"`
	Strengths      []Point `json:"strengths"`
	Improve        []Point `json:"improve"`
}

// TeamReport is everyone on the followed branch, by commits: an order of activity, not of merit.
type TeamReport struct {
	People   []DevProfile `json:"people"`
	TeamRate float64      `json:"teamRate"`
}

// reviewStore is the part of Store a profile needs.
type reviewStore interface {
	Get(sha string) (Review, bool)
}

// skillOf says what kind of code a file is.
func skillOf(path, stereotype string) string {
	base := filepath.Base(path)
	if strings.Contains(path, "/src/test/") || strings.HasPrefix(path, "src/test/") || strings.HasSuffix(base, "Test.java") || strings.HasSuffix(base, "Tests.java") {
		return "Testing"
	}
	switch stereotype {
	case "controller":
		return "REST APIs"
	case "service":
		return "Business logic"
	case "repository":
		return "Data access"
	case "entity":
		return "Data modelling"
	case "client":
		return "Service-to-service calls"
	case "config":
		return "Spring configuration"
	case "advice", "exception":
		return "Error handling"
	case "dto":
		return "API contracts"
	}
	for suffix, skill := range map[string]string{"Controller.java": "REST APIs", "Service.java": "Business logic", "Repository.java": "Data access", "Client.java": "Service-to-service calls", "Config.java": "Spring configuration", "Exception.java": "Error handling", "Handler.java": "Error handling", "Dto.java": "API contracts"} {
		if strings.HasSuffix(base, suffix) {
			return skill
		}
	}
	switch filepath.Ext(base) {
	case ".java":
		return "Java"
	case ".yml", ".yaml", ".properties":
		return "Configuration"
	case ".sql":
		return "SQL"
	}
	if base == "pom.xml" || strings.HasPrefix(base, "build.gradle") {
		return "Build and dependencies"
	}
	return ""
}

// workArea says where in the code a file is: its service, or its package in a single-module repository.
func workArea(path string) string {
	if i := strings.Index(path, "/src/"); i > 0 {
		return path[:i]
	}
	if strings.Contains(path, "src/") {
		return filepath.Base(filepath.Dir(path))
	}
	return "(project root)"
}

func sortedShares(lines map[string]int, commits map[string]map[string]bool) []Share {
	out := make([]Share, 0, len(lines))
	for name, n := range lines {
		out = append(out, Share{Name: name, Lines: n, Commits: len(commits[name])})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Lines != out[j].Lines {
			return out[i].Lines > out[j].Lines
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func sortedCounts(m map[string]int) []Count {
	out := make([]Count, 0, len(m))
	for name, n := range m {
		out = append(out, Count{name, n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].N != out[j].N {
			return out[i].N > out[j].N
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// Team builds everyone's profile from their commits and the issues tracked on this branch.
func Team(commits []gitx.AuthoredCommit, issues []Issue, store reviewStore, stereotypes map[string]string, people *PeopleReport) TeamReport {
	type work struct {
		p                         *DevProfile
		sizes                     []int
		files                     map[string]bool
		skills, areas             map[string]int
		skillCommits, areaCommits map[string]map[string]bool
		testLines, reviewedLines  int
		fixDays                   []float64
		mine                      []Issue
		fixes                     []Issue
	}
	all := map[string]*work{}
	at := func(name string) *work {
		if all[name] == nil {
			all[name] = &work{p: &DevProfile{Name: name}, files: map[string]bool{}, skills: map[string]int{}, areas: map[string]int{},
				skillCommits: map[string]map[string]bool{}, areaCommits: map[string]map[string]bool{}}
		}
		return all[name]
	}
	note := func(m map[string]map[string]bool, key, sha string) {
		if m[key] == nil {
			m[key] = map[string]bool{}
		}
		m[key][sha] = true
	}
	for _, c := range commits {
		w := at(c.Author)
		w.p.Commits++
		size, added := 0, 0
		for _, f := range c.Files {
			skill := skillOf(f.Path, stereotypes[f.Path])
			if skill == "" {
				continue // documents, images and the like say nothing about code
			}
			size += f.Added + f.Removed
			added += f.Added
			w.p.Added += f.Added
			w.p.Removed += f.Removed
			w.files[f.Path] = true
			w.skills[skill] += f.Added
			note(w.skillCommits, skill, c.SHA)
			if skill == "Testing" {
				w.testLines += f.Added
			}
			area := workArea(f.Path)
			w.areas[area] += f.Added
			note(w.areaCommits, area, c.SHA)
		}
		w.sizes = append(w.sizes, size)
		if size > bigCommitLines {
			w.p.BigCommits++
		}
		if rv, ok := store.Get(c.SHA); ok && rv.Status == StatusDone {
			w.p.Reviewed++
			w.reviewedLines += added
			if rv.MessageProblem != "" {
				w.p.BadMessages++
			}
		}
	}
	for _, is := range issues {
		if is.Group != "" && !is.GroupRoot && is.State == "open" {
			continue // one root cause, counted once
		}
		if is.State != "ignored" && is.FirstBy != "" {
			w := at(is.FirstBy)
			w.mine = append(w.mine, is)
		}
		if is.State == "resolved" && is.FixedBy != "" {
			w := at(is.FixedBy)
			w.fixes = append(w.fixes, is)
		}
	}
	teamSerious, teamLines := 0, 0
	for _, w := range all {
		p := w.p
		severities, kinds := map[string]int{}, map[string]int{}
		for _, is := range w.mine {
			p.Introduced++
			severities[is.Severity]++
			if is.Kind != "" {
				kinds[is.Kind]++
			}
			if is.State == "open" {
				p.OpenNow++
			}
			if is.Severity == "critical" || is.Severity == "high" || is.Severity == "medium" {
				p.Serious++
			}
		}
		for _, level := range []string{"critical", "high", "medium", "low", "info"} {
			if severities[level] > 0 {
				p.Severity = append(p.Severity, Count{level, severities[level]})
			}
		}
		p.Kinds = sortedCounts(kinds)
		for _, is := range w.fixes {
			p.Fixed++
			if is.FirstBy == p.Name {
				p.FixedOwn++
			} else {
				p.FixedOthers++
			}
			if is.FixedAt > is.FirstTime && is.FirstTime > 0 && !is.Manual {
				w.fixDays = append(w.fixDays, float64(is.FixedAt-is.FirstTime)/float64(24*time.Hour.Milliseconds()))
			}
		}
		if len(w.fixDays) > 0 {
			total := 0.0
			for _, d := range w.fixDays {
				total += d
			}
			p.AvgFixDays = total / float64(len(w.fixDays))
		}
		p.Files = len(w.files)
		if p.Added > 0 {
			p.TestShare = w.testLines * 100 / p.Added
		}
		sort.Ints(w.sizes)
		if len(w.sizes) > 0 {
			p.MedianCommit = w.sizes[len(w.sizes)/2]
		}
		p.Skills = sortedShares(w.skills, w.skillCommits)
		p.Areas = sortedShares(w.areas, w.areaCommits)
		p.EnoughEvidence = p.Reviewed >= minReviewedCommits && w.reviewedLines >= minReviewedLines
		if p.EnoughEvidence {
			p.Rate = float64(p.Serious) * 1000 / float64(w.reviewedLines)
			teamSerious += p.Serious
			teamLines += w.reviewedLines
		}
		if people != nil {
			for _, o := range people.People {
				if o.Name == p.Name {
					p.Owned = o.Owned
				}
			}
		}
	}
	report := TeamReport{People: []DevProfile{}}
	if teamLines > 0 {
		report.TeamRate = float64(teamSerious) * 1000 / float64(teamLines)
	}
	comparable := 0
	for _, w := range all {
		if w.p.EnoughEvidence {
			comparable++
		}
	}
	for _, w := range all {
		rate := 0.0
		if comparable >= 2 {
			rate = report.TeamRate
		}
		w.p.Strengths, w.p.Improve = judge(w.p, w.mine, rate)
		report.People = append(report.People, *w.p)
	}
	sort.Slice(report.People, func(i, j int) bool {
		a, b := report.People[i], report.People[j]
		if a.Commits != b.Commits {
			return a.Commits > b.Commits
		}
		return a.Name < b.Name
	})
	return report
}

var adviceSentence = regexp.MustCompile(`^[^\n]*?[.!?](\s|$)`)

func shortAdvice(s string) string {
	s = strings.TrimSpace(s)
	if m := adviceSentence.FindString(s); m != "" {
		s = strings.TrimSpace(m)
	}
	if i := strings.Index(s, "\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 220 {
		s = s[:219] + "…"
	}
	return s
}

// judge turns the numbers into points. Each needs a clear signal and quotes its own evidence.
// teamRate is 0 when there is nobody fair to compare with.
func judge(p *DevProfile, mine []Issue, teamRate float64) (strengths, improve []Point) {
	strengths, improve = []Point{}, []Point{}
	good := func(title, detail string) { strengths = append(strengths, Point{Title: title, Detail: detail}) }
	bad := func(title, detail, advice string, issues ...string) {
		improve = append(improve, Point{Title: title, Detail: detail, Advice: advice, Issues: issues})
	}

	// True whatever the amount of evidence: these only describe what they did.
	if p.FixedOthers >= 1 {
		good("Fixes other people's bugs", fmt.Sprintf("Closed %s from colleagues' commits.", plural(p.FixedOthers, "issue")))
	}
	if p.FixedOwn >= 3 {
		good("Cleans up after their own changes", fmt.Sprintf("Closed %s from their own commits.", plural(p.FixedOwn, "issue")))
	}
	if len(p.Areas) >= 3 {
		names := []string{}
		for _, a := range p.Areas[:3] {
			names = append(names, a.Name)
		}
		good("Works across the code", fmt.Sprintf("Changed %d areas, most in %s.", len(p.Areas), strings.Join(names, ", ")))
	}
	if p.Added >= minReviewedLines && p.TestShare >= 20 {
		good("Writes tests", fmt.Sprintf("%d%% of the lines they added are tests.", p.TestShare))
	}
	var urgent []string
	for _, is := range mine {
		if is.State == "open" && (is.Severity == "critical" || is.Severity == "high") {
			urgent = append(urgent, is.ID)
		}
	}
	if len(urgent) > 0 {
		bad("Urgent issues still open", fmt.Sprintf("%s rated critical or high came from their commits and %s still open.", plural(len(urgent), "issue"), map[bool]string{true: "is", false: "are"}[len(urgent) == 1]),
			"Start with these before new work.", urgent[:min(len(urgent), 5)]...)
	}

	if !p.EnoughEvidence {
		return strengths, improve
	}

	// The rest are about quality and habits, so they need enough reviewed work to mean something.
	if p.TestShare < 5 && p.Added >= minReviewedLines {
		bad("Little test code", fmt.Sprintf("%d%% of the lines they added are tests.", p.TestShare), "Add one test with the next change, for the behaviour that change relies on.")
	}
	if p.MedianCommit > 0 && p.MedianCommit <= 150 && p.Commits >= minReviewedCommits {
		good("Keeps commits small", fmt.Sprintf("A typical commit changes %d lines, which is easy to review.", p.MedianCommit))
	}
	if p.BigCommits*100 >= p.Commits*30 {
		bad("Large commits", fmt.Sprintf("%d of %s changed more than %d lines.", p.BigCommits, plural(p.Commits, "commit"), bigCommitLines), "Split work so each commit does one thing; Uncommitted changes in Review suggests how.")
	}
	if p.BadMessages*100 >= p.Reviewed*30 {
		bad("Vague commit messages", fmt.Sprintf("%d of %d reviewed commits have a message that does not say what changed.", p.BadMessages, p.Reviewed), "Say what changed and why. The Commit tab can suggest a message.")
	} else if p.BadMessages*10 <= p.Reviewed {
		good("Clear commit messages", fmt.Sprintf("%d of %d reviewed commits needed a better message.", p.BadMessages, p.Reviewed))
	}
	if p.Reviewed > 0 && teamRate > 0 {
		switch {
		case p.Rate <= teamRate*0.6:
			good("Fewer serious issues than average", fmt.Sprintf("%.1f per 1,000 reviewed lines, against %.1f for the team.", p.Rate, teamRate))
		case p.Rate >= teamRate*1.5 && p.Serious >= 3:
			bad("More serious issues than average", fmt.Sprintf("%.1f per 1,000 reviewed lines, against %.1f for the team.", p.Rate, teamRate), "This can also mean they work on the riskiest code. Pair on a review of the next change there.")
		}
	}

	// The same problem in different commits is a habit, and the most useful thing to point out.
	type repeat struct {
		title, suggestion string
		commits           map[string]bool
		ids               []string
	}
	repeats := map[string]*repeat{}
	for _, is := range mine {
		if is.Severity == "info" {
			continue
		}
		key := is.Ref
		if key == "" {
			key = is.Title
		}
		r := repeats[key]
		if r == nil {
			r = &repeat{title: is.Title, suggestion: is.Suggestion, commits: map[string]bool{}}
			repeats[key] = r
		}
		r.commits[is.FirstSeen] = true
		r.ids = append(r.ids, is.ID)
	}
	var habits []*repeat
	for _, r := range repeats {
		if len(r.commits) >= 2 {
			habits = append(habits, r)
		}
	}
	sort.Slice(habits, func(i, j int) bool {
		if len(habits[i].commits) != len(habits[j].commits) {
			return len(habits[i].commits) > len(habits[j].commits)
		}
		return habits[i].title < habits[j].title
	})
	for _, r := range habits[:min(len(habits), 3)] {
		advice := shortAdvice(r.suggestion)
		if advice == "" {
			advice = "Look for this before committing."
		}
		bad("Repeats: "+r.title, fmt.Sprintf("Found in %d different commits.", len(r.commits)), advice, r.ids[:min(len(r.ids), 5)]...)
	}
	return strengths, improve
}

// DevAdviceText is the model's write-up of a profile.
type DevAdviceText struct {
	Summary   string   `json:"summary"`
	Strengths []string `json:"strengths"`
	Improve   []string `json:"improve"`
	Advice    []string `json:"advice"`
}

// DevAdvice asks the model to write up a profile, from the measured facts only.
func (x *Fixer) DevAdvice(ctx context.Context, p DevProfile) (DevAdviceText, error) {
	// Plain sentences: given nested JSON, a small model tends to echo it back rather than write.
	var facts strings.Builder
	fmt.Fprintf(&facts, "Name: %s\nenoughEvidence: %v\nCommits: %d, of which %d reviewed\nLines added: %d, removed: %d, in the code today: %d\nShare of added lines that are tests: %d%%\nTypical commit: %d lines; %d commits over %d lines\n",
		p.Name, p.EnoughEvidence, p.Commits, p.Reviewed, p.Added, p.Removed, p.Owned, p.TestShare, p.MedianCommit, p.BigCommits, bigCommitLines)
	fmt.Fprintf(&facts, "Issues from their commits: %d, %d still open, %d critical, high or medium\nIssues they fixed: %d from their own commits, %d from colleagues'\n", p.Introduced, p.OpenNow, p.Serious, p.FixedOwn, p.FixedOthers)
	for _, sk := range p.Skills[:min(len(p.Skills), 6)] {
		fmt.Fprintf(&facts, "Works on %s: %d lines in %d commits\n", sk.Name, sk.Lines, sk.Commits)
	}
	for _, k := range p.Kinds {
		fmt.Fprintf(&facts, "Issues of type %s: %d\n", k.Name, k.N)
	}
	for _, g := range p.Strengths {
		fmt.Fprintf(&facts, "Measured strength: %s. %s\n", g.Title, g.Detail)
	}
	for _, g := range p.Improve {
		fmt.Fprintf(&facts, "Measured problem: %s. %s %s\n", g.Title, g.Detail, g.Advice)
	}
	llm := x.ask
	if llm == nil {
		llm = x.llm
	}
	out, err := llm.With(ctx).For(Label{Kind: "dev-advice", Title: "Summary of " + p.Name + "'s work"}).ChatJSON(prompts.Get("dev-advice"), "## The facts\n"+facts.String())
	if err != nil {
		return DevAdviceText{}, err
	}
	list := func(key string) []string {
		items := []string{}
		raw, _ := out[key].([]any)
		for _, v := range raw {
			if m, ok := v.(map[string]any); ok { // some models answer with objects despite the instructions
				v = strings.TrimSpace(str(m["title"]) + ": " + str(m["detail"]))
			}
			if s := strings.TrimSpace(str(v)); s != "" {
				items = append(items, s)
			}
		}
		return items
	}
	text := DevAdviceText{Summary: strings.TrimSpace(str(out["summary"])), Strengths: list("strengths"), Improve: list("improve"), Advice: list("advice")}
	if text.Summary == "" {
		return text, errors.New("the model returned no summary; try again")
	}
	return text, nil
}
