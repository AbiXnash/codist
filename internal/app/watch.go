package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/AbiXnash/codist-review"
	"github.com/AbiXnash/codist-review/risk"
)

// Reviews run in two lanes. Code checks take milliseconds, so they have their own lane and never
// wait behind the model. The model lane works through every commit, newest first, in the
// background; new commits and reviews a person asks for go to the front.

type job struct {
	sha string
	opt review.Options
}

// queue holds commits waiting in one lane, worked one at a time.
type queue struct {
	mu      sync.Mutex
	jobs    []job
	queued  map[string]bool
	wake    chan struct{}
	current string
}

func newQueue() *queue { return &queue{queued: map[string]bool{}, wake: make(chan struct{}, 1)} }

// push adds a job; first puts it at the front. A job already waiting moves to the front when
// pushed again with first.
func (q *queue) push(j job, first bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.queued[j.sha] {
		if !first || q.current == j.sha {
			return
		}
		for i, x := range q.jobs {
			if x.sha == j.sha {
				q.jobs = append(q.jobs[:i], q.jobs[i+1:]...)
				break
			}
		}
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

// waiting says whether a commit is queued or being worked on in this lane.
func (q *queue) waiting(sha string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.queued[sha]
}

// running says whether this lane is working on the commit right now.
func (q *queue) running(sha string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.current == sha
}

// sortBy orders the waiting jobs by score, highest first, keeping the order of equal scores.
func (q *queue) sortBy(score func(sha string) float64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	sort.SliceStable(q.jobs, func(i, j int) bool { return score(q.jobs[i].sha) > score(q.jobs[j].sha) })
}

// length is how many jobs are waiting.
func (q *queue) length() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.jobs)
}

// lockSHA keeps the two lanes from writing the same commit's results at once.
func (a *App) lockSHA(sha string) func() {
	m, _ := a.shaLocks.LoadOrStore(sha, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// work runs one lane. The model lane waits while the model server is away instead of failing
// every commit in the backlog.
func (a *App) work(ctx context.Context, q *queue, model bool) {
	for {
		if model && !a.modelReady() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(10 * time.Second):
				continue
			}
		}
		j, ok := q.pop()
		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-q.wake:
				continue
			}
		}
		start := time.Now()
		if model {
			a.events.publish("review", map[string]any{"sha": j.sha, "status": "running"})
		}
		var err error
		if model {
			unlock := a.lockSHA(j.sha)
			err = a.reviewer(model).Review(ctx, j.sha, j.opt)
			unlock()
		} else if m, _ := a.shaLocks.LoadOrStore(j.sha, &sync.Mutex{}); m.(*sync.Mutex).TryLock() {
			err = a.reviewer(model).Review(ctx, j.sha, j.opt)
			m.(*sync.Mutex).Unlock()
		} // otherwise the model is reviewing it, which runs the checks too: the fast lane moves on
		q.done(j.sha)
		status := "done"
		if err != nil {
			status = "failed"
			slog.Warn("review failed", "sha", short(j.sha), "err", err)
		} else {
			slog.Info("reviewed", "sha", short(j.sha), "model", model, "took", time.Since(start).Round(time.Millisecond), "left", q.length())
		}
		a.events.publish("review", map[string]any{"sha": j.sha, "status": a.statusOf(j.sha, status), "counts": a.store.Counts()[j.sha]})
	}
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// statusOf is a commit's state after one lane finished: still queued if the other lane has it.
func (a *App) statusOf(sha, finished string) string {
	if a.models.running(sha) {
		return "running"
	}
	if a.models.waiting(sha) || a.checks.waiting(sha) {
		return "queued"
	}
	return finished
}

func (a *App) modelReady() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.modelOK && !a.cfg.ModelOff
}

// backlog queues what each lane still has to do: code checks for every commit never reviewed, and
// the model for the recent commits it has not read yet. It runs at start, so a restart picks up where it stopped.
func (a *App) backlog() {
	reviews := a.store.Reviews()
	log, _ := a.repo.LogRef("HEAD", 1_000_000)
	for i, c := range log { // newest first
		rv, seen := reviews[c.SHA]
		// A review still "running" was cut off when Codist stopped, so it is done again.
		finished := seen && rv.Status != "running"
		if !finished {
			a.checks.push(job{sha: c.SHA, opt: review.Options{RulesOnly: true}}, false)
		}
		if !a.cfg.ModelOff && i < a.historyLimit() && (!finished || rv.Model == "") {
			a.models.push(job{sha: c.SHA}, false)
		}
	}
	slog.Info("backlog", "checks", a.checks.length(), "model", a.models.length())
}

func (a *App) historyLimit() int {
	if a.cfg.History > 0 {
		return a.cfg.History
	}
	return 1_000_000
}

// watch queues the backlog, then each new commit at the front of both lanes.
func (a *App) watch(ctx context.Context) {
	a.baseline()
	a.backlog()
	go a.learnRisk()
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
		fresh := []string{}
		for _, c := range recent {
			if c.SHA == last {
				break
			}
			if _, ok := a.store.GetReview(c.SHA); !ok {
				fresh = append(fresh, c.SHA)
			}
		}
		for i := len(fresh) - 1; i >= 0; i-- { // pushing to the front in reverse keeps the newest first
			a.checks.push(job{sha: fresh[i], opt: review.Options{RulesOnly: true}}, true)
			if !a.cfg.ModelOff {
				a.models.push(job{sha: fresh[i]}, true)
			}
			if a.runner != nil {
				a.runs.push(job{sha: fresh[i]}, true)
			}
		}
		last = head
		go a.learnRisk()
		go func() {
			a.index() // rebuild the graph for the new code before telling the pages
			a.events.publish("graph", map[string]string{"head": head})
		}()
		a.events.publish("commits", map[string]any{"head": head, "new": fresh})
	}
}

func (a *App) reviewer(model bool) *review.Reviewer {
	rules, _ := os.ReadFile(filepath.Join(a.repo.Root, ".codist", "rules.md")) // the team's rules, read fresh each review
	r := &review.Reviewer{Repo: a.repo, Store: a.store, MaxFiles: a.cfg.MaxFiles, Ignored: a.cfg.Ignored, Learn: true,
		Contracts: true, Rules: string(rules)}
	if model && a.modelReady() {
		r.Model = a.model
	}
	return r
}

// baseline checks the whole code once, the first time Codist runs on a repository, so problems
// already in the code show as issues and not only those in commits it reviews. Each is credited to
// the commit that last changed its line.
func (a *App) baseline() {
	if _, done := a.store.GetReview(review.BaselineSHA); done {
		return
	}
	start := time.Now()
	ix := a.index()
	files := map[string][]byte{}
	for _, t := range ix.Types {
		if _, ok := files[t.File]; !ok {
			if src, ok := a.source(t.File, ""); ok {
				files[t.File] = src
			}
		}
	}
	n, err := a.reviewer(false).Baseline(files)
	if err != nil {
		slog.Warn("baseline failed", "err", err)
		return
	}
	slog.Info("baseline", "files", len(files), "issues", n, "took", time.Since(start).Round(time.Millisecond))
}

// learnRisk trains the risk model on the repository's history and the issues Codist has found, at most every ten
// minutes. When the model is good enough, the model lane reads the riskiest recent commits first.
func (a *App) learnRisk() {
	a.mu.Lock()
	if time.Since(a.riskAt) < 10*time.Minute && a.risk != nil {
		a.mu.Unlock()
		return
	}
	a.riskAt = time.Now()
	a.mu.Unlock()
	known := map[string]bool{}
	for _, is := range a.store.AllIssues() {
		if is.FirstSHA != "" && is.FirstSHA != review.BaselineSHA {
			known[is.FirstSHA] = true
		}
	}
	m := risk.Train(a.repo, risk.Options{Known: known})
	a.mu.Lock()
	a.risk = m
	a.mu.Unlock()
	a.events.publish("status", map[string]any{"riskModel": m.Ready})
	if m.Ready {
		a.models.sortBy(func(sha string) float64 { return m.Scores[sha] })
		slog.Info("risk model", "auc", m.AUC, "commits", m.Commits, "bug-bringing", m.Positives, "signals", m.Signals)
	} else {
		slog.Info("risk model not used", "reason", m.Reason)
	}
}

// riskLevel is a commit's risk as a word, or "" while there is no usable model.
func (a *App) riskLevel(sha string) string {
	a.mu.RLock()
	m := a.risk
	a.mu.RUnlock()
	return m.Level(sha)
}

// riskModel describes the risk model for the status page.
func (a *App) riskModel() any {
	a.mu.RLock()
	m := a.risk
	a.mu.RUnlock()
	if m == nil {
		return map[string]any{"ready": false, "reason": "Learning from the history..."}
	}
	return m
}
