package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"codist/internal/incident"
	"codist/internal/store"
)

// place is a stack frame in the project's own code, with the code around it and the commit that
// last changed that line.
type place struct {
	incident.Frame
	Path    string
	Code    []codeLine
	Commit  string
	Short   string
	Subject string
	Author  string
	When    time.Time
}

type codeLine struct {
	No   int
	Text string
	Hit  bool
}

type errorView struct {
	incident.Error
	Places   []place
	External int // frames outside the project
}

// explanation is the model's reading of an incident, checked against the evidence it was given.
type explanation struct {
	Cause      string   `json:"cause"`
	Evidence   []string `json:"evidence"`
	Fix        string   `json:"fix"`
	Confidence string   `json:"confidence"`
	Model      string   `json:"model,omitempty"`
	Doubt      string   `json:"doubt,omitempty"`
	Error      string   `json:"error,omitempty"`
}

type incidentPage struct {
	Incident    store.Incident
	Errors      []errorView
	Explanation *explanation
	CanExplain  bool
}

func (a *App) incidentRoutes() {
	a.web.Handle("GET /incidents", func(w http.ResponseWriter, r *http.Request) {
		a.web.Render(w, r, "incidents", "incidents", "Incidents", map[string]any{"List": a.store.Incidents()})
	})
	a.web.Handle("POST /incidents", func(w http.ResponseWriter, r *http.Request) {
		logs := strings.TrimSpace(r.FormValue("logs"))
		title := strings.TrimSpace(r.FormValue("title"))
		if logs == "" {
			http.Error(w, "Paste the logs first.", http.StatusBadRequest)
			return
		}
		if title == "" {
			if errs := incident.Parse(logs); len(errs) > 0 {
				title = errs[0].Root
			} else {
				title = "Incident " + time.Now().Format("2 Jan 15:04")
			}
		}
		id, err := a.store.AddIncident(store.Incident{Title: trim(title, 120), Logs: logs})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/incidents/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
	})
	a.web.Handle("GET /incidents/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, err := a.incidentPage(r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		a.web.Render(w, r, "incident", "incidents", p.Incident.Title, p)
	})
	a.web.Handle("POST /incidents/{id}/explain", func(w http.ResponseWriter, r *http.Request) {
		p, err := a.incidentPage(r.PathValue("id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		ex := a.explain(r, p)
		raw, _ := json.Marshal(ex)
		_ = a.store.SetAnalysis(p.Incident.ID, raw)
		p.Explanation = &ex
		a.web.Fragment(w, "incident", "explanation", p)
	})
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func (a *App) incidentPage(idText string) (incidentPage, error) {
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		return incidentPage{}, fmt.Errorf("no incident %s", idText)
	}
	in, err := a.store.Incident(id)
	if err != nil {
		return incidentPage{}, err
	}
	p := incidentPage{Incident: in}
	a.mu.RLock()
	p.CanExplain = a.modelOK && !a.cfg.ModelOff
	a.mu.RUnlock()
	var ex explanation
	if json.Unmarshal(in.Analysis, &ex) == nil && (ex.Cause != "" || ex.Error != "") {
		p.Explanation = &ex
	}
	ix := a.index()
	for _, e := range incident.Parse(in.Logs) {
		v := errorView{Error: e}
		for _, f := range e.Frames {
			class := f.Class
			if i := strings.Index(class, "$"); i >= 0 {
				class = class[:i]
			}
			t := ix.ByID[class]
			if t == nil {
				v.External++
				continue
			}
			pl := place{Frame: f, Path: t.File}
			if src, err := os.ReadFile(filepath.Join(a.repo.Root, filepath.FromSlash(t.File))); err == nil && f.Line > 0 {
				lines := strings.Split(string(src), "\n")
				for n := max(1, f.Line-3); n <= min(len(lines), f.Line+3); n++ {
					pl.Code = append(pl.Code, codeLine{No: n, Text: lines[n-1], Hit: n == f.Line})
				}
			}
			if c, ok := a.repo.LastChange(t.File, f.Line); ok {
				pl.Commit, pl.Short, pl.Subject, pl.Author, pl.When = c.SHA, c.Short, c.Subject, c.Author, c.Time
			}
			v.Places = append(v.Places, pl)
		}
		p.Errors = append(p.Errors, v)
	}
	return p, nil
}

var explainSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"cause":      map[string]any{"type": "string"},
		"evidence":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"fix":        map[string]any{"type": "string"},
		"confidence": map[string]any{"type": "string", "enum": []string{"high", "medium", "low"}},
	},
	"required": []string{"cause", "evidence", "fix", "confidence"},
}

const explainSystem = `You help an engineer find the cause of a production error in a Java service.
You get the error, the project's code at each stack frame, and the commit that last changed each of those lines.
Base every statement on what you are given. In "evidence", copy exact lines from the log or the code that support
the cause; each is checked. If the evidence does not show a cause, say so and set confidence to low.
"fix" is a concrete change to the code. Reply with JSON only.`

// explain asks the model about the most frequent error and checks that its evidence is real.
func (a *App) explain(r *http.Request, p incidentPage) explanation {
	if len(p.Errors) == 0 {
		return explanation{Error: "There is no Java error in these logs to explain."}
	}
	e := p.Errors[0]
	var b strings.Builder
	fmt.Fprintf(&b, "## Error (seen %d times)\n%s\n", e.Count, e.First)
	for _, c := range e.Causes {
		fmt.Fprintf(&b, "Caused by: %s\n", c)
	}
	b.WriteString("\n## Stack frames, innermost cause\n")
	for _, f := range e.Frames {
		b.WriteString(f.Raw + "\n")
	}
	var corpus strings.Builder // everything the model saw, to check its quotes against
	corpus.WriteString(p.Incident.Logs)
	for _, pl := range e.Places {
		fmt.Fprintf(&b, "\n## %s line %d (in %s.%s)\n```java\n", pl.Path, pl.Line, pl.Class, pl.Method)
		for _, l := range pl.Code {
			mark := "  "
			if l.Hit {
				mark = ">>"
			}
			fmt.Fprintf(&b, "%s%4d| %s\n", mark, l.No, l.Text)
			corpus.WriteString("\n" + l.Text)
		}
		b.WriteString("```\n")
		if pl.Commit != "" {
			fmt.Fprintf(&b, "Last changed by commit %s \"%s\" (%s, %s)\n", pl.Short, pl.Subject, pl.Author, pl.When.Format("2 Jan 2006"))
		}
	}
	var ex explanation
	if _, err := a.model.ChatJSON(r.Context(), explainSystem, b.String(), explainSchema, &ex); err != nil {
		return explanation{Error: err.Error()}
	}
	ex.Model = a.model.Name
	all := squashSpace(corpus.String())
	var real []string
	for _, ev := range ex.Evidence {
		if s := squashSpace(ev); s != "" && strings.Contains(all, s) {
			real = append(real, ev)
		}
	}
	if len(ex.Evidence) > 0 && len(real) == 0 {
		ex.Doubt = "None of the model's quoted evidence is in the logs or the code, so treat this as a guess."
	} else if len(real) < len(ex.Evidence) {
		ex.Doubt = fmt.Sprintf("%d of the model's quotes are not in the logs or the code and were removed.", len(ex.Evidence)-len(real))
	}
	ex.Evidence = real
	return ex
}

func squashSpace(s string) string { return strings.Join(strings.Fields(s), "") }
