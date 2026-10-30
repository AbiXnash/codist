// Package prompts holds every instruction Codist gives a model, as Markdown files.
//
// The files in this directory are built into the binary. Any of them can be overridden without a
// rebuild by a file of the same name in <data-dir>/prompts/ (yours) or <repo>/.codist/prompts/
// (the team's, committed with the code); the team's copy wins. Overrides are re-read when they change.
//
// Each file starts with a front matter block (description, model, format, vars) followed by the
// prompt. Prompts that take values use Go template syntax: {{.Items}}, {{join .Items ", "}}.
package prompts

import (
	"bytes"
	"embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"
)

//go:embed *.md
var builtin embed.FS

// Prompt is one instruction file.
type Prompt struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Model       string `json:"model,omitempty"`
	Format      string `json:"format"` // json or text
	Vars        string `json:"vars,omitempty"`
	Body        string `json:"body"`
	Source      string `json:"source"` // built-in, personal or team
	Path        string `json:"path,omitempty"`
}

type cached struct {
	mod time.Time
	p   Prompt
}

var (
	mu    sync.Mutex
	dirs  []struct{ dir, source string }
	cache = map[string]cached{}
)

// Configure sets where overrides live: personal first, then the team's (which wins).
func Configure(personal, team string) {
	mu.Lock()
	defer mu.Unlock()
	dirs = dirs[:0]
	if personal != "" {
		dirs = append(dirs, struct{ dir, source string }{personal, "personal"})
	}
	if team != "" {
		dirs = append(dirs, struct{ dir, source string }{team, "team"})
	}
	cache = map[string]cached{}
}

// Get returns the prompt body. It panics on an unknown name: that is a programming error.
func Get(name string) string { return Load(name).Body }

// Load returns the prompt with its metadata, from the highest-priority place that has it.
func Load(name string) Prompt {
	mu.Lock()
	ds := append([]struct{ dir, source string }{}, dirs...)
	mu.Unlock()
	for i := len(ds) - 1; i >= 0; i-- {
		path := filepath.Join(ds[i].dir, name+".md")
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		mu.Lock()
		c, ok := cache[path]
		mu.Unlock()
		if ok && c.mod.Equal(info.ModTime()) {
			return c.p
		}
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		p := parse(name, string(b))
		if strings.TrimSpace(p.Body) == "" {
			slog.Warn("ignoring empty prompt override", "path", path)
			continue
		}
		p.Source, p.Path = ds[i].source, path
		if def := builtinPrompt(name); def.Format == "json" && !strings.Contains(strings.ToLower(p.Body), "json") {
			slog.Warn("prompt override does not ask for JSON; answers may not parse", "path", path)
		}
		mu.Lock()
		cache[path] = cached{info.ModTime(), p}
		mu.Unlock()
		return p
	}
	return builtinPrompt(name)
}

func builtinPrompt(name string) Prompt {
	b, err := builtin.ReadFile(name + ".md")
	if err != nil {
		panic("prompts: no prompt named " + name)
	}
	p := parse(name, string(b))
	p.Source = "built-in"
	return p
}

// Render fills a prompt's template with data.
func Render(name string, data any) string {
	body := Get(name)
	t, err := template.New(name).Funcs(template.FuncMap{"join": strings.Join}).Parse(body)
	if err != nil {
		slog.Warn("prompt template", "name", name, "err", err)
		return body
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		slog.Warn("prompt template", "name", name, "err", err)
		return body
	}
	return b.String()
}

// Items is the data for prompts that list things ({{range .Items}}).
func Items(items []string) map[string]any { return map[string]any{"Items": items} }

// All lists every prompt as it is in effect now.
func All() []Prompt {
	entries, _ := builtin.ReadDir(".")
	var out []Prompt
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			out = append(out, Load(strings.TrimSuffix(e.Name(), ".md")))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Export writes the built-in prompts to dir (for editing), skipping files that already exist.
func Export(dir string) (int, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	entries, _ := builtin.ReadDir(".")
	n := 0
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if _, err := os.Stat(path); err == nil || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		b, _ := builtin.ReadFile(e.Name())
		if err := os.WriteFile(path, b, 0o644); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func parse(name, s string) Prompt {
	p := Prompt{Name: name, Format: "text"}
	s = strings.TrimPrefix(s, "\uFEFF")
	if strings.HasPrefix(s, "---\n") {
		if end := strings.Index(s[4:], "\n---\n"); end >= 0 {
			for _, l := range strings.Split(s[4:4+end], "\n") {
				k, v, ok := strings.Cut(l, ":")
				if !ok {
					continue
				}
				v = strings.TrimSpace(v)
				switch strings.TrimSpace(k) {
				case "description":
					p.Description = v
				case "model":
					p.Model = v
				case "format":
					p.Format = v
				case "vars":
					p.Vars = v
				}
			}
			s = s[4+end+5:]
		}
	}
	p.Body = strings.TrimSpace(s)
	return p
}

// String is for logs.
func (p Prompt) String() string { return fmt.Sprintf("%s (%s)", p.Name, p.Source) }
