package incident

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"codist/internal/knowledge"
	"codist/prompts"
)

func (e *Engine) explain(in *Incident) error {
	var b strings.Builder
	b.WriteString("## Errors in the logs\n")
	for i, ev := range in.Errors {
		if i == 6 {
			break
		}
		fmt.Fprintf(&b, "- %s: %s (×%d", orDash(ev.Exception), trim(ev.Message, 300), ev.Count)
		if ev.FirstTime != "" {
			fmt.Fprintf(&b, ", first %s, last %s", ev.FirstTime, ev.LastTime)
		}
		b.WriteString(")\n")
		for _, c := range ev.CausedBy {
			b.WriteString("    caused by " + trim(c, 200) + "\n")
		}
		shown := 0
		for _, f := range ev.Frames {
			if shown == 8 {
				break
			}
			fmt.Fprintf(&b, "    at %s.%s(%s:%d)\n", f.Class, f.Method, f.File, f.Line)
			shown++
		}
	}

	b.WriteString("\n## Code where the errors occur (\">>\" marks the line in the stack trace)\n")
	head := e.repo.Head()
	for i, l := range in.Locations {
		if i == 5 {
			break
		}
		src, ok := e.repo.ReadFile(head, l.Path)
		if !ok {
			continue
		}
		lines := strings.Split(src, "\n")
		from, to := max(1, l.Line-12), min(len(lines), l.Line+12)
		if l.MethodBeg > 0 && l.MethodEnd-l.MethodBeg < 40 {
			from, to = max(1, l.MethodBeg-2), min(len(lines), l.MethodEnd+1)
		}
		fmt.Fprintf(&b, "### %s (%s.%s)", l.Path, l.Class[strings.LastIndex(l.Class, ".")+1:], l.Method)
		if l.Blame.SHA != "" {
			fmt.Fprintf(&b, " — line %d last changed in %s by %s: %s", l.Line, l.Blame.SHA[:7], l.Blame.Author, l.Blame.Summary)
		}
		b.WriteString("\n```java\n")
		for n := from; n <= to; n++ {
			mark := "   "
			if n == l.Line {
				mark = ">> "
			}
			fmt.Fprintf(&b, "%s%4d | %s\n", mark, n, lines[n-1])
		}
		b.WriteString("```\n")
		for _, is := range l.Issues {
			b.WriteString("Codist already flagged here: " + is + "\n")
		}
	}

	if len(in.Suspects) > 0 {
		b.WriteString("\n## Commits that most recently changed this code (ranked)\n")
		for i, s := range in.Suspects {
			if i == 4 {
				break
			}
			fmt.Fprintf(&b, "- %s %s by %s, %s: %s — %s\n", s.SHA[:7], time.UnixMilli(s.Time).Format("2006-01-02"), s.Author,
				s.Message, strings.Join(s.Reasons, "; "), "")
			if c, err := e.repo.Commit(s.SHA); err == nil {
				if files, err := e.repo.Diff(c); err == nil {
					for _, f := range files {
						for _, l := range in.Locations {
							if f.Path == l.Path {
								b.WriteString("```diff\n" + trim(f.Patch, 2500) + "\n```\n")
								break
							}
						}
					}
				}
			}
		}
	}
	if len(in.Similar) > 0 {
		b.WriteString("\n## Similar past incidents\n")
		for _, s := range in.Similar {
			fmt.Fprintf(&b, "- %s (similarity %.0f%%): %s\n", s.Title, s.Score*100, trim(s.RootCause, 300))
		}
	}

	out, err := e.llm.ChatJSON(prompts.Get("incident-rca"), trim(b.String(), 40000))
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, l := range in.Locations {
		known[l.Path] = true
	}
	shas := map[string]string{}
	for _, s := range in.Suspects {
		shas[s.SHA[:7]] = s.SHA
	}
	raw, _ := out["causes"].([]any)
	for _, item := range raw {
		c, ok := item.(map[string]any)
		if !ok {
			continue
		}
		cause := Cause{Title: str(c["title"]), Explanation: str(c["explanation"]), Confidence: strings.ToLower(str(c["confidence"])),
			Fix: str(c["fix"]), Path: str(c["path"]), Line: num(c["line"])}
		if cause.Title == "" {
			continue
		}
		if !known[cause.Path] {
			cause.Path = firstPath(in.Locations)
		}
		cause.SHA = resolveSHA(str(c["sha"]), shas)
		if ev, ok := c["evidence"].([]any); ok {
			for _, x := range ev {
				m, ok := x.(map[string]any)
				if !ok {
					continue
				}
				evd := Evidence{Kind: str(m["kind"]), Text: str(m["text"]), Path: str(m["path"]), Line: num(m["line"]), SHA: resolveSHA(str(m["sha"]), shas)}
				if evd.Path != "" && !known[evd.Path] {
					evd.Path, evd.Line = "", 0 // the model cited a file that is not in the evidence
				}
				if evd.Text != "" {
					cause.Evidence = append(cause.Evidence, evd)
				}
			}
		}
		if cause.Confidence != "high" && cause.Confidence != "medium" {
			cause.Confidence = "low"
		}
		// Every cause cites the log: add the top error when the model did not.
		hasLog := false
		for _, ev := range cause.Evidence {
			hasLog = hasLog || ev.Kind == "log"
		}
		if !hasLog && len(in.Errors) > 0 {
			top := in.Errors[0]
			text := strings.TrimSpace(top.Exception + ": " + top.Message)
			if len(top.Frames) > 0 {
				f := top.Frames[0]
				text += fmt.Sprintf(" at %s.%s(%s:%d)", simpleClass(f.Class), f.Method, f.File, f.Line)
			}
			cause.Evidence = append([]Evidence{{Kind: "log", Text: fmt.Sprintf("%s (×%d)", trim(text, 300), top.Count)}}, cause.Evidence...)
		}
		in.Causes = append(in.Causes, cause)
		if len(in.Causes) == 3 {
			break
		}
	}
	if r, ok := out["report"].(map[string]any); ok {
		in.Report = Report{Summary: str(r["summary"]), Impact: str(r["impact"]), RootCause: str(r["rootCause"]),
			Fix: str(r["fix"]), Prevention: str(r["prevention"])}
	}
	in.Report.Timeline = strings.Join(in.Timeline, "\n")
	return nil
}

// RuleFor drafts a knowledge-base rule from a cause, as YAML for the engineer to review.
func (e *Engine) RuleFor(in *Incident, idx int) (string, error) {
	if idx < 0 || idx >= len(in.Causes) {
		return "", fmt.Errorf("no such cause")
	}
	c := in.Causes[idx]
	var b strings.Builder
	fmt.Fprintf(&b, "Root cause: %s\n%s\nFix: %s\n", c.Title, c.Explanation, c.Fix)
	for _, ev := range c.Evidence {
		fmt.Fprintf(&b, "- %s: %s\n", ev.Kind, ev.Text)
	}
	out, err := e.llm.ChatJSON(prompts.Get("incident-rule"), b.String())
	if err != nil {
		return "", err
	}
	r := knowledge.Rule{ID: "learned-" + strings.TrimPrefix(slug(str(out["id"])), "learned-"), Title: str(out["title"]),
		Category: str(out["category"]), Severity: strings.ToLower(str(out["severity"])), Check: str(out["check"]),
		Why: str(out["why"]), Bad: str(out["bad"]), Good: str(out["good"])}
	for _, k := range []string{"triggers", "detect"} {
		if list, ok := out[k].([]any); ok {
			for _, v := range list {
				if s := str(v); s != "" {
					if k == "triggers" {
						r.Triggers = append(r.Triggers, s)
					} else {
						r.Detect = append(r.Detect, s)
					}
				}
			}
		}
	}
	if r.ID == "learned-" {
		r.ID = "learned-" + slug(r.Title)
	}
	// A rule must describe the pattern, not this incident: drop triggers and patterns that name
	// project classes or exception types, and patterns that do not compile.
	project := map[string]bool{}
	for _, n := range e.graph.Graph().Nodes {
		project[n.Name] = true
	}
	specific := func(s string) bool {
		for _, w := range regexp.MustCompile(`[A-Za-z_]\w*`).FindAllString(s, -1) {
			if project[w] || strings.HasSuffix(w, "Exception") {
				return true
			}
		}
		return false
	}
	var triggers, detect []string
	for _, t := range r.Triggers {
		if !specific(t) {
			triggers = append(triggers, t)
		}
	}
	for _, d := range r.Detect {
		if _, err := regexp.Compile(d); err == nil && !specific(d) {
			detect = append(detect, d)
		}
	}
	r.Triggers, r.Detect = triggers, detect
	b2, err := yaml.Marshal([]knowledge.Rule{r})
	if err != nil {
		return "", err
	}
	return "# Learned from incident: " + in.Title + "\n" + string(b2), nil
}

// Runbook drafts a runbook entry from the incident.
func (e *Engine) Runbook(in *Incident) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\nSummary: %s\nImpact: %s\nRoot cause: %s\nFix: %s\nPrevention: %s\n\nErrors:\n",
		in.Title, in.Report.Summary, in.Report.Impact, in.Report.RootCause, in.Report.Fix, in.Report.Prevention)
	for i, ev := range in.Errors {
		if i == 4 {
			break
		}
		fmt.Fprintf(&b, "- %s: %s (×%d)\n", orDash(ev.Exception), trim(ev.Message, 200), ev.Count)
	}
	if in.Confirmed >= 0 && in.Confirmed < len(in.Causes) {
		c := in.Causes[in.Confirmed]
		fmt.Fprintf(&b, "\nConfirmed cause: %s. %s\n", c.Title, c.Explanation)
	}
	md, err := e.llm.ChatText(prompts.Get("incident-runbook"), b.String())
	if err != nil {
		return "", err
	}
	md = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(md), "```markdown"), "```"))
	return md, nil
}

// ReportMarkdown renders the RCA report for copying or saving.
func ReportMarkdown(in *Incident) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# RCA: %s\n\n_Date: %s · Status: %s_\n\n", in.Title, time.UnixMilli(in.CreatedAt).Format("2006-01-02"), in.Status)
	sec := func(t, body string) {
		if strings.TrimSpace(body) != "" {
			fmt.Fprintf(&b, "## %s\n\n%s\n\n", t, strings.TrimSpace(body))
		}
	}
	sec("Summary", in.Report.Summary)
	sec("Timeline", in.Report.Timeline)
	sec("Impact", in.Report.Impact)
	root := in.Report.RootCause
	if in.Confirmed >= 0 && in.Confirmed < len(in.Causes) {
		c := in.Causes[in.Confirmed]
		root = c.Title + ". " + c.Explanation + "\n\n" + root
		if c.SHA != "" {
			root += fmt.Sprintf("\n\nIntroduced in commit %s.", c.SHA[:7])
		}
	}
	sec("Root cause", root)
	sec("Fix", in.Report.Fix)
	sec("Prevention", in.Report.Prevention)
	if len(in.Errors) > 0 {
		b.WriteString("## Errors\n\n")
		for _, ev := range in.Errors {
			fmt.Fprintf(&b, "- `%s` %s (×%d)\n", orDash(ev.Exception), trim(ev.Message, 160), ev.Count)
		}
	}
	return b.String()
}

// ParseRuleYAML validates learned-rule YAML before it is saved.
func ParseRuleYAML(text string) ([]knowledge.Rule, error) {
	var rules []knowledge.Rule
	if err := yaml.Unmarshal([]byte(text), &rules); err != nil {
		return nil, err
	}
	for _, r := range rules {
		if r.ID == "" || r.Title == "" || r.Check == "" {
			return nil, fmt.Errorf("each rule needs id, title and check")
		}
	}
	return rules, nil
}

func firstPath(locs []Location) string {
	if len(locs) > 0 {
		return locs[0].Path
	}
	return ""
}

func resolveSHA(short string, known map[string]string) string {
	if len(short) >= 7 {
		if full, ok := known[short[:7]]; ok {
			return full
		}
	}
	return ""
}

func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func orDash(s string) string {
	if s == "" {
		return "error"
	}
	return s
}

func str(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func num(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case string:
		var n int
		fmt.Sscan(t, &n)
		return n
	}
	return 0
}
