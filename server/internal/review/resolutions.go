package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Resolution records that an issue was closed by a person or by a committed fix, for findings whose
// code cannot prove it (the model pointed at a line the fix did not change). A later review that
// reports the same issue again overrides it.
type Resolution struct {
	By     string `json:"by"`
	At     int64  `json:"at"`
	Commit string `json:"commit,omitempty"`
	Manual bool   `json:"manual,omitempty"`
	Branch string `json:"branch,omitempty"` // where it was marked by hand; empty for older records
	// The same fix cherry-picked onto other branches.
	Also []string `json:"also,omitempty"`
}

// Resolutions is stored at <data-dir>/<repo>/resolutions.json.
type Resolutions struct {
	path  string
	mu    sync.Mutex
	items map[string]Resolution
	edits int
}

func NewResolutions(dataDir, repo string) *Resolutions {
	r := &Resolutions{path: filepath.Join(dataDir, repo, "resolutions.json"), items: map[string]Resolution{}}
	if b, err := os.ReadFile(r.path); err == nil {
		_ = json.Unmarshal(b, &r.items)
	}
	return r
}

func (r *Resolutions) Get(id string) (Resolution, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.items[id]
	return v, ok
}

func (r *Resolutions) Set(ids []string, res Resolution) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if res.At == 0 {
		res.At = time.Now().UnixMilli()
	}
	for _, id := range ids {
		r.items[id] = res
	}
	return r.persist()
}

// AlsoFixedBy records that a fix commit now also exists on another branch as a new commit.
func (r *Resolutions) AlsoFixedBy(commit string, others []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, res := range r.items {
		if res.Commit == commit {
			res.Also = append(res.Also, others...)
			r.items[id] = res
		}
	}
	return r.persist()
}

// Rekey follows commits that got new ids, as rewording does.
func (r *Resolutions) Rekey(mapping map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, res := range r.items {
		if to, ok := mapping[res.Commit]; ok {
			res.Commit = to
		}
		for i, c := range res.Also {
			if to, ok := mapping[c]; ok {
				res.Also[i] = to
			}
		}
		r.items[id] = res
	}
	return r.persist()
}

func (r *Resolutions) Remove(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.items, id)
	return r.persist()
}

// Version changes with every edit.
func (r *Resolutions) Version() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.edits
}

func (r *Resolutions) persist() error {
	r.edits++
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(r.items, "", "  ")
	return os.WriteFile(r.path, b, 0o644)
}

// Work is where an open issue stands in the team's workflow: who has it and how far along it is.
type Work struct {
	Stage    string `json:"stage"` // in-progress, in-review; empty means nobody started it
	Assignee string `json:"assignee,omitempty"`
	At       int64  `json:"at"`
}

// Workflow is stored at <data-dir>/<repo>/workflow.json.
type Workflow struct {
	path  string
	mu    sync.Mutex
	items map[string]Work
	edits int
}

func NewWorkflow(dataDir, repo string) *Workflow {
	w := &Workflow{path: filepath.Join(dataDir, repo, "workflow.json"), items: map[string]Work{}}
	if b, err := os.ReadFile(w.path); err == nil {
		_ = json.Unmarshal(b, &w.items)
	}
	return w
}

func (w *Workflow) Get(id string) (Work, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	v, ok := w.items[id]
	return v, ok
}

// Set records the stage of some issues; an empty stage and assignee clears them.
func (w *Workflow) Set(ids []string, stage, assignee string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, id := range ids {
		if stage == "" && assignee == "" {
			delete(w.items, id)
		} else {
			w.items[id] = Work{Stage: stage, Assignee: assignee, At: time.Now().UnixMilli()}
		}
	}
	w.edits++
	if err := os.MkdirAll(filepath.Dir(w.path), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(w.items, "", "  ")
	return os.WriteFile(w.path, b, 0o644)
}

func (w *Workflow) Version() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.edits
}
