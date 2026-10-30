package review

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// SignOff is the release checklist for one release target (the commit being shipped). It is
// stored on disk, so ticks survive reloads and changing the "changes since" range, and every open
// tab sees the same state. Items are keyed by their text, never by position.
type SignOff struct {
	To    string      `json:"to"`
	Items []CheckItem `json:"items"`
	Done  int         `json:"done"`
	Total int         `json:"total"`
}

type CheckItem struct {
	ID      string `json:"id"`
	Text    string `json:"text"`
	Done    bool   `json:"done"`
	By      string `json:"by,omitempty"`
	At      int64  `json:"at,omitempty"`
	Custom  bool   `json:"custom,omitempty"` // added by a person, not generated
	Current bool   `json:"current"`          // still generated for this release (or custom)
}

type SignOffs struct {
	dir string
	mu  sync.Mutex
}

func NewSignOffs(dataDir, repo string) *SignOffs {
	return &SignOffs{dir: filepath.Join(dataDir, repo, "signoffs")}
}

func itemID(text string) string {
	norm := strings.ToLower(strings.Join(strings.Fields(text), " "))
	h := sha1.Sum([]byte(norm))
	return hex.EncodeToString(h[:6])
}

var shaOnly = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

func (s *SignOffs) path(to string) (string, error) {
	if !shaOnly.MatchString(to) {
		return "", errors.New("release target must be a commit id")
	}
	return filepath.Join(s.dir, to+".json"), nil
}

func (s *SignOffs) load(to string) (map[string]CheckItem, []string) {
	items := map[string]CheckItem{}
	var order []string
	p, err := s.path(to)
	if err != nil {
		return items, order
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return items, order
	}
	var saved []CheckItem
	if json.Unmarshal(b, &saved) == nil {
		for _, it := range saved {
			items[it.ID] = it
			order = append(order, it.ID)
		}
	}
	return items, order
}

func (s *SignOffs) save(to string, items map[string]CheckItem, order []string) error {
	p, err := s.path(to)
	if err != nil {
		return err
	}
	out := make([]CheckItem, 0, len(order))
	for _, id := range order {
		if it, ok := items[id]; ok {
			it.Current = false // derived on read
			out = append(out, it)
		}
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return os.WriteFile(p, b, 0o644)
}

// For merges the generated checklist with the stored ticks for a release target. Ticked items that
// are no longer generated stay listed (marked not current) so a sign-off is never silently lost.
func (s *SignOffs) For(to string, generated []string) SignOff {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored, order := s.load(to)
	so := SignOff{To: to, Items: []CheckItem{}}
	seen := map[string]bool{}
	for _, text := range generated {
		id := itemID(text)
		it := stored[id]
		it.ID, it.Text, it.Current = id, text, true
		so.Items = append(so.Items, it)
		seen[id] = true
	}
	for _, id := range order {
		it := stored[id]
		if seen[id] || (!it.Custom && !it.Done) {
			continue
		}
		it.Current = it.Custom
		so.Items = append(so.Items, it)
	}
	for _, it := range so.Items {
		if it.Current {
			so.Total++
			if it.Done {
				so.Done++
			}
		}
	}
	return so
}

// Set ticks or unticks an item (adding it when it is new, e.g. a custom item).
func (s *SignOffs) Set(to, text string, done, custom bool, by string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("item text is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items, order := s.load(to)
	id := itemID(text)
	it, ok := items[id]
	if !ok {
		order = append(order, id)
		it = CheckItem{ID: id, Text: text, Custom: custom}
	}
	it.Done = done
	if done {
		it.By, it.At = by, time.Now().UnixMilli()
	} else {
		it.By, it.At = "", 0
	}
	items[id] = it
	return s.save(to, items, order)
}

// Remove deletes a custom item.
func (s *SignOffs) Remove(to, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, order := s.load(to)
	if it, ok := items[id]; !ok || !it.Custom {
		return errors.New("only items you added can be removed")
	}
	delete(items, id)
	return s.save(to, items, order)
}
