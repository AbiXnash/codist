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
	"codist/internal/web"
)

// App is one running Codist for one repository.
type App struct {
	cfg   config.Config
	repo  *git.Repo
	model *model.Client
	web   *web.Server

	mu        sync.RWMutex
	modelOK   bool
	modelNote string

	ixMu   sync.Mutex
	ix     *java.Index
	ixHead string
}

// New starts everything that runs in the background and registers the pages.
func New(ctx context.Context, cfg config.Config, repo *git.Repo) (*App, error) {
	a := &App{cfg: cfg, repo: repo, model: model.New(cfg.ModelURL, cfg.Model, cfg.ModelCtx)}
	srv, err := web.New(a.shell)
	if err != nil {
		return nil, err
	}
	a.web = srv
	a.routes()
	go a.watchModel(ctx)
	go a.index() // warm up, so the first page is quick
	return a, nil
}

// Handler serves the pages.
func (a *App) Handler() http.Handler { return a.web }

// Close stops background work.
func (a *App) Close() {}

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
	a.web.Handle("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		a.web.Render(w, r, "home", "review", "Review", nil)
	})
}
