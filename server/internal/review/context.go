package review

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"codist/internal/javaidx"
)

// section is one block of the prompt. When the prompt is over budget, sections with the highest
// priority number are trimmed first, down to keep characters (0 means the section may be dropped).
type section struct {
	title    string
	body     string
	priority int
	keep     int
}

// assemble fits the sections into budget characters, trimming the least important ones line by
// line from the end, and returns them in their original order.
func assemble(sections []section, budget int) string {
	total := 0
	for _, s := range sections {
		total += len(s.title) + len(s.body)
	}
	order := make([]int, len(sections))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return sections[order[a]].priority > sections[order[b]].priority })
	for _, i := range order {
		if total <= budget {
			break
		}
		s := &sections[i]
		excess := total - budget
		target := max(s.keep, len(s.body)-excess)
		if target >= len(s.body) {
			continue
		}
		cut := s.body[:target]
		if nl := strings.LastIndex(cut, "\n"); nl > 0 {
			cut = cut[:nl+1]
		}
		if target == 0 {
			cut = ""
		}
		total -= len(s.body) - len(cut)
		if cut != "" {
			cut += "… (trimmed to fit the context window)\n"
		} else {
			total -= len(s.title)
		}
		s.body = cut
	}
	var b strings.Builder
	for _, s := range sections {
		if s.body == "" {
			continue
		}
		b.WriteString(s.title + "\n" + s.body)
		if !strings.HasSuffix(s.body, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// rankRelated orders neighbouring types by how relevant they are to the change: types named in
// the changed lines first, then injected dependencies, then everything else.
func rankRelated(related []javaidx.Node, injected map[string]bool, added string) []javaidx.Node {
	score := func(n javaidx.Node) int {
		s := 0
		if regexp.MustCompile(`\b` + regexp.QuoteMeta(n.Name) + `\b`).MatchString(added) {
			s += 4
		}
		if injected[n.ID] {
			s += 2
		}
		if n.Stereotype == "entity" || n.Stereotype == "repository" || n.Stereotype == "service" {
			s++
		}
		return s
	}
	sort.SliceStable(related, func(i, j int) bool { return score(related[i]) > score(related[j]) })
	return related
}

// trivial reports whether the added lines are only imports, blank lines, braces, comments or annotations.
func trivial(lines []string, added []int) bool {
	for _, l := range added {
		if l < 1 || l > len(lines) {
			continue
		}
		t := strings.TrimSpace(lines[l-1])
		switch {
		case t == "", t == "{", t == "}", t == "};", t == ");":
		case strings.HasPrefix(t, "import "), strings.HasPrefix(t, "package "):
		case strings.HasPrefix(t, "//"), strings.HasPrefix(t, "/*"), strings.HasPrefix(t, "*"):
		case strings.HasPrefix(t, "@") && !strings.Contains(t, "("):
		default:
			return false
		}
	}
	return true
}

// cache stores model answers by prompt hash, so re-reviews of unchanged files are instant.
type cache struct {
	dir string
	mu  sync.Mutex
}

func (c *cache) key(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func (c *cache) get(key string) (map[string]any, bool) {
	b, err := os.ReadFile(filepath.Join(c.dir, key+".json"))
	if err != nil {
		return nil, false
	}
	var out map[string]any
	return out, json.Unmarshal(b, &out) == nil
}

func (c *cache) put(key string, v map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if os.MkdirAll(c.dir, 0o755) != nil {
		return
	}
	b, _ := json.Marshal(v)
	_ = os.WriteFile(filepath.Join(c.dir, key+".json"), b, 0o644)
}

// history describes earlier findings in this file, so the model knows what was reported before
// and can say when a problem comes back.
func (s *Service) history(path, sha string, limit int) string {
	var b strings.Builder
	n := 0
	for _, r := range s.store.All() {
		if r.SHA == sha || r.Status != StatusDone {
			continue
		}
		for _, f := range r.Files {
			if f.Path != path || len(f.Findings) == 0 {
				continue
			}
			fmt.Fprintf(&b, "- %s: ", r.SHA[:7])
			var titles []string
			for i, x := range f.Findings {
				if i == 5 {
					titles = append(titles, "…")
					break
				}
				titles = append(titles, fmt.Sprintf("%s (%s)", x.Title, x.Severity))
			}
			b.WriteString(strings.Join(titles, "; ") + "\n")
			n++
		}
		if n >= limit {
			break
		}
	}
	return b.String()
}
