package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/AbiXnash/codist/internal/web"
)

// The behaviour lane runs the methods each recent commit changed, before and after it, and keeps what differs.
// It is off unless the repository turns it on, since it runs the project's code.

// runBehaviour queues the recent commits that have no report yet, then works through the lane, newest first.
func (a *App) runBehaviour(ctx context.Context) {
	done := a.store.BehaviourDone()
	log, _ := a.repo.LogRef("HEAD", a.historyLimit())
	for _, c := range log {
		if !done[c.SHA] {
			a.runs.push(job{sha: c.SHA}, false)
		}
	}
	for {
		j, ok := a.runs.pop()
		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-a.runs.wake:
				continue
			}
		}
		a.events.publish("behaviour", map[string]string{"sha": j.sha, "status": "running"})
		rep := a.runner.Run(ctx, j.sha)
		raw, _ := json.Marshal(rep)
		if err := a.store.SetBehaviour(rep.SHA, raw); err != nil {
			slog.Warn("behaviour not saved", "sha", short(j.sha), "err", err)
		}
		a.runs.done(j.sha)
		slog.Info("behaviour", "sha", short(j.sha), "status", rep.Status, "differ", rep.Differ, "took", rep.Took, "reason", rep.Reason)
		a.events.publish("behaviour", map[string]any{"sha": rep.SHA, "status": rep.Status, "differ": rep.Differ})
	}
}

// behaviourOf is a commit's report for the page: the stored one, "queued" or "running" while waiting, or nil when
// the lane is off.
func (a *App) behaviourOf(sha string) any {
	if a.runner == nil {
		return nil
	}
	switch {
	case a.runs.running(sha):
		return map[string]string{"sha": sha, "status": "running"}
	case a.runs.waiting(sha):
		return map[string]string{"sha": sha, "status": "queued"}
	}
	if raw, ok := a.store.Behaviour(sha); ok {
		return json.RawMessage(raw)
	}
	return nil
}

// behaviourBrief is the lane's state for the commit list: queued, running, skipped, done, or differs when a run
// found a changed result.
func (a *App) behaviourBrief(sha string) string {
	var r map[string]any
	raw, _ := json.Marshal(a.behaviourOf(sha))
	_ = json.Unmarshal(raw, &r)
	if d, _ := r["differ"].(float64); d > 0 {
		return "differs"
	}
	s, _ := r["status"].(string)
	return s
}

// requestBehaviour runs one commit next.
func (a *App) requestBehaviour(w http.ResponseWriter, r *http.Request) {
	if a.runner == nil {
		web.Error(w, http.StatusConflict, "Running changed methods is off. Turn it on with behaviour = true in .codist/config.lua, or CODIST_BEHAVIOUR=1.")
		return
	}
	c, err := a.repo.Get(r.PathValue("sha"))
	if err != nil {
		web.Error(w, http.StatusNotFound, "That commit is not in the repository.")
		return
	}
	a.runs.push(job{sha: c.SHA}, true)
	a.events.publish("behaviour", map[string]string{"sha": c.SHA, "status": "queued"})
	web.JSON(w, map[string]any{"sha": c.SHA, "queued": true})
}
