package review

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Store keeps reviews as JSON files at <data-dir>/<repo>/reviews/<sha>.json, cached in memory.
type Store struct {
	dir   string
	mu    sync.RWMutex
	cache map[string]Review
}

func NewStore(dataDir, repo string) (*Store, error) {
	s := &Store{dir: filepath.Join(dataDir, repo, "reviews"), cache: map[string]Review{}}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return nil, err
	}
	entries, _ := os.ReadDir(s.dir)
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var r Review
		if err := json.Unmarshal(b, &r); err != nil {
			slog.Warn("ignoring unreadable review", "file", e.Name())
			continue
		}
		if r.Status == StatusQueued || r.Status == StatusRunning {
			r.Status, r.Error = StatusFailed, "Interrupted: Codist stopped before the review finished"
		}
		s.cache[r.SHA] = r
	}
	slog.Info("loaded reviews", "count", len(s.cache), "dir", s.dir)
	return s, nil
}

func (s *Store) Get(sha string) (Review, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.cache[sha]
	return r, ok
}

func (s *Store) Save(r Review) {
	s.mu.Lock()
	s.cache[r.SHA] = r
	s.mu.Unlock()
	b, _ := json.MarshalIndent(r, "", "  ")
	tmp := filepath.Join(s.dir, r.SHA+".json.tmp")
	if err := os.WriteFile(tmp, b, 0o644); err == nil {
		_ = os.Rename(tmp, filepath.Join(s.dir, r.SHA+".json"))
	}
}

// All returns finished and running reviews, newest first.
func (s *Store) All() []Review {
	s.mu.RLock()
	out := make([]Review, 0, len(s.cache))
	for _, r := range s.cache {
		out = append(out, r)
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		return max(out[i].FinishedAt, out[i].StartedAt) > max(out[j].FinishedAt, out[j].StartedAt)
	})
	return out
}
