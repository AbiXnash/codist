package app

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"codist/internal/review"
)

// catchUp is how many recent unreviewed commits are checked at start. The model is slow on a
// laptop, so catching up uses code checks only; the newest commit still gets the model.
const catchUp = 30

type job struct {
	sha string
	opt review.Options
}

// queue holds commits waiting for review, one worker at a time: there is one model.
type queue struct {
	mu      sync.Mutex
	jobs    []job
	queued  map[string]bool
	wake    chan struct{}
	current string
}

func newQueue() *queue { return &queue{queued: map[string]bool{}, wake: make(chan struct{}, 1)} }

// push adds a job; first puts it at the front, for a review a person asked for.
func (q *queue) push(j job, first bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.queued[j.sha] {
		return
	}
	q.queued[j.sha] = true
	if first {
		q.jobs = append([]job{j}, q.jobs...)
	} else {
		q.jobs = append(q.jobs, j)
	}
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *queue) pop() (job, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.jobs) == 0 {
		return job{}, false
	}
	j := q.jobs[0]
	q.jobs = q.jobs[1:]
	q.current = j.sha
	return j, true
}

func (q *queue) done(sha string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.queued, sha)
	q.current = ""
}

// waiting says whether a commit is queued or being reviewed.
func (q *queue) waiting(sha string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.queued[sha]
}

func (a *App) work(ctx context.Context) {
	for {
		j, ok := a.queue.pop()
		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-a.queue.wake:
				continue
			}
		}
		start := time.Now()
		a.events.publish("review", map[string]any{"sha": j.sha, "status": "running"})
		status := "done"
		if err := a.reviewer().Review(ctx, j.sha, j.opt); err != nil {
			status = "failed"
			slog.Warn("review failed", "sha", j.sha[:7], "err", err)
		} else {
			slog.Info("reviewed", "sha", j.sha[:7], "rulesOnly", j.opt.RulesOnly, "took", time.Since(start).Round(time.Millisecond))
		}
		a.queue.done(j.sha)
		a.events.publish("review", map[string]any{"sha": j.sha, "status": status, "counts": a.store.Counts()[j.sha]})
	}
}

// watch queues recent unreviewed commits, then new ones as they arrive.
func (a *App) watch(ctx context.Context) {
	reviewed := a.store.Reviews()
	log, _ := a.repo.Log(catchUp)
	for i, c := range log {
		if _, ok := reviewed[c.SHA]; ok {
			continue
		}
		opt := review.Options{RulesOnly: true, Why: "Checked with code checks only while catching up. Review again for a model review."}
		if i == 0 {
			opt = review.Options{}
		}
		a.queue.push(job{sha: c.SHA, opt: opt}, false)
	}
	last := a.repo.Head()
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(a.cfg.PollMs) * time.Millisecond):
		}
		head := a.repo.Head()
		if head == "" || head == last {
			continue
		}
		recent, _ := a.repo.Log(50)
		var fresh []string
		for _, c := range recent {
			if c.SHA == last {
				break
			}
			if _, ok := a.store.GetReview(c.SHA); !ok {
				fresh = append(fresh, c.SHA)
			}
		}
		for _, sha := range fresh { // newest first
			a.queue.push(job{sha: sha}, false)
		}
		last = head
		go func() {
			a.index() // rebuild the graph for the new code before telling the pages
			a.events.publish("graph", map[string]string{"head": head})
		}()
		if fresh == nil {
			fresh = []string{}
		}
		a.events.publish("commits", map[string]any{"head": head, "new": fresh})
	}
}

func (a *App) reviewer() *review.Reviewer {
	r := &review.Reviewer{Repo: a.repo, Store: a.store, MaxFiles: a.cfg.MaxFiles, Ignored: a.cfg.Ignored}
	a.mu.RLock()
	ok := a.modelOK
	a.mu.RUnlock()
	if !a.cfg.ModelOff && ok {
		r.Model = a.model
	}
	return r
}
