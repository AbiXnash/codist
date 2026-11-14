// Package app wires the parts together and serves the API the interface reads.
package app

import (
	"cmp"
	"context"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/abxengg/codist-behaviour"
	"github.com/abxengg/codist-fix"
	"github.com/abxengg/codist-git"
	"github.com/abxengg/codist-java"
	"github.com/abxengg/codist-model"
	"github.com/abxengg/codist-review/activity"
	"github.com/abxengg/codist-review/risk"
	"github.com/abxengg/codist-review/store"
	"github.com/abxengg/codist-ui"
	"github.com/abxengg/codist/internal/config"
	"github.com/abxengg/codist/internal/web"
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
	tries    map[string]int // how often the model failed to read a commit in this run
	ctx      context.Context

	mu        sync.RWMutex
	modelOK   bool
	modelNote string
	view      string // a branch being looked at without checking it out, or "" for the checked-out one
	fixer     *fix.Fixer

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
		mux: http.NewServeMux(), checks: newQueue("Code checks"), models: newQueue("AI model"), runs: newQueue("Behaviour runs"), events: newHub(), ctx: ctx, tries: map[string]int{},
		fixer: &fix.Fixer{Repo: repo, Store: st, Model: model.New(cfg.ModelURL, cfg.Model, cfg.ModelCtx), Notes: filepath.Join(cfg.DataDir, store.Name(repo.Root)+"-fix-notes.json")}}
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
func (a *App) Handler() http.Handler { return gzipped(a.guard(a.mux)) }

// guard refuses API requests that reach Codist through another website. There is no login, so only pages served
// from this machine may call it: a foreign Host is a name pointed at this machine to read answers (DNS rebinding),
// and a foreign Origin or a cross-site fetch is another page's script.
func (a *App) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if u, err := url.Parse(origin); err == nil && origin != "" {
			origin = u.Host
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && (!a.local(r.Host) || r.Header.Get("Origin") != "" && !a.local(origin) ||
			r.Header.Get("Sec-Fetch-Site") == "cross-site") {
			web.Error(w, http.StatusForbidden, "Codist only answers its own pages. Set CODIST_HOSTS to allow another host name.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// local says a host, with or without a port, is this machine or a name the settings allow (CODIST_HOSTS).
func (a *App) local(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.ToLower(strings.Trim(host, "[]"))
	ip := net.ParseIP(host)
	return host == "localhost" || strings.HasSuffix(host, ".localhost") || ip != nil && ip.IsLoopback() || slices.Contains(a.cfg.Hosts, host)
}

// Close stops background work.
func (a *App) Close() { _ = a.store.Close() }

func (a *App) routes() {
	h := a.mux.HandleFunc
	activity.Routes(h, a.checks, a.models, a.runs) // the Activity page: lanes, model calls, live log
	a.fixer.Routes(h)                              // prepare, check, apply and discard fixes; remember rejections
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

// viewRef is what the interface shows: the branch being viewed, or HEAD. viewing says it is another branch.
func (a *App) viewRef() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return cmp.Or(a.view, "HEAD")
}

func (a *App) viewing() bool { return a.viewRef() != "HEAD" }

func ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}
