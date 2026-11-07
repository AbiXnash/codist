// Package app wires the parts together and serves the API the interface reads.
package app

import (
	"context"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/AbiXnash/codist-behaviour"
	"github.com/AbiXnash/codist-git"
	"github.com/AbiXnash/codist-java"
	"github.com/AbiXnash/codist-model"
	"github.com/AbiXnash/codist-review/risk"
	"github.com/AbiXnash/codist-review/store"
	"github.com/AbiXnash/codist-ui"
	"github.com/AbiXnash/codist/internal/config"
	"github.com/AbiXnash/codist/internal/web"
)

// App is one running Codist for one repository.
type App struct {
	cfg      config.Config
	repo     *git.Repo
	model    *model.Client
	store    *store.Store
	mux      *http.ServeMux
	checks   *queue // code checks: fast, never wait for the model
	models   *queue // the model, one commit at a time
	runs     *queue // running the changed methods before and after a commit, when turned on
	runner   *behaviour.Runner
	risk     *risk.Model // which commits tend to bring bugs here, learned from this repository's history
	riskAt   time.Time
	events   *hub
	shaLocks sync.Map
	ctx      context.Context

	mu        sync.RWMutex
	modelOK   bool
	modelNote string
	view      string // a branch being looked at without checking it out, or "" for the checked-out one

	ixMu  sync.Mutex
	ix    *java.Index
	ixSHA string
}

// New starts everything that runs in the background and registers the API.
func New(ctx context.Context, cfg config.Config, repo *git.Repo) (*App, error) {
	st, err := store.OpenRepo(cfg.DataDir, repo.Root)
	if err != nil {
		return nil, err
	}
	a := &App{cfg: cfg, repo: repo, model: model.New(cfg.ModelURL, cfg.Model, cfg.ModelCtx), store: st,
		mux: http.NewServeMux(), checks: newQueue(), models: newQueue(), runs: newQueue(), events: newHub(), ctx: ctx}
	if cfg.Behaviour {
		a.runner = &behaviour.Runner{Repo: repo, Work: filepath.Join(cfg.DataDir, "behaviour", store.Name(repo.Root)), Ignored: cfg.Ignored}
		go a.runBehaviour(ctx)
	}
	a.modelOK, a.modelNote = a.model.Status(ctx)
	a.routes()
	go a.watchModel(ctx)
	go a.index() // warm up, so the first graph is quick
	go a.work(ctx, a.checks, false)
	go a.work(ctx, a.models, true)
	go a.watch(ctx)
	return a, nil
}

// Handler serves the API and the interface.
func (a *App) Handler() http.Handler { return gzipped(a.mux) }

// Close stops background work.
func (a *App) Close() { _ = a.store.Close() }

func (a *App) routes() {
	h := a.mux.HandleFunc
	h("GET /api/status", a.status)
	h("GET /api/features", a.features)
	h("GET /api/events", a.events.serve)
	h("GET /api/refs", a.refs)
	h("POST /api/refs/follow", a.follow)
	h("GET /api/file", a.file)
	h("GET /api/working", a.working)
	h("GET /api/graph", a.graph)
	h("GET /api/graph/calls", a.calls)
	h("GET /api/commits", a.commits)
	h("GET /api/commits/{sha}", a.commit)
	h("GET /api/commits/{sha}/impact", a.impact)
	h("POST /api/commits/{sha}/review", a.requestReview)
	h("POST /api/commits/{sha}/behaviour", a.requestBehaviour)
	h("GET /api/issues", a.issues)
	h("POST /api/issues/{id}/resolve", a.resolveIssue)
	h("DELETE /api/issues/{id}/resolve", a.reopenIssue)
	h("POST /api/issues/{id}/feedback", a.feedback)
	h("GET /api/repo", a.repoReport)
	h("GET /api/incidents", a.incidents)
	h("POST /api/incidents", a.createIncident)
	h("GET /api/incidents/{id}", a.incident)
	h("PUT /api/incidents/{id}", a.updateIncident)
	h("DELETE /api/incidents/{id}", a.deleteIncident)
	a.mux.Handle("/api/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		web.Error(w, http.StatusNotFound, "This part of Codist is not in this version.")
	}))
	a.mux.Handle("/", ui.Handler())
}

// watchModel checks the model server now and every 30 seconds, so the interface says when it goes away.
func (a *App) watchModel(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
		ok, note := a.model.Status(ctx)
		a.mu.Lock()
		a.modelOK, a.modelNote = ok, note
		a.mu.Unlock()
	}
}

// viewRef is what the interface shows: the branch being viewed, or HEAD.
func (a *App) viewRef() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.view != "" {
		return a.view
	}
	return "HEAD"
}

func (a *App) viewing() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.view != ""
}

func base(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}

func ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}
