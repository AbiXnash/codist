// Package watch polls the repository. New commits trigger a re-index and a review;
// edits in the working tree (local mode) only re-index, so the graph follows you as you type.
package watch

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"codist/internal/config"
	"codist/internal/events"
	"codist/internal/gitx"
	"codist/internal/graph"
	"codist/internal/review"
)

type Watcher struct {
	cfg     config.Config
	repo    *gitx.Repo
	graph   *graph.Service
	reviews *review.Service
	hub     *events.Hub

	mu          sync.Mutex
	lastHead    string
	fingerprint string
	lastFetch   time.Time
	poke        chan struct{}
}

func New(cfg config.Config, repo *gitx.Repo, g *graph.Service, reviews *review.Service, hub *events.Hub) *Watcher {
	return &Watcher{cfg: cfg, repo: repo, graph: g, reviews: reviews, hub: hub, poke: make(chan struct{}, 1)}
}

// Start indexes once, catches up on commits made while Codist was stopped, then polls.
func (w *Watcher) Start() {
	w.repo.Fetch()
	w.lastFetch = time.Now()
	w.lastHead = w.repo.Head()
	if !w.repo.Remote {
		w.fingerprint = w.repo.Fingerprint()
	}
	w.graph.Reindex()

	if w.cfg.AutoReview && w.lastHead != "" {
		seen := w.loadSeen()
		var missed []gitx.Commit
		if seen != "" && seen != w.lastHead {
			missed = w.repo.CommitsSince(seen, w.lastHead, 10)
		} else if !w.reviews.Reviewed(w.lastHead) {
			missed = w.repo.CommitsSince("", w.lastHead, 1) // first run: just the latest commit
		}
		for _, c := range missed {
			if !w.reviews.Reviewed(c.SHA) {
				slog.Info("reviewing commit made while Codist was stopped", "sha", c.ShortSHA)
				w.reviews.Enqueue(c.SHA)
			}
		}
		// Reviews cut off by a restart are finished now.
		for _, c := range w.repo.Log(20) {
			if w.reviews.Interrupted(c.SHA) {
				slog.Info("resuming interrupted review", "sha", c.ShortSHA)
				w.reviews.Enqueue(c.SHA)
			}
		}
		w.saveSeen(w.lastHead)
	}

	go func() {
		t := time.NewTicker(w.cfg.Poll)
		defer t.Stop()
		for {
			select {
			case <-t.C:
			case <-w.poke:
				w.lastFetch = time.Time{}
			}
			w.tick()
		}
	}()
}

// Poke fetches and checks right away; used by the GitHub webhook.
func (w *Watcher) Poke() {
	select {
	case w.poke <- struct{}{}:
	default:
	}
}

func (w *Watcher) tick() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.repo.Remote && time.Since(w.lastFetch) > w.cfg.FetchInterval {
		w.repo.Fetch()
		w.lastFetch = time.Now()
	}

	reindexed := false
	if head := w.repo.Head(); head != "" && head != w.lastHead {
		fresh := w.repo.CommitsSince(w.lastHead, head, 10)
		slog.Info("new commits", "count", len(fresh), "branch", w.repo.Tracking())
		w.lastHead = head
		w.saveSeen(head)
		w.graph.Reindex()
		reindexed = true
		shas := make([]string, 0, len(fresh))
		for _, c := range fresh {
			shas = append(shas, c.SHA)
		}
		w.hub.Publish("commits", map[string]any{"head": head, "new": shas})
		if w.cfg.AutoReview {
			for _, sha := range shas {
				w.reviews.Enqueue(sha)
			}
		}
	}

	if !w.repo.Remote {
		if fp := w.repo.Fingerprint(); fp != w.fingerprint {
			w.fingerprint = fp
			if !reindexed {
				w.graph.Reindex()
			}
		}
	}
}

func (w *Watcher) seenFile() string {
	return filepath.Join(w.cfg.DataDir, w.repo.Name(), "last-seen-head")
}

func (w *Watcher) loadSeen() string {
	b, _ := os.ReadFile(w.seenFile())
	return strings.TrimSpace(string(b))
}

func (w *Watcher) saveSeen(sha string) {
	_ = os.MkdirAll(filepath.Dir(w.seenFile()), 0o755)
	_ = os.WriteFile(w.seenFile(), []byte(sha+"\n"), 0o644)
}
