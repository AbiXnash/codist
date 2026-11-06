package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"codist/internal/incident"
	"codist/internal/store"
	"codist/internal/web"
)

type frameJSON struct {
	Class  string `json:"class"`
	Method string `json:"method"`
	File   string `json:"file"`
	Line   int    `json:"line"`
}

type errorJSON struct {
	Exception string      `json:"exception"`
	Message   string      `json:"message"`
	Frames    []frameJSON `json:"frames"`
	CausedBy  []string    `json:"causedBy,omitempty"`
	Count     int         `json:"count"`
}

type blameJSON struct {
	SHA     string `json:"sha"`
	Author  string `json:"author"`
	Time    int64  `json:"time"`
	Summary string `json:"summary"`
}

type locationJSON struct {
	Path    string    `json:"path"`
	Line    int       `json:"line"`
	Class   string    `json:"class"`
	Method  string    `json:"method"`
	NodeID  string    `json:"nodeId"`
	Snippet string    `json:"snippet"`
	Origin  bool      `json:"origin"`
	Hits    int       `json:"hits"`
	Blame   blameJSON `json:"blame"`
	Issues  []string  `json:"issues,omitempty"`
}

type suspectJSON struct {
	SHA     string   `json:"sha"`
	Author  string   `json:"author"`
	Time    int64    `json:"time"`
	Message string   `json:"message"`
	Score   float64  `json:"score"`
	Reasons []string `json:"reasons"`
}

type evidenceJSON struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
	Path string `json:"path,omitempty"`
	Line int    `json:"line,omitempty"`
}

type causeJSON struct {
	Title       string         `json:"title"`
	Explanation string         `json:"explanation"`
	Confidence  string         `json:"confidence"`
	Evidence    []evidenceJSON `json:"evidence"`
	Fix         string         `json:"fix"`
	Path        string         `json:"path,omitempty"`
	Line        int            `json:"line,omitempty"`
}

type reportJSON struct {
	Summary    string `json:"summary"`
	Timeline   string `json:"timeline"`
	Impact     string `json:"impact"`
	RootCause  string `json:"rootCause"`
	Fix        string `json:"fix"`
	Prevention string `json:"prevention"`
}

// incidentJSON is an incident as the interface shows it. It is stored whole, so a page opens at once.
type incidentJSON struct {
	ID        string         `json:"id"`
	Title     string         `json:"title"`
	CreatedAt int64          `json:"createdAt"`
	Status    string         `json:"status"` // analysing, done, failed, confirmed
	Error     string         `json:"error,omitempty"`
	LogBytes  int            `json:"logBytes"`
	Excerpt   string         `json:"excerpt"`
	Errors    []errorJSON    `json:"errors"`
	Locations []locationJSON `json:"locations"`
	Suspects  []suspectJSON  `json:"suspects"`
	Similar   []any          `json:"similar"`
	Causes    []causeJSON    `json:"causes"`
	Timeline  []string       `json:"timeline"`
	Report    reportJSON     `json:"report"`
	Confirmed int            `json:"confirmed"`
	Runbook   string         `json:"runbook,omitempty"`
	Model     string         `json:"model"`
}

func (a *App) loadIncident(id string) (incidentJSON, store.Incident, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return incidentJSON{}, store.Incident{}, fmt.Errorf("no incident %s", id)
	}
	in, err := a.store.Incident(n)
	if err != nil {
		return incidentJSON{}, in, err
	}
	var j incidentJSON
	if json.Unmarshal(in.Analysis, &j) != nil || j.ID == "" {
		j = a.readLogs(in)
	}
	j.Title = in.Title
	return j, in, nil
}

func (a *App) saveIncident(j incidentJSON) {
	n, _ := strconv.ParseInt(j.ID, 10, 64)
	raw, _ := json.Marshal(j)
	_ = a.store.SetAnalysis(n, raw)
}

func (a *App) incidents(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	for _, row := range a.store.Incidents() {
		j, _, err := a.loadIncident(strconv.FormatInt(row.ID, 10))
		if err != nil {
			continue
		}
		occ := 0
		for _, e := range j.Errors {
			occ += e.Count
		}
		top := ""
		if len(j.Causes) > 0 {
			top = j.Causes[0].Title
		}
		out = append(out, map[string]any{"id": j.ID, "title": j.Title, "status": j.Status, "createdAt": j.CreatedAt, "errors": len(j.Errors),
			"occurrences": occ, "confirmed": j.Confirmed > 0, "topCause": top})
	}
	web.JSON(w, out)
}

func (a *App) createIncident(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title string `json:"title"`
		Logs  string `json:"logs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || strings.TrimSpace(in.Logs) == "" {
		web.Error(w, http.StatusBadRequest, "Paste the logs first.")
		return
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		if errs := incident.Parse(in.Logs); len(errs) > 0 {
			title = errs[0].Root
		} else {
			title = "Incident " + time.Now().Format("2 Jan 15:04")
		}
	}
	if len(title) > 120 {
		title = title[:119] + "…"
	}
	id, err := a.store.AddIncident(store.Incident{Title: title, Logs: in.Logs})
	if err != nil {
		web.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	stored, _ := a.store.Incident(id)
	j := a.readLogs(stored)
	a.saveIncident(j)
	go a.analyse(j, stored.Logs)
	web.JSON(w, map[string]string{"id": j.ID})
}

func (a *App) incident(w http.ResponseWriter, r *http.Request) {
	j, _, err := a.loadIncident(r.PathValue("id"))
	if err != nil {
		web.Error(w, http.StatusNotFound, err.Error())
		return
	}
	web.JSON(w, map[string]any{"incident": j, "markdown": markdown(j)})
}

func (a *App) updateIncident(w http.ResponseWriter, r *http.Request) {
	j, _, err := a.loadIncident(r.PathValue("id"))
	if err != nil {
		web.Error(w, http.StatusNotFound, err.Error())
		return
	}
	var in struct {
		Title     *string     `json:"title"`
		Report    *reportJSON `json:"report"`
		Confirmed *int        `json:"confirmed"`
		Runbook   *string     `json:"runbook"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		web.Error(w, http.StatusBadRequest, "That update could not be read.")
		return
	}
	if in.Title != nil && strings.TrimSpace(*in.Title) != "" {
		j.Title = strings.TrimSpace(*in.Title)
		n, _ := strconv.ParseInt(j.ID, 10, 64)
		_ = a.store.SetIncidentTitle(n, j.Title)
	}
	if in.Report != nil {
		j.Report = *in.Report
	}
	if in.Confirmed != nil {
		j.Confirmed = *in.Confirmed
		if j.Confirmed > 0 {
			j.Status = "confirmed"
		} else if j.Status == "confirmed" {
			j.Status = "done"
		}
	}
	if in.Runbook != nil {
		j.Runbook = *in.Runbook
	}
	a.saveIncident(j)
	web.JSON(w, map[string]any{"incident": j, "markdown": markdown(j)})
}

func (a *App) deleteIncident(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || a.store.DeleteIncident(n) != nil {
		web.Error(w, http.StatusNotFound, "No such incident.")
		return
	}
	web.JSON(w, map[string]bool{"deleted": true})
}

// readLogs does everything that needs no model: errors, their places in the code, the commits that
// last changed those lines, and recent commits to those files as suspects.
func (a *App) readLogs(in store.Incident) incidentJSON {
	j := incidentJSON{ID: strconv.FormatInt(in.ID, 10), Title: in.Title, CreatedAt: ms(in.Created), Status: "analysing", LogBytes: len(in.Logs),
		Errors: []errorJSON{}, Locations: []locationJSON{}, Suspects: []suspectJSON{}, Similar: []any{}, Causes: []causeJSON{}, Timeline: []string{}}
	j.Excerpt = in.Logs
	if len(j.Excerpt) > 4000 {
		j.Excerpt = j.Excerpt[:4000] + "\n…"
	}
	ix := a.index()
	issues := map[string][]string{}
	for _, is := range a.store.AllIssues() {
		if is.State == "open" {
			issues[is.Path] = append(issues[is.Path], is.Latest.Title)
		}
	}
	places := map[string]*locationJSON{}
	var order []string
	for i, e := range incident.Parse(in.Logs) {
		ej := errorJSON{Exception: e.Type, Message: e.Message, Frames: []frameJSON{}, CausedBy: e.Causes, Count: e.Count}
		first := true
		for _, f := range e.Frames {
			ej.Frames = append(ej.Frames, frameJSON{Class: f.Class, Method: f.Method, File: f.File, Line: f.Line})
			class := f.Class
			if k := strings.Index(class, "$"); k >= 0 {
				class = class[:k]
			}
			t := ix.ByID[class]
			if t == nil || f.Line <= 0 {
				continue
			}
			key := t.File + ":" + strconv.Itoa(f.Line)
			if p, ok := places[key]; ok {
				p.Hits += e.Count
				continue
			}
			loc := &locationJSON{Path: t.File, Line: f.Line, Class: t.Name, Method: f.Method, NodeID: t.ID, Origin: first && i == 0, Hits: e.Count, Issues: issues[t.File]}
			first = false
			if src, ok := a.source(t.File, ""); ok {
				lines := strings.Split(string(src), "\n")
				var b strings.Builder
				for n := max(1, f.Line-3); n <= min(len(lines), f.Line+3); n++ {
					fmt.Fprintf(&b, "%d  %s\n", n, lines[n-1])
				}
				loc.Snippet = b.String()
			}
			if c, ok := a.repo.LastChange(t.File, f.Line); ok {
				loc.Blame = blameJSON{SHA: c.SHA, Author: c.Author, Time: ms(c.Time), Summary: c.Subject}
			}
			places[key] = loc
			order = append(order, key)
		}
		j.Errors = append(j.Errors, ej)
	}
	for _, k := range order {
		j.Locations = append(j.Locations, *places[k])
	}
	// Suspects: commits that last touched the failing lines rank first, then recent changes to those files.
	score := map[string]*suspectJSON{}
	add := func(sha, author, msg string, at time.Time, pts float64, why string) {
		s, ok := score[sha]
		if !ok {
			s = &suspectJSON{SHA: sha, Author: author, Time: ms(at), Message: msg, Reasons: []string{}}
			score[sha] = s
		}
		s.Score += pts
		s.Reasons = append(s.Reasons, why)
	}
	for _, l := range j.Locations {
		if l.Blame.SHA != "" {
			pts := 2.0
			if l.Origin {
				pts = 4
			}
			add(l.Blame.SHA, l.Blame.Author, l.Blame.Summary, time.UnixMilli(l.Blame.Time), pts, fmt.Sprintf("last changed %s line %d", base(l.Path), l.Line))
		}
		for i, c := range a.repo.LogFile(l.Path, 5) {
			add(c.SHA, c.Author, c.Subject, c.Time, 1/float64(i+2), "changed "+base(l.Path))
		}
	}
	for _, s := range score {
		j.Suspects = append(j.Suspects, *s)
	}
	sort.Slice(j.Suspects, func(x, y int) bool { return j.Suspects[x].Score > j.Suspects[y].Score })
	j.Suspects = j.Suspects[:min(8, len(j.Suspects))]
	return j
}

var causeSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"summary":   map[string]any{"type": "string"},
		"impact":    map[string]any{"type": "string"},
		"rootCause": map[string]any{"type": "string"},
		"causes": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{
			"title": map[string]any{"type": "string"}, "explanation": map[string]any{"type": "string"},
			"confidence": map[string]any{"type": "string", "enum": []string{"high", "medium", "low"}},
			"evidence":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"fix":        map[string]any{"type": "string"},
		}, "required": []string{"title", "explanation", "confidence", "evidence", "fix"}}},
	},
	"required": []string{"summary", "rootCause", "causes"},
}

const causeSystem = `You help an engineer find the cause of a production error in a Java service.
You get the errors, the project's code at each stack frame, and the commits that last changed those lines.
Base every statement on what you are given. In "evidence", copy exact lines from the logs or the code; each is checked.
Give one to three causes, most likely first. If the evidence does not show a cause, say so and use low confidence.
"fix" is a concrete change to the code. Reply with JSON only.`

// analyse asks the model for causes and checks every quoted line against the logs and code.
func (a *App) analyse(j incidentJSON, logs string) {
	defer a.events.publish("incident", map[string]string{"id": j.ID, "status": j.Status})
	a.mu.RLock()
	ok := a.modelOK && !a.cfg.ModelOff
	a.mu.RUnlock()
	if !ok || len(j.Errors) == 0 {
		j.Status = "done"
		if len(j.Errors) == 0 {
			j.Report.Summary = "No Java errors were found in these logs. Codist looks for exception lines followed by \"at ...\" stack frames."
		} else {
			j.Report.Summary = "The model is not available, so only the errors, code locations and suspect commits are shown."
		}
		a.saveIncident(j)
		return
	}
	var b strings.Builder
	var corpus strings.Builder
	corpus.WriteString(logs)
	for _, e := range j.Errors[:min(3, len(j.Errors))] {
		fmt.Fprintf(&b, "## %s: %s (seen %d times)\n", e.Exception, e.Message, e.Count)
		for _, c := range e.CausedBy {
			fmt.Fprintf(&b, "Caused by: %s\n", c)
		}
		for _, f := range e.Frames[:min(12, len(e.Frames))] {
			fmt.Fprintf(&b, "  at %s.%s(%s:%d)\n", f.Class, f.Method, f.File, f.Line)
		}
	}
	for _, l := range j.Locations[:min(5, len(j.Locations))] {
		fmt.Fprintf(&b, "\n## %s line %d (%s.%s)\n```java\n%s```\n", l.Path, l.Line, l.Class, l.Method, l.Snippet)
		corpus.WriteString("\n" + l.Snippet)
		if l.Blame.SHA != "" {
			fmt.Fprintf(&b, "Line last changed by %s \"%s\" (%s)\n", l.Blame.SHA[:7], l.Blame.Summary, l.Blame.Author)
		}
	}
	var out struct {
		Summary   string `json:"summary"`
		Impact    string `json:"impact"`
		RootCause string `json:"rootCause"`
		Causes    []struct {
			Title, Explanation, Confidence, Fix string
			Evidence                            []string
		} `json:"causes"`
	}
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Minute)
	defer cancel()
	j.Model = a.model.Name
	if _, err := a.model.ChatJSON(ctx, causeSystem, b.String(), causeSchema, &out); err != nil {
		j.Status, j.Error = "failed", err.Error()
		a.saveIncident(j)
		return
	}
	all := strings.Join(strings.Fields(corpus.String()), "")
	for _, c := range out.Causes {
		cj := causeJSON{Title: c.Title, Explanation: c.Explanation, Confidence: c.Confidence, Fix: c.Fix, Evidence: []evidenceJSON{}}
		dropped := 0
		for _, ev := range c.Evidence {
			if s := strings.Join(strings.Fields(ev), ""); s != "" && strings.Contains(all, s) {
				kind := "log"
				if !strings.Contains(strings.Join(strings.Fields(logs), ""), s) {
					kind = "code"
				}
				cj.Evidence = append(cj.Evidence, evidenceJSON{Kind: kind, Text: ev})
			} else {
				dropped++
			}
		}
		if len(cj.Evidence) == 0 && len(c.Evidence) > 0 {
			cj.Confidence = "low"
			cj.Explanation += "\n\n(None of the model's quoted evidence is in the logs or the code, so treat this as a guess.)"
		} else if dropped > 0 {
			cj.Explanation += fmt.Sprintf("\n\n(%d quoted lines were not in the logs or code and were removed.)", dropped)
		}
		if len(j.Locations) > 0 {
			cj.Path, cj.Line = j.Locations[0].Path, j.Locations[0].Line
		}
		j.Causes = append(j.Causes, cj)
	}
	j.Report = reportJSON{Summary: out.Summary, Impact: out.Impact, RootCause: out.RootCause}
	if len(j.Causes) > 0 {
		j.Report.Fix = j.Causes[0].Fix
	}
	j.Status = "done"
	a.saveIncident(j)
}

// markdown is the incident as a report a person can paste into a ticket.
func markdown(j incidentJSON) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", j.Title)
	if j.Report.Summary != "" {
		fmt.Fprintf(&b, "%s\n\n", j.Report.Summary)
	}
	if j.Report.RootCause != "" {
		fmt.Fprintf(&b, "## Root cause\n%s\n\n", j.Report.RootCause)
	}
	if len(j.Errors) > 0 {
		b.WriteString("## Errors\n")
		for _, e := range j.Errors {
			fmt.Fprintf(&b, "- %s: %s (%d times)\n", e.Exception, e.Message, e.Count)
		}
		b.WriteString("\n")
	}
	if len(j.Locations) > 0 {
		b.WriteString("## Where in the code\n")
		for _, l := range j.Locations {
			fmt.Fprintf(&b, "- %s:%d in %s.%s\n", l.Path, l.Line, l.Class, l.Method)
		}
		b.WriteString("\n")
	}
	if j.Report.Fix != "" {
		fmt.Fprintf(&b, "## Fix\n%s\n", j.Report.Fix)
	}
	return b.String()
}
