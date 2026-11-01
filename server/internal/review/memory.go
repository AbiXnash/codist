package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// MemoryItem is something a developer taught Codist about this repository: why a suggestion was
// rejected, a preference, a convention. Items go into the prompts of later reviews and fixes.
type MemoryItem struct {
	ID      string `json:"id" yaml:"id"`
	At      int64  `json:"at" yaml:"at"`
	By      string `json:"by" yaml:"by"`
	Kind    string `json:"kind" yaml:"kind"` // rejected-fix, preference
	Text    string `json:"text" yaml:"text"`
	Title   string `json:"title,omitempty" yaml:"title,omitempty"` // the finding it was about
	Ref     string `json:"ref,omitempty" yaml:"ref,omitempty"`
	Path    string `json:"path,omitempty" yaml:"path,omitempty"`
	Snippet string `json:"snippet,omitempty" yaml:"snippet,omitempty"`
	Team    bool   `json:"team" yaml:"-"`
}

// Memory stores items personally (<data-dir>/<repo>/memory.json) or for the team, in
// <repo>/.codist/memory.yaml, which is committed with the code.
type Memory struct {
	personal, team string
	mu             sync.Mutex
}

func NewMemory(dataDir, repo, root string) *Memory {
	return &Memory{personal: filepath.Join(dataDir, repo, "memory.json"), team: filepath.Join(root, ".codist", "memory.yaml")}
}

func (m *Memory) load() (mine, team []MemoryItem) {
	if b, err := os.ReadFile(m.personal); err == nil {
		_ = json.Unmarshal(b, &mine)
	}
	if b, err := os.ReadFile(m.team); err == nil {
		_ = yaml.Unmarshal(b, &team)
	}
	for i := range team {
		team[i].Team = true
	}
	return mine, team
}

func (m *Memory) save(mine, team []MemoryItem) error {
	if err := os.MkdirAll(filepath.Dir(m.personal), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(mine, "", "  ")
	if err := os.WriteFile(m.personal, b, 0o644); err != nil {
		return err
	}
	if len(team) == 0 {
		if err := os.Remove(m.team); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(m.team), 0o755); err != nil {
		return err
	}
	y, _ := yaml.Marshal(team)
	return os.WriteFile(m.team, append([]byte("# What the team taught Codist about this repository. Commit this file to share it.\n"), y...), 0o644)
}

// All lists everything, newest first.
func (m *Memory) All() []MemoryItem {
	m.mu.Lock()
	defer m.mu.Unlock()
	mine, team := m.load()
	out := append(append([]MemoryItem{}, mine...), team...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].At > out[j].At })
	return out
}

// Add stores a new item.
func (m *Memory) Add(it MemoryItem) (MemoryItem, error) {
	it.Text = strings.TrimSpace(it.Text)
	if it.Text == "" {
		return it, errors.New("say what Codist should remember")
	}
	if len(it.Text) > 1000 {
		return it, errors.New("keep it to a few sentences")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	it.ID, it.At = newID(), time.Now().UnixMilli()
	if it.Kind == "" {
		it.Kind = "preference"
	}
	mine, team := m.load()
	if it.Team {
		team = append(team, it)
	} else {
		mine = append(mine, it)
	}
	return it, m.save(mine, team)
}

func without(items []MemoryItem, id string) ([]MemoryItem, *MemoryItem) {
	for i := range items {
		if items[i].ID == id {
			found := items[i]
			return append(items[:i:i], items[i+1:]...), &found
		}
	}
	return items, nil
}

// Remove deletes an item.
func (m *Memory) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	mine, team := m.load()
	mine, a := without(mine, id)
	team, b := without(team, id)
	if a == nil && b == nil {
		return errors.New("no such item")
	}
	return m.save(mine, team)
}

// Share moves an item to the team file, or back to personal.
func (m *Memory) Share(id string, shared bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	mine, team := m.load()
	mine, a := without(mine, id)
	team, b := without(team, id)
	it := a
	if it == nil {
		it = b
	}
	if it == nil {
		return errors.New("no such item")
	}
	it.Team = shared
	if shared {
		team = append(team, *it)
	} else {
		mine = append(mine, *it)
	}
	return m.save(mine, team)
}

// ForPrompt renders what applies to this file and, if given, this kind of finding. Items naming
// another file or rule stay out; general ones always apply. Most recent first, at most 12.
func (m *Memory) ForPrompt(path, ref string) string {
	if m == nil {
		return ""
	}
	var b strings.Builder
	n := 0
	for _, it := range m.All() {
		if n == 12 {
			break
		}
		if ref != "" && it.Ref != "" && it.Ref != ref {
			continue
		}
		if ref == "" && it.Path != "" && it.Path != path && it.Ref == "" {
			continue
		}
		about := ""
		if it.Title != "" {
			about = fmt.Sprintf(" (about %q", it.Title)
			if it.Path != "" {
				about += " in " + filepath.Base(it.Path)
			}
			about += ")"
		}
		fmt.Fprintf(&b, "- %s%s\n", it.Text, about)
		n++
	}
	return b.String()
}
