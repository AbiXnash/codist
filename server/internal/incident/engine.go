package incident

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"codist/internal/events"
	"codist/internal/gitx"
	"codist/internal/graph"
	"codist/internal/javaidx"
	"codist/internal/review"
)

// Engine runs root cause analyses.
type Engine struct {
	repo    *gitx.Repo
	graph   *graph.Service
	llm     *review.Ollama
	tracker *review.Tracker
	reviews *review.Service
	store   *Store
	hub     *events.Hub
}

func NewEngine(repo *gitx.Repo, g *graph.Service, llm *review.Ollama, tracker *review.Tracker, reviews *review.Service, store *Store, hub *events.Hub) *Engine {
	return &Engine{repo: repo, graph: g, llm: llm, tracker: tracker, reviews: reviews, store: store, hub: hub}
}

func (e *Engine) Store() *Store { return e.store }

// Start records a new incident and analyses it in the background.
func (e *Engine) Start(title, logs string) (*Incident, error) {
	evs := Parse(logs)
	if len(evs) == 0 {
		return nil, fmt.Errorf("no errors or stack traces found in the logs")
	}
	in := &Incident{ID: newID(), Title: strings.TrimSpace(title), CreatedAt: time.Now().UnixMilli(), Status: "analysing",
		LogBytes: len(logs), Errors: evs, Confirmed: -1, Model: e.llm.Model}
	if in.Title == "" {
		in.Title = defaultTitle(evs)
	}
	in.Excerpt = excerpt(logs)
	e.store.Save(in)
	e.publish(in)
	go e.analyse(in)
	return in, nil
}

func (e *Engine) publish(in *Incident) {
	e.hub.Publish("incident", map[string]string{"id": in.ID, "status": in.Status})
}

func (e *Engine) analyse(in *Incident) {
	defer func() {
		if r := recover(); r != nil {
			in.Status, in.Error = "failed", fmt.Sprint(r)
			e.store.Save(in)
			e.publish(in)
		}
	}()
	in.Locations = e.locate(in.Errors)
	in.Suspects = e.suspects(in.Locations, namedFields(in.Errors))
	isProject := e.isProjectClass()
	toks := Tokens(in.Errors, isProject)
	for t := range toks {
		in.Tokens = append(in.Tokens, t)
	}
	sort.Strings(in.Tokens)
	in.Similar = e.similar(in.ID, toks)
	in.Timeline = timeline(in)
	e.store.Save(in) // the deterministic evidence is useful even if the model is unavailable
	e.publish(in)

	if p := e.llm.Problem(); p != "" {
		in.Status, in.Error = "done", p+". The evidence below was gathered without the model."
		e.store.Save(in)
		e.publish(in)
		return
	}
	if err := e.explain(in); err != nil {
		slog.Warn("rca model step failed", "incident", in.ID, "err", err)
		in.Error = "The model could not explain this incident: " + err.Error()
	}
	in.Status = "done"
	e.store.Save(in)
	e.publish(in)
}

// isProjectClass reports whether a frame class belongs to the indexed project.
func (e *Engine) isProjectClass() func(string) bool {
	ids := map[string]bool{}
	for _, n := range e.graph.Graph().Nodes {
		ids[n.ID] = true
	}
	return func(c string) bool { return ids[OuterClass(c)] }
}

// abbreviated matches Spring's shortened logger names (c.d.s.r.ReconService) to a full name.
func abbreviated(short, full string) bool {
	sp, fp := strings.Split(short, "."), strings.Split(full, ".")
	if len(sp) != len(fp) {
		return false
	}
	for i := range sp {
		if sp[i] != fp[i] && !(len(sp[i]) == 1 && strings.HasPrefix(fp[i], sp[i])) {
			return false
		}
	}
	return true
}

func (e *Engine) locate(evs []ErrorEvent) []Location {
	nodes := map[string]javaidxNode{}
	for _, n := range e.graph.Graph().Nodes {
		nodes[n.ID] = javaidxNode{path: n.File, members: n.Members}
	}
	head := e.repo.Head()
	files := map[string][]string{}
	read := func(path string) []string {
		if l, ok := files[path]; ok {
			return l
		}
		src, ok := e.repo.ReadFile(head, path)
		if !ok {
			src, _, _ = e.repo.ReadWorking(path)
		}
		files[path] = strings.Split(src, "\n")
		return files[path]
	}

	byKey := map[string]*Location{}
	var order []string
	add := func(class, method string, line int, origin bool, hits int) {
		n, ok := nodes[OuterClass(class)]
		if !ok {
			return
		}
		lines := read(n.path)
		if line <= 0 || line > len(lines) {
			line = 0
			for _, m := range n.members {
				if m.Name == method {
					line = m.StartLine
				}
			}
			if line == 0 {
				return
			}
		}
		key := fmt.Sprintf("%s:%d", n.path, line)
		if l, ok := byKey[key]; ok {
			l.Hits += hits
			l.Origin = l.Origin || origin
			return
		}
		loc := &Location{Path: n.path, Line: line, Class: OuterClass(class), Method: method, NodeID: OuterClass(class),
			Snippet: strings.TrimSpace(lines[line-1]), Origin: origin, Hits: hits}
		for _, m := range n.members {
			if m.StartLine <= line && line <= m.EndLine && m.Kind != "field" {
				loc.Method, loc.MethodBeg, loc.MethodEnd = m.Name, m.StartLine, m.EndLine
			}
		}
		loc.Blame, _ = e.repo.Blame(n.path, line)
		byKey[key] = loc
		order = append(order, key)
	}

	for _, ev := range evs {
		first := true
		for _, f := range ev.Frames {
			if _, ok := nodes[OuterClass(f.Class)]; !ok || strings.Contains(f.Class, "$$") || f.Line == 0 && f.File == "<generated>" {
				continue
			}
			add(f.Class, f.Method, f.Line, first, ev.Count)
			first = false
		}
		if len(ev.Frames) == 0 && ev.Logger != "" {
			for id := range nodes {
				if id == ev.Logger || abbreviated(ev.Logger, id) {
					add(id, "", 0, true, ev.Count)
				}
			}
		}
	}

	// Open Codist issues near each location.
	issues := e.tracker.Issues()
	out := make([]Location, 0, len(order))
	for _, k := range order {
		l := byKey[k]
		for _, is := range issues {
			if is.State == "open" && is.Path == l.Path && (abs(is.Line-l.Line) <= 3 || (l.MethodBeg > 0 && is.Line >= l.MethodBeg && is.Line <= l.MethodEnd) || is.Severity == "critical") {
				l.Issues = append(l.Issues, fmt.Sprintf("%s (%s, line %d, since %s)", is.Title, is.Severity, is.Line, is.FirstSeen[:min(7, len(is.FirstSeen))]))
			}
		}
		out = append(out, *l)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Origin != out[j].Origin {
			return out[i].Origin
		}
		return out[i].Hits > out[j].Hits
	})
	if len(out) > 12 {
		out = out[:12]
	}
	return out
}

type javaidxNode struct {
	path    string
	members []javaidx.Member
}

var nullField = regexp.MustCompile(`because "(?:this\.)?(\w+)(?:\.\w+)*" is null|"this\.(\w+)"`)

// namedFields are the fields an error message blames, e.g. "this.customers" is null.
func namedFields(evs []ErrorEvent) map[string]bool {
	out := map[string]bool{}
	for _, ev := range evs {
		for _, m := range nullField.FindAllStringSubmatch(ev.Message, -1) {
			for _, g := range m[1:] {
				if g != "" {
					out[g] = true
				}
			}
		}
	}
	return out
}

var severityWeight = map[string]float64{"critical": 10, "high": 6, "medium": 3, "low": 1, "info": 0.5}

// suspects ranks recent commits that touched the failing code. Signals, strongest first: Codist
// flagged a severe issue at that spot in the commit; the commit last changed the declaration of a
// field the error names (a removed constructor leaves "this.x is null"); it changed the failing
// method; it last changed the failing line; it touched the file at all.
func (e *Engine) suspects(locs []Location, fields map[string]bool) []Suspect {
	type acc struct {
		s       Suspect
		reasons map[string]bool
	}
	by := map[string]*acc{}
	commits := map[string]gitx.Commit{}
	for _, c := range e.repo.Log(300) {
		commits[c.SHA] = c
	}
	diffs := map[string][]gitx.FileChange{}
	bump := func(sha string, score float64, reason string) {
		c, ok := commits[sha]
		if !ok {
			return
		}
		a := by[sha]
		if a == nil {
			a = &acc{s: Suspect{SHA: sha, Author: c.Author, Time: c.Time, Message: c.Message}, reasons: map[string]bool{}}
			by[sha] = a
		}
		a.s.Score += score
		if !a.reasons[reason] {
			a.reasons[reason] = true
			a.s.Reasons = append(a.s.Reasons, reason)
		}
	}
	for li, l := range locs {
		weight := 1.0 / (1 + float64(li)*0.25) // the throwing frame matters most
		if l.Origin {
			weight *= 1.5
		}
		file := l.Path[strings.LastIndex(l.Path, "/")+1:]
		if l.Blame.SHA != "" {
			bump(l.Blame.SHA, 2*weight, fmt.Sprintf("last changed the failing line %s:%d", file, l.Line))
		}
		// The declaration of a field the error names.
		for _, n := range e.graph.Graph().Nodes {
			if n.ID != l.Class {
				continue
			}
			for _, m := range n.Members {
				if m.Kind == "field" && fields[m.Name] {
					if b, ok := e.repo.Blame(l.Path, m.StartLine); ok {
						bump(b.SHA, 8, fmt.Sprintf("last changed the declaration of '%s', which the error says is null", m.Name))
					}
				}
			}
		}
		touching := e.repo.CommitsTouching(l.Path)
		for rank, sha := range touching {
			if rank == 15 {
				break
			}
			recency := 1 / (1 + float64(rank)*0.2)
			bump(sha, 1*weight*recency, "changed "+file)
			if l.MethodBeg > 0 {
				d, ok := diffs[sha]
				if !ok {
					if c, ok2 := commits[sha]; ok2 {
						d, _ = e.repo.Diff(c)
					}
					diffs[sha] = d
				}
				for _, fc := range d {
					if fc.NewPath != l.Path {
						continue
					}
					for _, al := range append(append([]int{}, fc.AddedLines...), fc.DeletedAt...) {
						if al >= l.MethodBeg && al <= l.MethodEnd {
							bump(sha, 3*weight*recency, fmt.Sprintf("changed %s() where the error occurs", l.Method))
							break
						}
					}
				}
			}
		}
		for _, is := range l.Issues {
			if i := strings.LastIndex(is, "since "); i > 0 {
				short := strings.TrimSuffix(is[i+6:], ")")
				sev := ""
				if open := strings.LastIndex(is[:i], "("); open > 0 {
					sev = strings.SplitN(is[open+1:], ",", 2)[0]
				}
				for sha := range commits {
					if strings.HasPrefix(sha, short) {
						bump(sha, severityWeight[sev]*weight, fmt.Sprintf("Codist flagged %s (%s) in this commit", strings.SplitN(is, " (", 2)[0], sev))
					}
				}
			}
		}
	}
	var out []Suspect
	for _, a := range by {
		a.s.Score = math.Round(a.s.Score*10) / 10
		out = append(out, a.s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

// similar finds past incidents with overlapping exceptions, frames and messages.
func (e *Engine) similar(self string, toks map[string]bool) []Similar {
	weight := func(t string) float64 {
		switch {
		case strings.HasPrefix(t, "ex:"):
			return 3
		case strings.HasPrefix(t, "at:"), strings.HasPrefix(t, "log:"):
			return 2
		}
		return 1
	}
	var out []Similar
	for _, past := range e.store.All() {
		if past.ID == self || past.Status == "analysing" {
			continue
		}
		other := map[string]bool{}
		for _, t := range past.Tokens {
			other[t] = true
		}
		var inter, union float64
		for t := range toks {
			union += weight(t)
			if other[t] {
				inter += weight(t)
			}
		}
		for t := range other {
			if !toks[t] {
				union += weight(t)
			}
		}
		if union == 0 {
			continue
		}
		score := inter / union
		if score < 0.25 {
			continue
		}
		root := past.Report.RootCause
		if past.Confirmed >= 0 && past.Confirmed < len(past.Causes) {
			root = past.Causes[past.Confirmed].Title + ". " + root
		}
		out = append(out, Similar{ID: past.ID, Title: past.Title, Score: math.Round(score*100) / 100, RootCause: strings.TrimSpace(root), CreatedAt: past.CreatedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

func timeline(in *Incident) []string {
	var t []string
	for _, s := range in.Suspects {
		if len(t) == 3 {
			break
		}
		t = append(t, fmt.Sprintf("%s  commit %s by %s: %s", time.UnixMilli(s.Time).Format("2006-01-02 15:04"), s.SHA[:7], s.Author, s.Message))
	}
	for _, ev := range in.Errors {
		name := ev.Exception
		if name == "" {
			name = ev.Message
		}
		name = name[strings.LastIndex(name, ".")+1:]
		if ev.FirstTime != "" {
			when := ev.FirstTime
			if ev.LastTime != "" && ev.LastTime != ev.FirstTime {
				when += " → " + ev.LastTime
			}
			t = append(t, fmt.Sprintf("%s  %s ×%d", when, trim(name, 80), ev.Count))
		}
	}
	sort.Strings(t)
	return t
}

var interesting = regexp.MustCompile(`\b(ERROR|FATAL|SEVERE|WARN)\b|Exception|Error:|^\s*at |Caused by:`)

// excerpt keeps the log lines around errors (at most 400 lines), for the record.
func excerpt(raw string) string {
	lines := strings.Split(raw, "\n")
	keep := map[int]bool{}
	for i, l := range lines {
		if interesting.MatchString(l) {
			for k := max(0, i-1); k <= min(len(lines)-1, i+1); k++ {
				keep[k] = true
			}
		}
	}
	var b strings.Builder
	n, last := 0, -2
	for i, l := range lines {
		if !keep[i] {
			continue
		}
		if i != last+1 && n > 0 {
			b.WriteString("…\n")
		}
		b.WriteString(l + "\n")
		last = i
		if n++; n >= 400 {
			b.WriteString("… (truncated)\n")
			break
		}
	}
	return b.String()
}

func defaultTitle(evs []ErrorEvent) string {
	ev := evs[0]
	name := ev.Exception
	if name == "" {
		return trim(ev.Message, 80)
	}
	name = name[strings.LastIndex(name, ".")+1:]
	for _, f := range ev.Frames {
		if !strings.HasPrefix(f.Class, "java.") && !strings.HasPrefix(f.Class, "org.springframework") && !strings.HasPrefix(f.Class, "jdk.") && !strings.Contains(f.Class, "$$") {
			return fmt.Sprintf("%s in %s.%s()", name, simpleClass(f.Class), f.Method)
		}
	}
	return name
}

func trim(s string, n int) string {
	if len(s) > n {
		return s[:n-1] + "…"
	}
	return s
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
