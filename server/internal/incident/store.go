package incident

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Store keeps incidents as JSON files at <data-dir>/<repo>/incidents/<id>.json.
type Store struct {
	dir string
	mu  sync.Mutex
}

func NewStore(dataDir, repo string) *Store {
	s := &Store{dir: filepath.Join(dataDir, repo, "incidents")}
	_ = os.MkdirAll(s.dir, 0o755)
	return s
}

func (s *Store) Save(in *Incident) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.MarshalIndent(in, "", "  ")
	tmp := filepath.Join(s.dir, in.ID+".json.tmp")
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, filepath.Join(s.dir, in.ID+".json"))
	}
}

func (s *Store) Get(id string) (*Incident, bool) {
	if strings.ContainsAny(id, "/\\.") {
		return nil, false
	}
	b, err := os.ReadFile(filepath.Join(s.dir, id+".json"))
	if err != nil {
		return nil, false
	}
	var in Incident
	if json.Unmarshal(b, &in) != nil {
		return nil, false
	}
	return &in, true
}

func (s *Store) Delete(id string) {
	if !strings.ContainsAny(id, "/\\.") {
		_ = os.Remove(filepath.Join(s.dir, id+".json"))
	}
}

// All returns every incident, newest first.
func (s *Store) All() []*Incident {
	entries, _ := os.ReadDir(s.dir)
	var out []*Incident
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			if in, ok := s.Get(strings.TrimSuffix(e.Name(), ".json")); ok {
				out = append(out, in)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out
}
