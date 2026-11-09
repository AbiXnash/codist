package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	model "github.com/abxengg/codist-model"
	"github.com/abxengg/codist-model/incident"
	"github.com/abxengg/codist-review/store"
	"github.com/abxengg/codist/internal/web"
)

// projectCode answers the incident analysis's questions about the project from the index, git and the issue store.
type projectCode struct{ a *App }

func (p projectCode) Class(name string) (incident.Class, bool) {
	t := p.a.index().ByID[name]
	if t == nil {
		return incident.Class{}, false
	}
	return incident.Class{Path: t.File, Name: t.Name, NodeID: t.ID}, true
}

func (p projectCode) Source(path string) ([]byte, bool) { return p.a.source(path, "") }

func (p projectCode) LastChange(path string, line int) (incident.Commit, bool) {
	c, ok := p.a.repo.LastChange(path, line)
	return incident.Commit{SHA: c.SHA, Author: c.Author, Subject: c.Subject, Time: c.Time}, ok
}

func (p projectCode) History(path string, limit int) []incident.Commit {
	var out []incident.Commit
	for _, c := range p.a.repo.LogFile(path, limit) {
		out = append(out, incident.Commit{SHA: c.SHA, Author: c.Author, Subject: c.Subject, Time: c.Time})
	}
	return out
}

func (p projectCode) OpenIssues(path string) []string {
	var out []string
	for _, is := range p.a.store.AllIssues() {
		if is.State == "open" && is.Path == path {
			out = append(out, is.Latest.Title)
		}
	}
	return out
}

func (a *App) readLogs(in store.Incident) incident.Analysis {
	return incident.Read(strconv.FormatInt(in.ID, 10), in.Title, in.Created, in.Logs, projectCode{a})
}

func (a *App) loadIncident(id string) (incident.Analysis, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return incident.Analysis{}, fmt.Errorf("no incident %s", id)
	}
	in, err := a.store.Incident(n)
	if err != nil {
		return incident.Analysis{}, err
	}
	var j incident.Analysis
	if json.Unmarshal(in.Analysis, &j) != nil || j.ID == "" {
		j = a.readLogs(in)
	}
	j.Title = in.Title
	return j, nil
}

func (a *App) saveIncident(j incident.Analysis) {
	n, _ := strconv.ParseInt(j.ID, 10, 64)
	raw, _ := json.Marshal(j)
	_ = a.store.SetAnalysis(n, raw)
}

// analyse asks the model for causes in the background, when it is available, and tells the pages.
func (a *App) analyse(j incident.Analysis, logs string) {
	a.mu.RLock()
	var m *model.Client
	if a.modelOK && !a.cfg.ModelOff {
		m = a.model
	}
	a.mu.RUnlock()
	ctx, cancel := context.WithTimeout(a.ctx, 10*time.Minute)
	defer cancel()
	incident.Explain(ctx, &j, logs, m)
	a.saveIncident(j)
	a.events.publish("incident", map[string]string{"id": j.ID, "status": j.Status})
}

func (a *App) incidents(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	for _, row := range a.store.Incidents() {
		j, err := a.loadIncident(strconv.FormatInt(row.ID, 10))
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
	id, err := a.store.AddIncident(store.Incident{Title: incident.Title(in.Title, in.Logs, time.Now()), Logs: in.Logs})
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
	j, err := a.loadIncident(r.PathValue("id"))
	if err != nil {
		web.Error(w, http.StatusNotFound, err.Error())
		return
	}
	web.JSON(w, map[string]any{"incident": j, "markdown": incident.Markdown(j)})
}

func (a *App) updateIncident(w http.ResponseWriter, r *http.Request) {
	j, err := a.loadIncident(r.PathValue("id"))
	if err != nil {
		web.Error(w, http.StatusNotFound, err.Error())
		return
	}
	var in struct {
		Title     *string          `json:"title"`
		Report    *incident.Report `json:"report"`
		Confirmed *int             `json:"confirmed"`
		Runbook   *string          `json:"runbook"`
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
	web.JSON(w, map[string]any{"incident": j, "markdown": incident.Markdown(j)})
}

func (a *App) deleteIncident(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || a.store.DeleteIncident(n) != nil {
		web.Error(w, http.StatusNotFound, "No such incident.")
		return
	}
	web.JSON(w, map[string]bool{"deleted": true})
}
