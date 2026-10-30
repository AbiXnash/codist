package review

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Ignore scopes:
//   - commit: this finding in this commit only
//   - code:   this finding on this exact line of code, in every commit (survives line moves)
//   - rule:   this rule everywhere, optionally limited to path prefixes
type Ignore struct {
	ID        string   `json:"id" yaml:"-"`
	Scope     string   `json:"scope" yaml:"-"`
	SHA       string   `json:"sha,omitempty" yaml:"-"`
	Path      string   `json:"path,omitempty" yaml:"path,omitempty"`
	Paths     []string `json:"paths,omitempty" yaml:"paths,omitempty"`
	Line      int      `json:"line,omitempty" yaml:"-"`
	Key       string   `json:"key" yaml:"rule"`
	Category  string   `json:"category,omitempty" yaml:"-"`
	Snippet   string   `json:"snippet,omitempty" yaml:"code,omitempty"`
	Title     string   `json:"title,omitempty" yaml:"title,omitempty"`
	Reason    string   `json:"reason,omitempty" yaml:"reason,omitempty"`
	Team      bool     `json:"team" yaml:"-"` // from .codist/ignore.yaml in the repository
	CreatedAt int64    `json:"createdAt" yaml:"-"`
}

// Ignores keeps personal ignores in the data directory and reads team ignores from the repository.
type Ignores struct {
	local string // <data-dir>/<repo>/ignores.json
	team  string // <repo>/.codist/ignore.yaml
	mu    sync.Mutex
	items []Ignore
}

func NewIgnores(dataDir, repoName, repoRoot string) *Ignores {
	x := &Ignores{local: filepath.Join(dataDir, repoName, "ignores.json"), team: filepath.Join(repoRoot, ".codist", "ignore.yaml")}
	if b, err := os.ReadFile(x.local); err == nil {
		_ = json.Unmarshal(b, &x.items)
	}
	return x
}

// All returns personal and team ignores; the team file is re-read so edits apply immediately.
func (x *Ignores) All() []Ignore {
	x.mu.Lock()
	out := append([]Ignore{}, x.items...)
	x.mu.Unlock()
	return append(out, x.teamIgnores()...)
}

func (x *Ignores) teamIgnores() []Ignore {
	b, err := os.ReadFile(x.team)
	if err != nil {
		return nil
	}
	var raw []Ignore
	if yaml.Unmarshal(b, &raw) != nil {
		return nil
	}
	for i := range raw {
		raw[i].Team = true
		raw[i].ID = "team-" + itoa(i)
		raw[i].Scope = "rule"
		if raw[i].Snippet != "" {
			raw[i].Scope = "code"
		}
	}
	return raw
}

// Add stores an ignore. Team ignores are appended to .codist/ignore.yaml so they can be committed.
func (x *Ignores) Add(ig Ignore) (Ignore, error) {
	ig.ID, ig.CreatedAt = newID(), time.Now().UnixMilli()
	if ig.Team {
		if ig.Scope == "commit" {
			return ig, errors.New("commit-level ignores are personal; choose this line or this rule to share with the team")
		}
		existing := x.teamIgnores()
		existing = append(existing, ig)
		if err := os.MkdirAll(filepath.Dir(x.team), 0o755); err != nil {
			return ig, err
		}
		b, _ := yaml.Marshal(existing)
		header := "# Findings the team marked as intentional. Commit this file to share it.\n" +
			"# rule: rule id (or title:<lowercase title>)  code: exact line  path/paths: where it applies\n"
		return ig, os.WriteFile(x.team, append([]byte(header), b...), 0o644)
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	x.items = append(x.items, ig)
	return ig, x.persist()
}

func (x *Ignores) Remove(id string) error {
	if strings.HasPrefix(id, "team-") {
		return errors.New("team ignores live in .codist/ignore.yaml; edit that file to remove them")
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	for i, ig := range x.items {
		if ig.ID == id {
			x.items = append(x.items[:i], x.items[i+1:]...)
			return x.persist()
		}
	}
	return nil
}

func (x *Ignores) persist() error {
	if err := os.MkdirAll(filepath.Dir(x.local), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(x.items, "", "  ")
	return os.WriteFile(x.local, b, 0o644)
}

// Match returns the ignore covering a finding, if any.
func (x *Ignores) Match(all []Ignore, sha, path string, f Finding) (Ignore, bool) {
	snippet := strings.TrimSpace(f.Snippet)
	for _, ig := range all {
		switch ig.Scope {
		case "commit":
			if ig.SHA == sha && ig.Path == path && ig.Line == f.Line && (ig.Key == f.Key() || ig.Title == f.Title) {
				return ig, true
			}
		case "code":
			sameKind := ig.Key == f.Key() || (strings.HasPrefix(ig.Key, "title:") && f.Ref == "" && ig.Category == f.Category)
			if (ig.Path == "" || ig.Path == path) && sameKind && snippet != "" && strings.TrimSpace(ig.Snippet) == snippet {
				return ig, true
			}
		case "rule":
			if ig.Key != f.Key() {
				continue
			}
			if len(ig.Paths) == 0 && ig.Path == "" {
				return ig, true
			}
			for _, p := range append(ig.Paths, ig.Path) {
				if p != "" && strings.HasPrefix(path, p) {
					return ig, true
				}
			}
		}
	}
	return Ignore{}, false
}

// Decorate marks ignored findings on a copy of the review, so counts and the UI reflect current ignores.
func (x *Ignores) Decorate(r Review) Review {
	all := x.All()
	if len(all) == 0 {
		return r
	}
	files := make([]FileReview, len(r.Files))
	for i, f := range r.Files {
		f.Findings = append([]Finding{}, f.Findings...)
		for j := range f.Findings {
			if ig, ok := x.Match(all, r.SHA, f.Path, f.Findings[j]); ok {
				f.Findings[j].Ignored, f.Findings[j].IgnoreID = true, ig.ID
			}
		}
		files[i] = f
	}
	r.Files = files
	return r
}

// ForPrompt lists ignores relevant to a file, so the model does not spend tokens re-reporting them.
func (x *Ignores) ForPrompt(path string) []string {
	var out []string
	for _, ig := range x.All() {
		if ig.Scope == "commit" || (ig.Path != "" && ig.Path != path) {
			continue
		}
		what := strings.TrimPrefix(ig.Key, "title:")
		if ig.Title != "" {
			what = ig.Title
		}
		line := "- " + what
		if ig.Snippet != "" {
			line += " on `" + strings.TrimSpace(ig.Snippet) + "`"
		}
		if ig.Reason != "" {
			line += " (" + ig.Reason + ")"
		}
		out = append(out, line)
	}
	sort.Strings(out)
	return out
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}
