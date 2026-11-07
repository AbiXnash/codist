package app

import (
	"encoding/json"
	"net/http"
	"time"

	review "github.com/AbiXnash/codist-review"
	"github.com/AbiXnash/codist/internal/web"
)

func (a *App) status(w http.ResponseWriter, r *http.Request) {
	ix := a.index()
	a.mu.RLock()
	modelOK, note, view := a.modelOK, a.modelNote, a.view
	a.mu.RUnlock()
	checkedOut := a.repo.Branch()
	branch := checkedOut
	if view != "" {
		branch = view
	}
	problem := ""
	if a.cfg.ModelOff {
		problem = "Model review is off (CODIST_MODEL_REVIEW)."
	} else if !modelOK {
		problem = note
	}
	head := a.repo.Resolve(a.viewRef())
	web.JSON(w, map[string]any{
		"repo": base(a.repo.Root), "path": a.repo.Root, "branch": branch, "viewing": view != "", "checkedOut": checkedOut,
		"tracking": branch, "head": head, "model": a.cfg.Model, "llmProblem": problem,
		"graphSource": head, "graphBuiltAt": time.Now().UnixMilli(), "canFix": false, "user": "",
		"rules": 15, "models": map[string]string{"review": a.cfg.Model, "summary": a.cfg.Model, "fix": a.cfg.Model, "ask": a.cfg.Model},
		"types": len(ix.Types), "behaviour": a.runner != nil, "modelUsage": a.modelUsage(),
	})
}

// features turns on only what this version has: Review, Graph, Incidents and Repository.
func (a *App) features(w http.ResponseWriter, r *http.Request) {
	web.JSON(w, map[string]any{
		"menus": map[string]bool{"review": true, "repo": true, "incidents": true, "branches": false, "mine": false, "team": false,
			"release": false, "activity": false, "memory": false},
		"panels":   map[string]bool{"issues": true, "commit": true, "code": true, "ask": false},
		"features": map[string]bool{"explorer": true, "branchGraph": false, "branchSwitcher": true, "fix": false, "reword": false, "localPlan": false},
		"sources":  []string{},
	})
}

func (a *App) refs(w http.ResponseWriter, r *http.Request) {
	current := a.repo.Branch()
	out := []map[string]any{}
	for _, ref := range a.repo.Refs() {
		out = append(out, map[string]any{"name": ref.Name, "remote": ref.Remote, "sha": ref.SHA, "message": ref.Message,
			"by": ref.By, "at": ms(ref.At), "checkedOut": !ref.Remote && ref.Name == current})
	}
	web.JSON(w, out)
}

// follow switches the branch Codist shows. Another branch is read from git, so nothing in the
// working tree changes.
func (a *App) follow(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" {
		web.Error(w, http.StatusBadRequest, "Name a branch.")
		return
	}
	if a.repo.Resolve(in.Name) == "" {
		web.Error(w, http.StatusNotFound, "There is no branch "+in.Name+".")
		return
	}
	a.mu.Lock()
	if in.Name == a.repo.Branch() {
		a.view = ""
	} else {
		a.view = in.Name
	}
	a.mu.Unlock()
	go func() {
		a.index()
		a.events.publish("branch", map[string]string{"branch": in.Name})
	}()
	web.JSON(w, map[string]string{"branch": in.Name})
}

func (a *App) file(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	src, ok := a.source(q.Get("path"), q.Get("sha"))
	if !ok {
		web.Error(w, http.StatusNotFound, "That file is not there.")
		return
	}
	web.JSON(w, map[string]string{"content": string(src)})
}

// working reports uncommitted changes. This version reviews commits only.
func (a *App) working(w http.ResponseWriter, r *http.Request) {
	web.JSON(w, map[string]any{"files": []any{}, "changes": []any{}, "findings": 0})
}

// modelUsage sums what the model has cost across reviewed commits, so a person can see what reviews take on this
// machine: time per commit, how fast it writes, and how often a reply came from the cache.
func (a *App) modelUsage() map[string]any {
	var commits, calls, cached, promptTokens, replyTokens int
	var seconds, decode float64
	version := review.PromptVersion
	for _, raw := range a.store.AllStats() {
		var s review.Stats
		if json.Unmarshal(raw, &s) != nil {
			continue
		}
		commits++
		calls += s.Calls
		cached += s.Cached
		promptTokens += s.PromptTokens
		replyTokens += s.ReplyTokens
		seconds += s.ModelSeconds
		decode += s.DecodeSeconds
	}
	out := map[string]any{"commits": commits, "calls": calls, "cached": cached, "promptVersion": version}
	if commits > 0 {
		out["secondsPerCommit"] = seconds / float64(commits)
	}
	if calls > 0 {
		out["promptTokensPerCall"] = promptTokens / calls
	}
	if decode > 0 {
		out["tokensPerSecond"] = float64(replyTokens) / decode
	}
	return out
}
