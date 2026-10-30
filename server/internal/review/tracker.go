package review

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"sync"

	"codist/internal/gitx"
	"codist/internal/javaidx"
	"codist/internal/knowledge"
)

// Issue is one problem tracked across commits: the same rule on the same line of code is the same
// issue, however many commits report it and wherever the line moves.
type Issue struct {
	ID         string `json:"id"`
	State      string `json:"state"` // open, resolved, ignored
	Severity   string `json:"severity"`
	Category   string `json:"category"`
	Title      string `json:"title"`
	Detail     string `json:"detail"`
	Suggestion string `json:"suggestion"`
	Ref        string `json:"ref,omitempty"`
	Source     string `json:"source"`
	Path       string `json:"path"`
	Line       int    `json:"line"` // where the code is now (open issues)
	Snippet    string `json:"snippet"`
	FirstSeen  string `json:"firstSeen"`
	FirstTime  int64  `json:"firstTime"`
	FirstBy    string `json:"firstBy"`  // author of the commit that introduced it
	FirstMsg   string `json:"firstMsg"` // its subject
	LastSeen   string `json:"lastSeen"`
	FixedIn    string `json:"fixedIn,omitempty"`
	FixedBy    string `json:"fixedBy,omitempty"`
	FixedAt    int64  `json:"fixedAt,omitempty"`
	FixedMsg   string `json:"fixedMsg,omitempty"`
	// Reopened: the issue was fixed, then the code came back (a revert, a bad merge, a copy).
	WasFixedIn  string `json:"wasFixedIn,omitempty"`
	ReopenedIn  string `json:"reopenedIn,omitempty"`
	ReopenedBy  string `json:"reopenedBy,omitempty"`
	ReopenedAt  int64  `json:"reopenedAt,omitempty"`
	ReopenedMsg string `json:"reopenedMsg,omitempty"`
	// Uncommitted: the code is gone from the working tree but still in the last commit.
	Uncommitted bool     `json:"uncommitted,omitempty"`
	Commits     []string `json:"commits"`
	IgnoreID    string   `json:"ignoreId,omitempty"`
	Group       string   `json:"group,omitempty"`
	GroupRoot   bool     `json:"groupRoot,omitempty"`
	ReviewLine  int      `json:"reviewLine"` // line in the LastSeen review, used to fix it
}

// Fingerprint identifies a finding independently of its line number.
func Fingerprint(path string, f Finding) string {
	key := f.Key()
	if f.Ref == "" {
		key = "llm:" + f.Category // model titles vary between runs; category + code is stable
	}
	h := sha1.Sum([]byte(path + "\x00" + key + "\x00" + normalizeCode(f.Snippet)))
	return hex.EncodeToString(h[:8])
}

func normalizeCode(s string) string { return strings.Join(strings.Fields(s), " ") }

// Tracker folds every review into issues.
type Tracker struct {
	repo    *gitx.Repo
	store   *Store
	ignores *Ignores

	// Rules re-runs the rule checks, so a rule issue whose cause was fixed elsewhere (its line
	// unchanged) is resolved. Nil disables the re-check.
	Rules RuleContext

	// Stamp adds to the cache key whatever else the issues depend on (the rules' version).
	Stamp func() string

	mu      sync.Mutex
	files   map[string]string // "sha:path" -> content, for resolution checks
	fixedAt map[string]string // issue id + head -> commit where the rule stopped firing

	// Issues are derived data: recomputed only when their inputs change.
	issuesMu  sync.Mutex
	issuesKey string
	issues    []Issue
}

// Version identifies the state the issues are computed from; it changes when they may change.
func (t *Tracker) Version() string { return t.key() }

// issuesKey identifies everything Issues depends on: the tracked head, the working tree, the
// reviews, the ignores and the rules.
func (t *Tracker) key() string {
	k := fmt.Sprintf("%s|%d|%s", t.repo.Head(), t.store.Version(), t.ignores.Version())
	if !t.repo.Remote {
		k += "|" + t.repo.Fingerprint() + "|" + t.repo.BuildFingerprint()
	}
	if t.Stamp != nil {
		k += "|" + t.Stamp()
	}
	return k
}

func NewTracker(repo *gitx.Repo, store *Store, ignores *Ignores) *Tracker {
	return &Tracker{repo: repo, store: store, ignores: ignores, files: map[string]string{}, fixedAt: map[string]string{}}
}

func (t *Tracker) fileAt(sha, path string) (string, bool) {
	key := sha + ":" + path
	t.mu.Lock()
	c, ok := t.files[key]
	t.mu.Unlock()
	if ok {
		return c, c != "\x00missing"
	}
	content, exists := t.repo.ReadFile(sha, path)
	if !exists {
		content = "\x00missing"
	}
	t.mu.Lock()
	t.files[key] = content
	t.mu.Unlock()
	return content, exists
}

func (t *Tracker) current(path string) (string, bool) {
	if t.repo.Remote {
		return t.fileAt(t.repo.Head(), path)
	}
	c, ok, err := t.repo.ReadWorking(path)
	return c, ok && err == nil
}

// Issues returns every tracked issue, newest activity first. The result is cached until something
// it depends on changes; concurrent callers share one computation.
func (t *Tracker) Issues() []Issue {
	t.issuesMu.Lock()
	defer t.issuesMu.Unlock()
	k := t.key()
	if t.issues == nil || k != t.issuesKey {
		t.issues, t.issuesKey = t.compute(), k
	}
	return append([]Issue(nil), t.issues...)
}

func (t *Tracker) compute() []Issue {
	commits := t.repo.Log(300)
	bySHA := map[string]gitx.Commit{}
	for _, c := range commits {
		bySHA[c.SHA] = c
	}
	byID := map[string]*Issue{}
	var order []string
	// A model finding on a pattern a rule owns stands or falls with that rule's findings in the
	// same review: its line (often a class declaration) is no evidence on its own.
	peers := map[string][]string{}
	for i := len(commits) - 1; i >= 0; i-- { // oldest first
		c := commits[i]
		r, ok := t.store.Get(c.SHA)
		if !ok || r.Status != StatusDone {
			continue
		}
		r = t.ignores.Decorate(r)
		ruleIDs := map[string][]string{}
		for _, fr := range r.Files {
			for _, f := range fr.Findings {
				if f.Source == "rule" && Recheckable(fr.Path, f) {
					ruleIDs[f.Ref] = append(ruleIDs[f.Ref], Fingerprint(fr.Path, f))
				}
			}
		}
		for _, fr := range r.Files {
			for _, f := range fr.Findings {
				if f.Source != "rule" {
					owned := f.Ref
					if len(ruleIDs[owned]) == 0 {
						owned = ruleCovered(f.Ref, f.Title)
					}
					if owned != "" && len(ruleIDs[owned]) > 0 {
						peers[Fingerprint(fr.Path, f)] = ruleIDs[owned]
					}
				}
			}
		}
		for _, fr := range r.Files {
			for _, f := range fr.Findings {
				id := Fingerprint(fr.Path, f)
				is, seen := byID[id]
				if !seen {
					is = &Issue{ID: id, FirstSeen: c.SHA, FirstTime: c.Time, FirstBy: c.Author, FirstMsg: c.Message, Path: fr.Path}
					byID[id] = is
					order = append(order, id)
				}
				is.LastSeen, is.ReviewLine = c.SHA, f.Line
				is.Commits = append(is.Commits, c.SHA)
				is.Severity, is.Category, is.Title, is.Detail, is.Suggestion = f.Severity, f.Category, f.Title, f.Detail, f.Suggestion
				is.Ref, is.Source, is.Snippet, is.Line = f.Ref, f.Source, f.Snippet, f.Line
				is.IgnoreID = ""
				if f.Ignored {
					is.IgnoreID = f.IgnoreID
				}
			}
		}
	}

	recheck := t.recheck()
	out := make([]Issue, 0, len(order))
	for _, id := range order {
		is := byID[id]
		switch {
		case is.IgnoreID != "":
			is.State = "ignored"
		default:
			if line := t.locate(is); line > 0 && recheck(is, line) {
				is.State, is.Line = "open", line
				if fixed, back := t.reopened(is); back != "" {
					is.WasFixedIn, is.ReopenedIn = fixed, back
					if c, ok := bySHA[back]; ok {
						is.ReopenedBy, is.ReopenedAt, is.ReopenedMsg = c.Author, c.Time, c.Message
					}
				}
			} else if line > 0 {
				// The code is still there but the rule no longer flags it: fixed by a change elsewhere.
				is.State, is.Line = "resolved", 0
				is.FixedIn = t.ruleFixedIn(is)
				if c, ok := bySHA[is.FixedIn]; ok {
					is.FixedBy, is.FixedAt, is.FixedMsg = c.Author, c.Time, c.Message
				} else if is.FixedIn == "" && !t.repo.Remote {
					is.Uncommitted = true
				}
			} else {
				is.State, is.Line = "resolved", 0
				is.FixedIn = t.fixedIn(is)
				if c, ok := bySHA[is.FixedIn]; ok {
					is.FixedBy, is.FixedAt, is.FixedMsg = c.Author, c.Time, c.Message
				} else if is.FixedIn == "" && !t.repo.Remote {
					// Gone from the working tree but not from the last commit: fixed locally.
					if head, ok := t.fileAt(t.repo.Head(), is.Path); ok && containsLine(head, normalizeCode(is.Snippet)) {
						is.Uncommitted = true
					}
				}
			}
		}
		out = append(out, *is)
	}
	for i := range out {
		ps := peers[out[i].ID]
		if out[i].State != "open" || len(ps) == 0 {
			continue
		}
		var last *Issue
		for _, id := range ps {
			p := byID[id]
			if p == nil || p.State != "resolved" {
				last = nil
				break
			}
			if last == nil || p.FixedAt > last.FixedAt {
				last = p
			}
		}
		if last != nil {
			out[i].State, out[i].Line = "resolved", 0
			out[i].FixedIn, out[i].FixedBy, out[i].FixedAt, out[i].FixedMsg, out[i].Uncommitted = last.FixedIn, last.FixedBy, last.FixedAt, last.FixedMsg, last.Uncommitted
		}
	}

	// Group the open issues the same way a single review is grouped.
	var r Review
	idx := map[string][]int{}
	for i, is := range out {
		if is.State != "open" {
			continue
		}
		pos := -1
		for k, fr := range r.Files {
			if fr.Path == is.Path {
				pos = k
			}
		}
		if pos < 0 {
			r.Files = append(r.Files, FileReview{Path: is.Path})
			pos = len(r.Files) - 1
		}
		r.Files[pos].Findings = append(r.Files[pos].Findings, Finding{Line: is.Line, Severity: is.Severity, Category: is.Category,
			Title: is.Title, Detail: is.Detail, Ref: is.Ref, Snippet: is.Snippet})
		idx[is.Path] = append(idx[is.Path], i)
	}
	g := AssignGroups(r)
	for _, fr := range g.Files {
		for k, f := range fr.Findings {
			i := idx[fr.Path][k]
			out[i].Group, out[i].GroupRoot = f.Group, f.GroupRoot
		}
	}

	sort.SliceStable(out, func(a, b int) bool {
		sa, sb := stateRank[out[a].State], stateRank[out[b].State]
		if sa != sb {
			return sa < sb
		}
		if ra, rb := sevRank(out[a].Severity), sevRank(out[b].Severity); ra != rb {
			return ra < rb
		}
		return out[a].FirstTime > out[b].FirstTime
	})
	return out
}

var stateRank = map[string]int{"open": 0, "ignored": 1, "resolved": 2}

func sevRank(s string) int {
	for i, x := range severities {
		if x == s {
			return i
		}
	}
	return len(severities)
}

// locate returns the issue's current line, or 0 when its code is gone.
func (t *Tracker) locate(is *Issue) int {
	src, ok := t.current(is.Path)
	if !ok {
		return 0
	}
	want := normalizeCode(is.Snippet)
	if want == "" {
		return is.Line
	}
	best, dist := 0, 1<<30
	for i, l := range strings.Split(src, "\n") {
		if normalizeCode(l) == want {
			if d := abs(i + 1 - is.Line); d < dist {
				best, dist = i+1, d
			}
		}
	}
	return best
}

// recheck returns a check that a rule issue still fires at line in the current code. Each file is
// scanned at most once per call.
func (t *Tracker) recheck() func(is *Issue, line int) bool {
	if t.Rules == nil {
		return func(*Issue, int) bool { return true }
	}
	var ctx javaidx.Context
	var kb *knowledge.Base
	loaded := false
	scans := map[string][]Finding{}
	return func(is *Issue, line int) bool {
		f := Finding{Source: is.Source, Ref: is.Ref}
		if !Recheckable(is.Path, f) {
			return true
		}
		scan, ok := scans[is.Path]
		if !ok {
			src, exists := t.current(is.Path)
			if !exists {
				return true
			}
			if !loaded {
				var ready bool
				if ctx, kb, ready = t.Rules(); !ready {
					t.Rules = nil // not indexed yet: treat as still open
				}
				loaded = true
			}
			if t.Rules == nil {
				return true
			}
			scan = RuleScan(is.Path, []byte(src), ctx, kb)
			scans[is.Path] = scan
		}
		return fires(scan, is.Ref, line)
	}
}

// ruleFixedIn finds the first commit after the issue was last seen where the rule stops firing,
// checking each commit with the whole project as it was then. "" means only the working tree fixes it.
func (t *Tracker) ruleFixedIn(is *Issue) string {
	head := t.repo.Head()
	key := is.ID + "@" + head
	t.mu.Lock()
	sha, ok := t.fixedAt[key]
	t.mu.Unlock()
	if ok {
		return sha
	}
	_, kb, _ := t.Rules()
	for _, c := range t.repo.CommitsSince(is.LastSeen, head, 20) {
		src, exists := t.fileAt(c.SHA, is.Path)
		if !exists {
			continue
		}
		line := lineOf(strings.Split(src, "\n"), is.Snippet)
		if line == 0 {
			sha = c.SHA
			break
		}
		g := javaidx.Index(t.repo.JavaFilesAt(c.SHA), c.SHA)
		if !fires(RuleScan(is.Path, []byte(src), javaidx.ProjectContext(&g), kb), is.Ref, line) {
			sha = c.SHA
			break
		}
	}
	t.mu.Lock()
	t.fixedAt[key] = sha
	t.mu.Unlock()
	return sha
}

// reopened finds an open issue's last comeback: the commit that removed its code after it was
// introduced, and the later commit that brought it back. Both are "" when it never went away.
func (t *Tracker) reopened(is *Issue) (fixed, back string) {
	touching := t.repo.CommitsTouching(is.Path) // newest first
	want := normalizeCode(is.Snippet)
	present := false
	for i := len(touching) - 1; i >= 0; i-- {
		sha := touching[i]
		if sha == is.FirstSeen {
			present = true
			continue
		}
		if !present && fixed == "" {
			continue // before the issue was first seen
		}
		src, ok := t.fileAt(sha, is.Path)
		has := ok && containsLine(src, want)
		switch {
		case present && !has:
			fixed, back = sha, ""
		case !present && has:
			back = sha
		}
		present = has
	}
	if back == "" {
		fixed = ""
	}
	return fixed, back
}

// fixedIn finds the commit that removed the flagged code for good: walking the commits that
// touched the file from newest to oldest, it is the one just after the newest commit that still
// contains the code. Commits in between that kept the code (but changed other lines) do not count.
func (t *Tracker) fixedIn(is *Issue) string {
	touching := t.repo.CommitsTouching(is.Path) // newest first
	want := normalizeCode(is.Snippet)
	newer := ""
	for _, sha := range touching {
		src, ok := t.fileAt(sha, is.Path)
		if (ok && containsLine(src, want)) || sha == is.LastSeen {
			return newer
		}
		newer = sha
	}
	return ""
}

func containsLine(src, want string) bool {
	for _, l := range strings.Split(src, "\n") {
		if normalizeCode(l) == want {
			return true
		}
	}
	return false
}

// Annotate marks each finding of a review with its first commit and current state, and records
// which commits fixed issues that this review still lists.
func (t *Tracker) Annotate(r Review, issues []Issue) Review {
	byID := map[string]Issue{}
	for _, is := range issues {
		byID[is.ID] = is
	}
	for i := range r.Files {
		for j := range r.Files[i].Findings {
			f := &r.Files[i].Findings[j]
			if is, ok := byID[Fingerprint(r.Files[i].Path, *f)]; ok {
				f.FirstSeen, f.State, f.FixedIn = is.FirstSeen, is.State, is.FixedIn
				f.FixedBy, f.FixedAt, f.FixedMsg, f.Uncommitted = is.FixedBy, is.FixedAt, is.FixedMsg, is.Uncommitted
			}
		}
	}
	return r
}

// FixedBy counts the issues each commit resolved.
func FixedBy(issues []Issue) map[string]int {
	out := map[string]int{}
	for _, is := range issues {
		if is.FixedIn != "" && (is.Group == "" || is.GroupRoot || is.State == "resolved") {
			out[is.FixedIn]++
		}
	}
	return out
}

// FixedIn returns the issues a commit resolved, one per root cause.
func FixedInCommit(issues []Issue, sha string) []Issue {
	out := []Issue{}
	for _, is := range issues {
		if is.FixedIn == sha {
			out = append(out, is)
		}
	}
	return out
}
