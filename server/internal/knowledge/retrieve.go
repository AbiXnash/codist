package knowledge

import (
	"embed"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Retrieval-augmented review: the bugbase (Markdown) and the YAML rules form one corpus of bug
// patterns. For each file under review the entries that most resemble its code are retrieved with
// BM25, using the code's own vocabulary (identifiers split at camelCase, annotations, calls, types)
// as the query, and given to the model as reference material.

//go:embed bugbase/*.md
var bugbaseFS embed.FS

// Entry is one bug pattern of the bugbase.
type Entry struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Severity string `json:"severity"`
	Category string `json:"category"`
	Tags     string `json:"tags,omitempty"`
	See      string `json:"see,omitempty"`
	Body     string `json:"body"`
	Source   string `json:"source"`
}

var entryHeading = regexp.MustCompile(`^##\s+(.+?)\s*\{#([a-z0-9-]+)\}\s*$`)

// ParseBugbase reads entries from Markdown: "## Title {#id}", metadata lines, then the body.
func ParseBugbase(md, source string) []Entry {
	var out []Entry
	var cur *Entry
	inMeta := false
	var body strings.Builder
	flush := func() {
		if cur != nil {
			cur.Body = strings.TrimSpace(body.String())
			out = append(out, *cur)
		}
		body.Reset()
	}
	for _, l := range strings.Split(md, "\n") {
		if m := entryHeading.FindStringSubmatch(l); m != nil {
			flush()
			cur = &Entry{Title: m[1], ID: m[2], Severity: "medium", Source: source}
			inMeta = true
			continue
		}
		if cur == nil {
			continue
		}
		if strings.HasPrefix(l, "# ") { // a new top-level section ends the entry
			flush()
			cur = nil
			continue
		}
		if inMeta {
			if k, v, ok := strings.Cut(l, ":"); ok && !strings.Contains(k, " ") {
				v = strings.TrimSpace(v)
				switch strings.TrimSpace(k) {
				case "severity":
					cur.Severity = v
				case "category":
					cur.Category = v
				case "tags":
					cur.Tags = v
				case "see":
					cur.See = v
				}
				continue
			}
			inMeta = false
		}
		body.WriteString(l + "\n")
	}
	flush()
	return out
}

func loadBugbase(dirs []string) []Entry {
	var out []Entry
	files, _ := fs.Glob(bugbaseFS, "bugbase/*.md")
	for _, f := range files {
		b, _ := bugbaseFS.ReadFile(f)
		out = append(out, ParseBugbase(string(b), "built-in")...)
	}
	for _, d := range dirs {
		matches, _ := filepath.Glob(filepath.Join(d, "*.md"))
		for _, f := range matches {
			if strings.EqualFold(filepath.Base(f), "README.md") {
				continue
			}
			if b, err := os.ReadFile(f); err == nil {
				out = append(out, ParseBugbase(string(b), f)...)
			}
		}
	}
	// A later entry with the same id (team or learned) replaces the built-in one.
	byID := map[string]int{}
	var merged []Entry
	for _, e := range out {
		if i, ok := byID[e.ID]; ok {
			merged[i] = e
			continue
		}
		byID[e.ID] = len(merged)
		merged = append(merged, e)
	}
	return merged
}

// doc is one retrievable item: a bugbase entry or a YAML rule.
type doc struct {
	entry Entry
	tf    map[string]float64
	len   float64
}

type index struct {
	docs  []doc
	df    map[string]float64
	avgdl float64
}

func (b *Base) buildIndex() {
	var docs []doc
	add := func(e Entry, weighted ...string) {
		tf := map[string]float64{}
		n := 0.0
		for i, text := range weighted {
			w := []float64{3, 3, 1, 1.5}[min(i, 3)] // title, tags, body, examples
			for _, t := range tokens(text) {
				tf[t] += w
				n += w
			}
		}
		docs = append(docs, doc{entry: e, tf: tf, len: n})
	}
	for _, e := range b.Entries {
		add(e, e.Title, e.Tags+" "+e.ID, e.Body)
	}
	for _, r := range b.Rules {
		e := Entry{ID: r.ID, Title: r.Title, Severity: r.Severity, Category: r.Category, Tags: strings.Join(r.Triggers, " "),
			Body: strings.TrimSpace(oneLine(r.Check) + " " + oneLine(r.Why)), Source: "rule " + r.Source}
		if r.Bad != "" || r.Good != "" {
			e.Body += "\n\n```java\n// bad\n" + strings.TrimRight(r.Bad, "\n") + "\n// better\n" + strings.TrimRight(r.Good, "\n") + "\n```"
		}
		add(e, e.Title, e.Tags+" "+e.ID, e.Body, r.Bad+" "+r.Good)
	}
	ix := &index{docs: docs, df: map[string]float64{}}
	total := 0.0
	for _, d := range docs {
		total += d.len
		for t := range d.tf {
			ix.df[t]++
		}
	}
	if len(docs) > 0 {
		ix.avgdl = total / float64(len(docs))
	}
	b.ix = ix
}

// Retrieve returns the k entries most relevant to a file, ranked by BM25 with the changed lines
// weighted above the rest of the file. Entries scoring far below the best are dropped.
func (b *Base) Retrieve(path, file, changed string, k int) []Entry {
	if b.ix == nil {
		b.buildIndex()
	}
	ix := b.ix
	if len(ix.docs) == 0 || k <= 0 {
		return nil
	}
	q := map[string]float64{}
	for _, t := range tokens(changed) {
		q[t] = math.Min(q[t]+2, 6)
	}
	for _, t := range tokens(file) {
		q[t] = math.Min(q[t]+0.5, 6)
	}
	kind := Kind(path)
	if kind == "test" {
		q["test"] += 2
	}
	const k1, bb = 1.2, 0.75
	N := float64(len(ix.docs))
	type scored struct {
		e Entry
		s float64
	}
	var hits []scored
	for _, d := range ix.docs {
		s := 0.0
		for t, qw := range q {
			f := d.tf[t]
			if f == 0 {
				continue
			}
			idf := math.Log(1 + (N-ix.df[t]+0.5)/(ix.df[t]+0.5))
			s += qw * idf * f * (k1 + 1) / (f + k1*(1-bb+bb*d.len/ix.avgdl))
		}
		if s > 0 {
			hits = append(hits, scored{d.entry, s})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].s > hits[j].s })
	var out []Entry
	seen := map[string]bool{}
	for _, h := range hits {
		if len(out) == k || h.s < hits[0].s*0.2 {
			break
		}
		if seen[h.e.ID] {
			continue
		}
		seen[h.e.ID] = true
		out = append(out, h.e)
	}
	return out
}

// FormatEntries renders retrieved knowledge for the prompt: what the pattern is and why it matters,
// with examples, as reference rather than instructions.
func FormatEntries(es []Entry, lines int) string {
	var b strings.Builder
	for _, e := range es {
		fmt.Fprintf(&b, "### [%s] %s (%s)\n%s\n\n", e.ID, e.Title, e.Severity, clipBody(e.Body, lines))
	}
	return b.String()
}

func clipBody(s string, lines int) string {
	parts := strings.Split(strings.TrimSpace(s), "\n")
	if len(parts) <= lines {
		return strings.Join(parts, "\n")
	}
	out := strings.Join(parts[:lines], "\n")
	if strings.Count(out, "```")%2 == 1 {
		out += "\n```"
	}
	return out
}

var (
	wordRe  = regexp.MustCompile(`@?[A-Za-z][A-Za-z0-9_]*`)
	camelRe = regexp.MustCompile(`[A-Z]+[a-z0-9]*|[a-z][a-z0-9]*|[0-9]+`)
	stop    = map[string]bool{
		"the": true, "a": true, "an": true, "and": true, "or": true, "of": true, "to": true, "in": true, "is": true,
		"it": true, "for": true, "on": true, "with": true, "as": true, "be": true, "by": true, "at": true, "this": true,
		"that": true, "are": true, "not": true, "if": true, "new": true, "return": true, "public": true, "private": true,
		"protected": true, "static": true, "final": true, "void": true, "class": true, "import": true, "package": true,
		"string": true, "int": true, "long": true, "java": true, "util": true, "lang": true, "var": true, "get": false,
		"bad": true, "better": true, "from": true, "when": true, "one": true, "can": true, "its": true, "has": true,
	}
)

// tokens splits code and prose into retrieval terms: whole identifiers, their camelCase parts,
// and annotations (kept with their @).
func tokens(s string) []string {
	var out []string
	for _, w := range wordRe.FindAllString(s, -1) {
		if strings.HasPrefix(w, "@") {
			out = append(out, strings.ToLower(w))
			w = w[1:]
		}
		lw := strings.ToLower(w)
		if len(lw) > 1 && !stop[lw] {
			out = append(out, lw)
		}
		parts := camelRe.FindAllString(w, -1)
		if len(parts) > 1 {
			for _, p := range parts {
				if lp := strings.ToLower(p); len(lp) > 2 && !stop[lp] {
					out = append(out, lp)
				}
			}
		}
	}
	return out
}
