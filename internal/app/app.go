// Package app wires the parts together: repository, settings, model and pages.
package app

import (
	"context"
	"net/http"
	"sync"
	"time"

	"codist/internal/config"
	"codist/internal/git"
	"codist/internal/java"
	"codist/internal/model"
	"codist/internal/store"
	"codist/internal/web"
)

// App is one running Codist for one repository.
type App struct {
	cfg   config.Config
	repo  *git.Repo
	model *model.Client
	web   *web.Server
	store *store.Store

	mu        sync.RWMutex
	modelOK   bool
	modelNote string

	queue *queue

	ixMu   sync.Mutex
	ix     *java.Index
	ixHead string
}

// New starts everything that runs in the background and registers the pages.
func New(ctx context.Context, cfg config.Config, repo *git.Repo) (*App, error) {
	a := &App{cfg: cfg, repo: repo, model: model.New(cfg.ModelURL, cfg.Model, cfg.ModelCtx), queue: newQueue()}
	st, err := store.Open(cfg.DataDir, base(repo.Root))
	if err != nil {
		return nil, err
	}
	a.store = st
	srv, err := web.New(a.shell)
	if err != nil {
		return nil, err
	}
	a.web = srv
	a.routes()
	ok, note := a.model.Status(ctx) // know before the first review whether the model can be used
	a.modelOK, a.modelNote = ok, note
	go a.watchModel(ctx)
	go a.index() // warm up, so the first page is quick
	go a.work(ctx)
	go a.watch(ctx)
	return a, nil
}

// Handler serves the pages.
func (a *App) Handler() http.Handler { return a.web }

// Close stops background work.
func (a *App) Close() {
	if a.store != nil {
		_ = a.store.Close()
	}
}

func (a *App) shell() web.Shell {
	a.mu.RLock()
	defer a.mu.RUnlock()
	head := a.repo.Head()
	if len(head) > 7 {
		head = head[:7]
	}
	s := web.Shell{Repo: base(a.repo.Root), Branch: a.repo.Branch(), Head: head, Model: a.cfg.Model, ModelOK: a.modelOK, ModelNote: a.modelNote}
	if a.cfg.ModelOff {
		s.Model, s.ModelOK, s.ModelNote = "", true, "model review is off"
	}
	return s
}

// watchModel checks the model server now and every 30 seconds, so the page says when it goes away.
func (a *App) watchModel(ctx context.Context) {
	for {
		ok, note := a.model.Status(ctx)
		a.mu.Lock()
		a.modelOK, a.modelNote = ok, note
		a.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}

func base(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}

func (a *App) routes() {
	a.reviewRoutes()
	a.repoRoutes()
	a.graphRoutes()
	a.incidentRoutes()
}
