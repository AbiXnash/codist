// Package knowledge holds what Codist knows about Java and Spring defects: the bugbase (Markdown
// entries explaining real-world bug patterns) and YAML rules (the same kind of knowledge, some with
// deterministic detect patterns). For each changed file the most similar entries are retrieved
// (see retrieve.go) and given to the model as reference material.
package knowledge

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed rules/*.yaml
var builtin embed.FS

type Rule struct {
	ID       string   `yaml:"id" json:"id"`
	Title    string   `yaml:"title" json:"title"`
	Category string   `yaml:"category" json:"category"`
	Severity string   `yaml:"severity" json:"severity"`
	Triggers []string `yaml:"triggers,omitempty" json:"triggers,omitempty"`
	Applies  []string `yaml:"applies,omitempty" json:"applies,omitempty"`
	Paths    []string `yaml:"paths,omitempty" json:"paths,omitempty"`
	Check    string   `yaml:"check" json:"check"`
	Why      string   `yaml:"why" json:"why"`
	Bad      string   `yaml:"bad,omitempty" json:"bad,omitempty"`
	Good     string   `yaml:"good,omitempty" json:"good,omitempty"`
	// Detect makes the rule deterministic: a regex that, when it matches a changed line, reports
	// the rule directly without asking the model (used for deprecated APIs and config mistakes).
	Detect []string `yaml:"detect,omitempty" json:"detect,omitempty"`
	Source string   `yaml:"-" json:"source"`

	matchers []func(string) bool
	detects  []*regexp.Regexp
}

// Hit is a deterministic match of a rule's detect pattern on a line.
type Hit struct {
	Rule Rule
	Line int
}

// Detect runs every detect pattern that applies to path against the given 1-based lines.
func (b *Base) Detect(path string, lines []string, only map[int]bool) []Hit {
	kind := Kind(path)
	var out []Hit
	for _, r := range b.Rules {
		if len(r.detects) == 0 || !slices.Contains(r.Applies, kind) {
			continue
		}
		if len(r.Paths) > 0 && !slices.ContainsFunc(r.Paths, func(p string) bool { return strings.Contains(path, p) }) {
			continue
		}
		for i, l := range lines {
			if only != nil && !only[i+1] {
				continue
			}
			for _, rx := range r.detects {
				if rx.MatchString(l) {
					out = append(out, Hit{Rule: r, Line: i + 1})
					break
				}
			}
		}
	}
	return out
}

type Base struct {
	Rules   []Rule
	Entries []Entry // the bugbase: Markdown knowledge, built-in plus *.md in the knowledge dirs
	ix      *index
}

// Load reads the built-in rules, then every *.yaml / *.yml in dirs (missing dirs are skipped).
// A later rule with the same id replaces an earlier one, so teams can override built-ins.
func Load(dirs ...string) (*Base, error) {
	byID := map[string]Rule{}
	var order []string
	add := func(rules []Rule, source string) {
		for _, r := range rules {
			if r.ID == "" || r.Title == "" {
				continue
			}
			r.Source = source
			if len(r.Applies) == 0 {
				r.Applies = []string{"java"}
			}
			if r.Severity == "" {
				r.Severity = "medium"
			}
			r.compile()
			if _, seen := byID[r.ID]; !seen {
				order = append(order, r.ID)
			}
			byID[r.ID] = r
		}
	}

	entries, err := fs.Glob(builtin, "rules/*.yaml")
	if err != nil {
		return nil, err
	}
	for _, name := range entries {
		b, _ := builtin.ReadFile(name)
		rules, err := parse(b)
		if err != nil {
			return nil, fmt.Errorf("built-in %s: %w", name, err)
		}
		add(rules, "built-in/"+filepath.Base(name))
	}
	for _, dir := range dirs {
		files, _ := filepath.Glob(filepath.Join(dir, "*.y*ml"))
		sort.Strings(files)
		for _, file := range files {
			b, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			rules, err := parse(b)
			if err != nil {
				slog.Warn("skipping invalid rules file", "file", file, "err", err)
				continue
			}
			add(rules, file)
		}
	}

	base := &Base{}
	for _, id := range order {
		base.Rules = append(base.Rules, byID[id])
	}
	base.Entries = loadBugbase(dirs)
	base.buildIndex()
	return base, nil
}

func parse(b []byte) ([]Rule, error) {
	var rules []Rule
	err := yaml.Unmarshal(b, &rules)
	return rules, err
}

func (r *Rule) compile() {
	r.detects = nil
	for _, d := range r.Detect {
		if rx, err := regexp.Compile(strings.TrimPrefix(d, "re:")); err == nil {
			r.detects = append(r.detects, rx)
		} else {
			slog.Warn("invalid detect pattern", "rule", r.ID, "pattern", d, "err", err)
		}
	}
	r.matchers = nil
	for _, t := range r.Triggers {
		if re, ok := strings.CutPrefix(t, "re:"); ok {
			if rx, err := regexp.Compile("(?m)" + re); err == nil {
				r.matchers = append(r.matchers, rx.MatchString)
			}
			continue
		}
		needle := t
		r.matchers = append(r.matchers, func(s string) bool { return strings.Contains(s, needle) })
	}
}

var kinds = map[string]string{
	".java": "java", ".yml": "yaml", ".yaml": "yaml", ".properties": "properties",
	".xml": "xml", ".sql": "sql", ".gradle": "gradle", ".kts": "gradle",
}

// Kind is the rule "applies" key for a file path.
func Kind(path string) string {
	return kinds[strings.ToLower(filepath.Ext(path))]
}

var severityRank = map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3, "info": 4}

// Select picks up to n rules for a file. A trigger in the added lines weighs most, one elsewhere
// in the file less, and rules whose triggers match more completely (more specific) rank higher,
// so a precise trigger like "System.out" beats a broad rule that matches "set".
// General rules (no triggers) fill the last slots.
func (b *Base) Select(path, file, added string, n int) []Rule {
	kind := Kind(path)
	type scored struct {
		r     Rule
		score float64
	}
	var hits, general []scored
	for _, r := range b.Rules {
		if !slices.Contains(r.Applies, kind) {
			continue
		}
		if len(r.Paths) > 0 && !slices.ContainsFunc(r.Paths, func(p string) bool { return strings.Contains(path, p) }) {
			continue
		}
		if len(r.matchers) == 0 {
			general = append(general, scored{r, 0})
			continue
		}
		inAdded, inFile, matched := false, false, 0
		for _, m := range r.matchers {
			switch {
			case m(added):
				inAdded, inFile = true, true
				matched++
			case m(file):
				inFile = true
				matched++
			}
		}
		score := 0.0
		if inAdded {
			score += 3
		} else if inFile {
			score++
		}
		if score > 0 {
			score += 2 * float64(matched) / float64(len(r.matchers))
			hits = append(hits, scored{r, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return severityRank[hits[i].r.Severity] < severityRank[hits[j].r.Severity]
	})
	var out []Rule
	for _, h := range hits {
		if len(out) == n-min(len(general), 2) {
			break
		}
		out = append(out, h.r)
	}
	for _, g := range general {
		if len(out) == n {
			break
		}
		out = append(out, g.r)
	}
	return out
}

// Format renders rules as a compact checklist for the prompt.
func Format(rules []Rule) string {
	var b strings.Builder
	for _, r := range rules {
		fmt.Fprintf(&b, "[%s] (%s, %s) %s\n  Look for: %s\n  Why: %s\n", r.ID, r.Severity, r.Category, r.Title, oneLine(r.Check), oneLine(r.Why))
		if r.Bad != "" {
			b.WriteString("  Bad:\n" + indent(clip(r.Bad, 6), "    "))
		}
		if r.Good != "" {
			b.WriteString("  Good:\n" + indent(clip(r.Good, 6), "    "))
		}
	}
	return b.String()
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func clip(s string, lines int) string {
	parts := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(parts) > lines {
		parts = append(parts[:lines], "...")
	}
	return strings.Join(parts, "\n") + "\n"
}

func indent(s, pad string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n") + "\n"
}

// ExportTraining writes the rules as chat-format JSONL for LoRA fine-tuning (mlx-lm, Unsloth, axolotl).
// Each rule gives a positive example (the bad code, with the finding) and a negative example
// (the good code, with no findings), which teaches the model not to invent problems.
func (b *Base) ExportTraining(w io.Writer, system string) (int, error) {
	enc := json.NewEncoder(w)
	count := 0
	write := func(user, assistant string) error {
		count++
		return enc.Encode(map[string]any{"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
			{"role": "assistant", "content": assistant},
		}})
	}
	for _, r := range b.Rules {
		if r.Bad == "" {
			continue
		}
		lang := r.Applies[0]
		bad := strings.TrimRight(r.Bad, "\n")
		firstLine := 1
		for i, l := range strings.Split(bad, "\n") {
			if t := strings.TrimSpace(l); t != "" && !strings.HasPrefix(t, "//") {
				firstLine = i + 1
				break
			}
		}
		finding, _ := json.Marshal(map[string]any{
			"summary": "Change introduces: " + strings.ToLower(r.Title[:1]) + r.Title[1:] + ".",
			"findings": []map[string]any{{
				"line": firstLine, "endLine": strings.Count(bad, "\n") + 1, "severity": r.Severity, "category": r.Category,
				"title": r.Title, "detail": oneLine(r.Why), "suggestion": strings.TrimRight(r.Good, "\n"), "ref": r.ID,
			}},
		})
		if err := write(reviewPrompt(lang, bad), string(finding)); err != nil {
			return count, err
		}
		if r.Good != "" {
			clean, _ := json.Marshal(map[string]any{"summary": "The change looks correct.", "findings": []any{}})
			if err := write(reviewPrompt(lang, strings.TrimRight(r.Good, "\n")), string(clean)); err != nil {
				return count, err
			}
		}
	}
	return count, nil
}

func reviewPrompt(lang, code string) string {
	var b strings.Builder
	b.WriteString("## File after the commit (\">>\" = changed line)\n```" + lang + "\n")
	for i, l := range strings.Split(code, "\n") {
		fmt.Fprintf(&b, ">> %4d | %s\n", i+1, l)
	}
	b.WriteString("```\n")
	return b.String()
}
