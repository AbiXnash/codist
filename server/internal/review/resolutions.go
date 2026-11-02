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

// Rekey follows commits that got new ids, as rewording does.
func (r *Resolutions) Rekey(mapping map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, res := range r.items {
		if to, ok := mapping[res.Commit]; ok {
			res.Commit = to
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
