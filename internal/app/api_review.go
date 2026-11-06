package app

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"codist/internal/git"
	"codist/internal/java"
	"codist/internal/review"
	"codist/internal/store"
	"codist/internal/web"
)

type commitJSON struct {
	SHA         string   `json:"sha"`
	ShortSHA    string   `json:"shortSha"`
	Message     string   `json:"message"`
	FullMessage string   `json:"fullMessage"`
	Author      string   `json:"author"`
	Time        int64    `json:"time"`
	Parents     []string `json:"parents"`
}

func commitOf(c git.Commit) commitJSON {
	full := c.Subject
	if c.Body != "" {
		full += "\n\n" + c.Body
	}
	parents := c.Parents
	if parents == nil {
		parents = []string{}
	}
	return commitJSON{SHA: c.SHA, ShortSHA: c.Short, Message: c.Subject, FullMessage: full, Author: c.Author, Time: ms(c.Time), Parents: parents}
}

// commitCache keeps commits already read, since issues refer to the same few commits again and again.
var commitCache sync.Map

func (a *App) commitInfo(sha string) (git.Commit, bool) {
	if sha == "" || sha == "manual" {
		return git.Commit{}, false
	}
	if c, ok := commitCache.Load(sha); ok {
		return c.(git.Commit), true
	}
	c, err := a.repo.Get(sha)
	if err != nil {
		return git.Commit{}, false
	}
	commitCache.Store(sha, c)
	return c, true
}

// reviewStatus is a commit's review state as the interface knows it.
func (a *App) reviewStatus(sha string, reviews map[string]store.Review) string {
	rv, ok := reviews[sha]
	if ok && rv.Status == "running" || a.models.running(sha) {
		return "running"
	}
	if a.models.waiting(sha) || a.checks.waiting(sha) {
		return "queued"
	}
	if ok {
		return rv.Status
	}
	return ""
}

func (a *App) commits(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 2000 {
		limit = 300
	}
	log, err := a.repo.LogRef(a.viewRef(), limit)
	if err != nil {
		web.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	reviews, counts := a.store.Reviews(), a.store.Counts()
	fixed := map[string]int{}
	for _, is := range a.store.AllIssues() {
		if is.State == "resolved" && is.FixedSHA != "" {
			fixed[is.FixedSHA]++
		}
	}
	out := make([]map[string]any, 0, len(log))
	for _, c := range log {
		item := map[string]any{"commit": commitOf(c)}
		if rv, ok := reviews[c.SHA]; ok && rv.Model != "" {
			item["model"] = rv.Model
		}
		if st := a.reviewStatus(c.SHA, reviews); st != "" {
			item["status"] = st
		}
		if n := counts[c.SHA]; len(n) > 0 {
			item["counts"] = n
		}
		if fixed[c.SHA] > 0 {
			item["fixed"] = fixed[c.SHA]
		}
		out = append(out, item)
	}
	web.JSON(w, out)
}

var hunk = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// deletedAt lists the old-side line numbers a patch removes.
func deletedAt(patch string) []int {
	out := []int{}
	old := 0
	for _, l := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(l, "@@"):
			if m := hunk.FindStringSubmatch(l); m != nil {
				old, _ = strconv.Atoi(m[1])
			}
		case strings.HasPrefix(l, "-"):
			out = append(out, old)
			old++
		case strings.HasPrefix(l, "+"), strings.HasPrefix(l, `\`):
		default:
			old++
		}
	}
	return out
}

var changeTypes = map[string]string{"added": "ADD", "modified": "MODIFY", "deleted": "DELETE", "renamed": "RENAME"}

// edgesCache keeps the class links of the current index, which every commit view needs.
var edgesCache struct {
	sync.Mutex
	sha   string
	edges []graphEdge
}

func (a *App) edges() []graphEdge {
	ix := a.index()
	edgesCache.Lock()
	defer edgesCache.Unlock()
	if edgesCache.sha != a.ixSHA || edgesCache.edges == nil {
		edgesCache.sha, edgesCache.edges = a.ixSHA, graphEdges(ix)
	}
	return edgesCache.edges
}

func (a *App) commit(w http.ResponseWriter, r *http.Request) {
	c, err := a.repo.Get(r.PathValue("sha"))
	if err != nil {
		web.Error(w, http.StatusNotFound, "That commit is not in the repository.")
		return
	}
	changes, _ := a.repo.Changes(c.SHA)
	files := make([]map[string]any, 0, len(changes))
	changed := []map[string]any{}
	touched := map[string]bool{}
	for _, ch := range changes {
		fc := map[string]any{"changeType": changeTypes[ch.Status], "path": ch.Path, "addedLines": orEmpty(ch.Added), "deletedAt": deletedAt(ch.Patch), "patch": ch.Patch}
		if ch.Status != "added" {
			fc["oldPath"] = ch.OldPath
		}
		if ch.Status != "deleted" {
			fc["newPath"] = ch.Path
		}
		files = append(files, fc)
		if !strings.HasSuffix(ch.Path, ".java") {
			continue
		}
		ref := c.SHA
		if ch.Status == "deleted" && len(c.Parents) > 0 {
			ref = c.Parents[0]
		}
		src, ok := a.repo.FileAt(ref, ch.Path)
		if !ok {
			continue
		}
		f := java.Parse(ch.Path, src)
		for _, t := range f.Types {
			var members []string
			for _, m := range t.Methods {
				for _, l := range ch.Added {
					if l >= m.Line && l <= m.End {
						members = append(members, m.Name)
						break
					}
				}
			}
			status := ch.Status
			if status == "renamed" {
				status = "modified"
			}
			changed = append(changed, map[string]any{"id": t.ID, "file": ch.Path, "status": status, "members": orEmptyS(members)})
			touched[t.ID] = true
		}
	}
	impacted := []string{}
	seen := map[string]bool{}
	for _, e := range a.edges() {
		if touched[e.Target] && !touched[e.Source] && !seen[e.Source] {
			seen[e.Source] = true
			impacted = append(impacted, e.Source)
		}
	}
	resolved := []map[string]any{}
	for _, is := range a.store.AllIssues() {
		if is.State == "resolved" && is.FixedSHA == c.SHA {
			resolved = append(resolved, a.issueJSON(is))
		}
	}
	web.JSON(w, map[string]any{
		"analysis": map[string]any{"commit": commitOf(c), "files": files, "changed": changed, "impacted": impacted},
		"review":   a.reviewJSON(c, changes),
		"fixes":    map[string]string{},
		"resolved": resolved,
	})
}

func orEmpty(xs []int) []int {
	if xs == nil {
		return []int{}
	}
	return xs
}

func orEmptyS(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

func category(rule, source string) string {
	switch {
	case rule == "hardcoded-secret" || rule == "sql-concatenation":
		return "security"
	case rule == "slow-call-in-loop" || rule == "complex-method":
		return "performance"
	case rule == "equals-without-hashcode" || rule == "shared-date-format" || rule == "transactional-private":
		return "spring"
	}
	return "bug"
}

func (a *App) reviewJSON(c git.Commit, changes []git.FileChange) any {
	rv, ok := a.store.GetReview(c.SHA)
	waiting := a.models.waiting(c.SHA) || a.checks.waiting(c.SHA)
	if !ok && !waiting {
		return nil
	}
	status := rv.Status
	if a.models.running(c.SHA) {
		status = "running"
	} else if waiting && status != "running" {
		status = "queued"
	}
	findings := a.store.Findings(c.SHA)
	issues := map[string]store.TrackedIssue{}
	for _, is := range a.store.AllIssues() {
		issues[is.Key] = is
	}
	ix := a.index()
	byFile := map[string][]map[string]any{}
	for _, f := range findings {
		node := ""
		if t, _ := ix.TypeAt(f.Path, f.Line); t != nil {
			node = t.ID
		}
		source := "rule"
		if f.Source == "model" {
			source = "llm"
		}
		j := map[string]any{"line": f.Line, "endLine": f.EndLine, "severity": f.Severity, "category": category(f.Rule, f.Source),
			"title": f.Title, "detail": f.Detail, "suggestion": f.Suggestion, "nodeId": node, "onChangedLine": true,
			"source": source, "snippet": f.Snippet, "state": f.State}
		if f.Rule != "" {
			j["ref"] = f.Rule
		}
		if f.Doubt != "" {
			j["doubt"] = f.Doubt
		}
		if is, ok := issues[f.Key]; ok {
			if is.FirstSHA != c.SHA {
				j["firstSeen"] = is.FirstSHA
			}
			if is.State == "resolved" && is.FixedSHA != "" && is.FixedSHA != "manual" {
				j["fixedIn"] = is.FixedSHA
				if fc, ok := a.commitInfo(is.FixedSHA); ok {
					j["fixedBy"], j["fixedAt"], j["fixedMsg"] = fc.Author, ms(fc.Time), fc.Subject
				}
			}
		}
		byFile[f.Path] = append(byFile[f.Path], j)
	}
	files := []map[string]any{}
	for _, ch := range changes {
		if !strings.HasSuffix(ch.Path, ".java") || ch.Status == "deleted" {
			continue
		}
		fs := byFile[ch.Path]
		if fs == nil {
			fs = []map[string]any{}
		}
		files = append(files, map[string]any{"path": ch.Path, "summary": "", "findings": fs, "pending": status == "running" || status == "queued"})
	}
	out := map[string]any{"sha": c.SHA, "status": status, "model": rv.Model, "startedAt": ms(rv.Started), "finishedAt": ms(rv.Finished),
		"summary": rv.Note, "risk": "", "highlights": []string{}, "files": files}
	if rv.Status == "failed" {
		out["error"] = rv.Note
	}
	return out
}

func (a *App) requestReview(w http.ResponseWriter, r *http.Request) {
	c, err := a.repo.Get(r.PathValue("sha"))
	if err != nil {
		web.Error(w, http.StatusNotFound, "That commit is not in the repository.")
		return
	}
	a.models.push(job{sha: c.SHA, opt: review.Options{}}, true)
	a.events.publish("review", map[string]any{"sha": c.SHA, "status": "queued"})
	web.JSON(w, map[string]any{"sha": c.SHA, "queued": true})
}

// impact is the call-level impact report. This version shows impact through the graph instead.
func (a *App) impact(w http.ResponseWriter, r *http.Request) {
	web.JSON(w, map[string]any{"sha": r.PathValue("sha"), "changed": []any{}, "callers": []any{}, "callerCount": 0, "possible": 0,
		"routes": []any{}, "tests": []any{}, "classes": []any{}})
}

var kinds = map[string]string{"bug": "Bug", "security": "Security", "performance": "Performance", "spring": "Spring"}

func urgency(sev string) string {
	switch sev {
	case "critical", "high":
		return "Fix now"
	case "medium":
		return "Fix soon"
	}
	return "When convenient"
}

func (a *App) issueJSON(is store.TrackedIssue) map[string]any {
	f := is.Latest
	source := "rule"
	if f.Source == "model" {
		source = "llm"
	}
	cat := category(f.Rule, f.Source)
	j := map[string]any{"id": is.Key, "state": is.State, "severity": f.Severity, "category": cat, "kind": kinds[cat], "urgency": urgency(f.Severity),
		"title": f.Title, "detail": f.Detail, "suggestion": f.Suggestion, "source": source, "path": is.Path, "line": f.Line,
		"snippet": f.Snippet, "firstSeen": is.FirstSHA, "lastSeen": lastSeen(f.SHA, is.FirstSHA), "commits": orEmptyS(is.Commits), "reviewLine": f.Line}
	if f.Rule != "" {
		j["ref"] = f.Rule
	}
	if fc, ok := a.commitInfo(is.FirstSHA); ok {
		j["firstTime"], j["firstBy"], j["firstMsg"] = ms(fc.Time), fc.Author, fc.Subject
	}
	if is.State == "resolved" {
		if is.FixedSHA == "manual" {
			j["manual"] = true
		} else if fc, ok := a.commitInfo(is.FixedSHA); ok {
			j["fixedIn"], j["fixedBy"], j["fixedAt"], j["fixedMsg"] = is.FixedSHA, fc.Author, ms(fc.Time), fc.Subject
		}
	}
	return j
}

func (a *App) issues(w http.ResponseWriter, r *http.Request) {
	want := r.URL.Query().Get("state")
	counts := map[string]int{}
	bySev := map[string]int{}
	out := []map[string]any{}
	for _, is := range a.store.AllIssues() {
		counts[is.State]++
		if is.State == "open" {
			bySev[is.Latest.Severity]++
		}
		if want == "" || want == "all" || want == is.State {
			out = append(out, a.issueJSON(is))
		}
	}
	web.JSON(w, map[string]any{"issues": out, "counts": counts, "openBySeverity": bySev})
}

func (a *App) resolveIssue(w http.ResponseWriter, r *http.Request) { a.setIssue(w, r, "resolved") }
func (a *App) reopenIssue(w http.ResponseWriter, r *http.Request)  { a.setIssue(w, r, "open") }

func (a *App) setIssue(w http.ResponseWriter, r *http.Request, state string) {
	if err := a.store.SetIssueState(r.PathValue("id"), state); err != nil {
		web.Error(w, http.StatusNotFound, err.Error())
		return
	}
	web.JSON(w, map[string]string{"state": state})
}

// lastSeen is the commit an issue was last reported in. The first-run scan of the whole code is not
// a commit, so its issues count as last seen where they started.
func lastSeen(sha, first string) string {
	if sha == review.BaselineSHA {
		return first
	}
	return sha
}
