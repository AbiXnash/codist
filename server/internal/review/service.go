package review

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"codist/internal/config"
	"codist/internal/events"
	"codist/internal/gitx"
	"codist/internal/graph"
	"codist/internal/javaidx"
	"codist/internal/knowledge"
	"codist/prompts"
)

var (
	reviewable = []string{".java", ".xml", ".yml", ".yaml", ".properties", ".sql", ".gradle", ".kts"}
	severities = []string{"critical", "high", "medium", "low", "info"}
	risks      = []string{"low", "medium", "high"}
)

// FilePrompt is the system prompt used for per-file reviews (also used for training data).
func FilePrompt() string { return prompts.Get("review-file") }

// Service reviews commits one at a time, file by file.
type Service struct {
	cfg      config.Config
	repo     *gitx.Repo
	graph    *graph.Service
	analyzer *Analyzer
	llm      *Ollama
	store    *Store
	hub      *events.Hub

	queue   chan string
	mu      sync.Mutex
	pending map[string]bool

	OnDone func(sha string) // called after a review finishes (used to prefetch fixes)

	kbMu    sync.Mutex
	kb      *knowledge.Base
	kbStamp string

	summaryLLM *Ollama
	cache      *cache
	builds     sync.Map // sha -> Build
	sources    sync.Map // sha -> map[path]source
	Ignores    *Ignores
}

// Build reads pom.xml or build.gradle at a commit (cached per commit).
func (s *Service) Build(sha string) Build {
	if b, ok := s.builds.Load(sha); ok {
		return b.(Build)
	}
	var b Build
	for _, p := range []string{"pom.xml", "build.gradle.kts", "build.gradle"} {
		if content, ok := s.repo.ReadFile(sha, p); ok {
			b, _ = ParseBuild(p, content)
			break
		}
	}
	s.builds.Store(sha, b)
	return b
}

// Reviewed reports whether a commit already has a stored review.
func (s *Service) Reviewed(sha string) bool {
	_, ok := s.store.Get(sha)
	return ok
}

// Interrupted reports whether a commit's review was cut off by a restart.
func (s *Service) Interrupted(sha string) bool {
	r, ok := s.store.Get(sha)
	return ok && r.Status == StatusFailed && strings.HasPrefix(r.Error, "Interrupted")
}

// KnowledgeDirs are the folders searched for extra rule files, in override order.
func (s *Service) KnowledgeDirs() []string {
	dirs := []string{filepath.Join(s.cfg.DataDir, "rules"), filepath.Join(s.repo.Root, ".codist", "rules")}
	if s.cfg.RulesDir != "" {
		dirs = append(dirs, s.cfg.RulesDir)
	}
	return dirs
}

// Knowledge returns the rules, reloaded only when a rule file is added, edited or removed, so
// edits apply to the next review without a restart.
func (s *Service) Knowledge() *knowledge.Base {
	stamp := s.KnowledgeVersion()
	s.kbMu.Lock()
	defer s.kbMu.Unlock()
	if s.kb != nil && s.kbStamp == stamp {
		return s.kb
	}
	kb, err := knowledge.Load(s.KnowledgeDirs()...)
	if err != nil {
		slog.Warn("knowledge base", "err", err)
		return &knowledge.Base{}
	}
	s.kb, s.kbStamp = kb, stamp
	return kb
}

// KnowledgeVersion changes when any rule file in the knowledge directories changes.
func (s *Service) KnowledgeVersion() string {
	var b strings.Builder
	for _, d := range s.KnowledgeDirs() {
		entries, _ := os.ReadDir(d)
		for _, e := range entries {
			if info, err := e.Info(); err == nil {
				fmt.Fprintf(&b, "%s/%s:%d:%d;", d, e.Name(), info.Size(), info.ModTime().UnixNano())
			}
		}
	}
	return b.String()
}

func NewService(cfg config.Config, repo *gitx.Repo, g *graph.Service, a *Analyzer, llm *Ollama, store *Store, hub *events.Hub) *Service {
	s := &Service{cfg: cfg, repo: repo, graph: g, analyzer: a, llm: llm, store: store, hub: hub,
		queue: make(chan string, 256), pending: map[string]bool{}, summaryLLM: llm,
		cache: &cache{dir: filepath.Join(cfg.DataDir, repo.Name(), "cache")}}
	if cfg.SummaryModel != "" && cfg.SummaryModel != llm.Model {
		s.summaryLLM = NewOllama(cfg.OllamaURL, cfg.SummaryModel, cfg.NumCtx, cfg.LLMTimeout)
	}
	go s.worker()
	return s
}

func (s *Service) Model() string { return s.llm.Model }

// Enqueue queues a review; it returns false when one is already queued or running.
func (s *Service) Enqueue(sha string) bool {
	s.mu.Lock()
	if s.pending[sha] {
		s.mu.Unlock()
		return false
	}
	s.pending[sha] = true
	s.mu.Unlock()

	r := Review{SHA: sha, Status: StatusQueued, Model: s.llm.Model, Highlights: []string{}, Files: []FileReview{}}
	s.save(r)
	s.queue <- sha
	return true
}

func (s *Service) worker() {
	for sha := range s.queue {
		s.run(sha)
		s.mu.Lock()
		delete(s.pending, sha)
		s.mu.Unlock()
		if s.OnDone != nil {
			go s.OnDone(sha)
		}
	}
}

// Busy reports whether reviews are queued or running.
func (s *Service) Busy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pending) > 0
}

func (s *Service) save(r Review) {
	s.store.Save(r)
	counts := r.Counts()
	if s.Ignores != nil {
		counts = AssignGroups(s.Ignores.Decorate(r)).Counts()
	}
	s.hub.Publish("review", map[string]any{"sha": r.SHA, "status": r.Status, "files": len(r.Files), "counts": counts})
}

func (s *Service) run(sha string) {
	r := Review{SHA: sha, Status: StatusRunning, Model: s.llm.Model, StartedAt: time.Now().UnixMilli(),
		Highlights: []string{}, Files: []FileReview{}}
	s.save(r)
	fail := func(err error) {
		slog.Warn("review failed", "sha", sha[:7], "err", err)
		r.Status, r.Error, r.FinishedAt = StatusFailed, err.Error(), time.Now().UnixMilli()
		s.save(r)
	}

	problem := s.llm.Problem()
	a, err := s.analyzer.Analyze(sha)
	if err != nil {
		fail(err)
		return
	}

	var targets []gitx.FileChange
	for _, f := range a.Files {
		if f.Deleted() || f.Binary || len(f.AddedLines) == 0 {
			continue
		}
		if slices.ContainsFunc(reviewable, func(ext string) bool { return strings.HasSuffix(f.Path, ext) }) {
			targets = append(targets, f)
		}
	}
	sort.SliceStable(targets, func(i, j int) bool {
		if targets[i].IsJava() != targets[j].IsJava() {
			return targets[i].IsJava()
		}
		return len(targets[i].AddedLines) > len(targets[j].AddedLines)
	})
	if len(targets) > s.cfg.MaxFiles {
		targets = targets[:s.cfg.MaxFiles]
	}

	kb := s.Knowledge()
	build := s.Build(sha)
	for _, f := range targets {
		slog.Info("reviewing", "file", f.Path, "sha", sha[:7])
		fr, in, ok := s.rulePass(a, f, kb, build)
		fr.Pending = ok && problem == ""
		if ok && problem != "" {
			fr.Error = problem
		}
		r.Files = append(r.Files, fr)
		s.save(r)
		if fr.Pending {
			r.Files[len(r.Files)-1] = s.modelPass(a, f, fr, in, kb, build)
			s.save(r)
		}
	}

	risk := ruleRisk(r.Files)
	switch {
	case len(targets) == 0:
		r.Summary = "No reviewable source changes in this commit."
	case problem != "":
		r.Summary = problem + ". Only the static checks ran."
	default:
		out, err := s.summaryLLM.ChatJSON(prompts.Get("review-commit"), commitContext(a, r.Files))
		if err != nil {
			r.Summary = "Per-file reviews finished, but the commit summary failed: " + err.Error()
			break
		}
		r.Summary = str(out["summary"])
		r.SuggestedMessage = strings.TrimSpace(str(out["message"]))
		if hl, ok := out["highlights"].([]any); ok {
			for _, h := range hl {
				r.Highlights = append(r.Highlights, str(h))
			}
		}
		if llmRisk := normalise(str(out["risk"]), risks, "low"); slices.Index(risks, llmRisk) > slices.Index(risks, risk) {
			risk = llmRisk
		}
	}
	r.MessageProblem = messageProblem(a)
	if r.MessageProblem == "" {
		r.SuggestedMessage = ""
	} else {
		r.SuggestedMessage = fixMessageType(r.SuggestedMessage, a, r.Files)
	}
	r.Risk, r.Status, r.FinishedAt = risk, StatusDone, time.Now().UnixMilli()
	s.save(r)
}

var genericMessage = regexp.MustCompile(`(?i)^(\w+(\([^)]*\))?!?:\s*)?(wip|fix(es|ed)?|update[sd]?|change[sd]?|misc|stuff|minor|cleanup|clean up|refactor|tweaks?|temp|tmp|test|changes|(add(ed)?|remove[d]?|update[d]?) (some )?(logs?|logging|code|stuff|changes|files?))\.?$`)
var nonBehaviourType = regexp.MustCompile(`(?i)^(chore|style|docs|test|ci|build)(\(|:|!)`)

// messageProblem explains what is wrong with a commit message, or returns "".
func messageProblem(a Analysis) string {
	subject := strings.TrimSpace(a.Commit.Message)
	var problems []string
	switch {
	case len(subject) < 12:
		problems = append(problems, "the message is too short to tell what changed")
	case genericMessage.MatchString(subject):
		problems = append(problems, "the message is generic and does not say what changed or why")
	}
	if nonBehaviourType.MatchString(subject) {
		for _, f := range a.Files {
			if strings.Contains(f.Path, "src/main/") && f.IsJava() && len(f.AddedLines) > 0 {
				problems = append(problems, fmt.Sprintf("it is typed %q but changes production code (%s)", strings.SplitN(subject, ":", 2)[0], f.Path[strings.LastIndex(f.Path, "/")+1:]))
				break
			}
		}
	}
	if len(problems) == 0 {
		return ""
	}
	return strings.ToUpper(problems[0][:1]) + strings.Join(problems, "; ")[1:] + "."
}

// fixMessageType replaces a chore/style/docs type the model kept although production code changed:
// fix when the review found bugs the commit introduced, refactor otherwise.
func fixMessageType(msg string, a Analysis, files []FileReview) string {
	if msg == "" || !nonBehaviourType.MatchString(msg) {
		return msg
	}
	prod := false
	for _, f := range a.Files {
		prod = prod || (strings.Contains(f.Path, "src/main/") && f.IsJava() && len(f.AddedLines) > 0)
	}
	if !prod {
		return msg
	}
	kind := "refactor"
	for _, f := range files {
		for _, x := range f.Findings {
			if x.Category == "bug" {
				kind = "fix"
			}
		}
	}
	i := strings.IndexAny(msg, "(:!")
	return kind + msg[i:]
}

// constantChanges reports static final constants whose value this commit changed, and who uses them.
func (s *Service) constantChanges(a Analysis, f gitx.FileChange, in fileInput) []Finding {
	if !f.IsJava() || f.OldPath == "" || len(a.Commit.Parents) == 0 {
		return nil
	}
	old, ok := s.repo.ReadFile(a.Commit.Parents[0], f.OldPath)
	if !ok {
		return nil
	}
	values := func(src, path string) map[string]string {
		out := map[string]string{}
		for _, t := range javaidx.TypesIn([]byte(src), path) {
			for _, m := range t.Members {
				if m.Value != "" {
					out[t.Name+"."+m.Name] = m.Value
				}
			}
		}
		return out
	}
	before, after := values(old, f.OldPath), values(in.src, f.NewPath)
	var out []Finding
	for ref, now := range after {
		was, existed := before[ref]
		if !existed || was == now {
			continue
		}
		typ, name, _ := strings.Cut(ref, ".")
		line := 0
		for i, l := range in.lines {
			if strings.Contains(l, " "+name+" ") || strings.Contains(l, " "+name+"=") {
				line = i + 1
				break
			}
		}
		if line == 0 || !in.changed[line] {
			continue
		}
		var users []string
		rx := regexp.MustCompile(`\b` + regexp.QuoteMeta(ref) + `\b`)
		for p, src := range s.sourcesAt(a.Commit.SHA) {
			if p != f.NewPath && rx.Match(src) {
				users = append(users, strings.TrimSuffix(p[strings.LastIndex(p, "/")+1:], ".java"))
			}
		}
		sort.Strings(users)
		detail := fmt.Sprintf("Every class that reads %s now gets %s instead of %s.", ref, now, was)
		if len(users) > 0 {
			detail += " Used by: " + strings.Join(users, ", ") + ". Check each still expects the new value."
		} else {
			detail += " Nothing else uses it today."
		}
		if strings.HasSuffix(strings.ToLower(typ), "constant") || strings.HasSuffix(strings.ToLower(typ), "constants") {
			detail += " A shared constant whose value names one area (" + now + ") is easy to misuse; prefer one constant per area."
		}
		out = append(out, Finding{Line: line, EndLine: line, Severity: "medium", Category: "bug",
			Title:         fmt.Sprintf("%s changed from %s to %s", ref, was, now),
			Detail:        detail,
			Suggestion:    fmt.Sprintf("If only one class needs %s, add a new constant instead of changing the shared one:\npublic static final String %s_%s = %s;", now, strings.ToUpper(strings.Trim(now, `"`)), name, now),
			OnChangedLine: true, Source: "rule", Ref: "shared-constant-changed", Snippet: lineAt(in.lines, line)})
	}
	return out
}

// sourcesAt returns every Java file at a commit (cached).
func (s *Service) sourcesAt(sha string) map[string][]byte {
	if v, ok := s.sources.Load(sha); ok {
		return v.(map[string][]byte)
	}
	m := s.repo.JavaFilesAt(sha)
	s.sources.Store(sha, m)
	return m
}

// ruleRisk is the lowest risk consistent with the findings; the model may raise it but not lower it.
func ruleRisk(files []FileReview) string {
	risk := "low"
	for _, f := range files {
		for _, x := range f.Findings {
			switch x.Severity {
			case "critical":
				return "high"
			case "high":
				risk = "medium"
			}
		}
	}
	return risk
}

// fileInput is what both passes need about one changed file.
type fileInput struct {
	src     string
	lines   []string
	changed map[int]bool
	added   string
	types   []javaidx.Node
}

// rulePass runs the deterministic checks: Java rules, knowledge-base detect patterns and build-file
// checks. It is instant, so its findings are shown before the model starts.
func (s *Service) rulePass(a Analysis, f gitx.FileChange, kb *knowledge.Base, build Build) (FileReview, fileInput, bool) {
	fr := FileReview{Path: f.NewPath, Findings: []Finding{}}
	src, ok := s.repo.ReadFile(a.Commit.SHA, f.NewPath)
	if !ok {
		fr.Error = "file not found at this commit"
		return fr, fileInput{}, false
	}
	in := fileInput{src: src, lines: strings.Split(src, "\n"), changed: map[int]bool{}}
	var added strings.Builder
	for _, l := range f.AddedLines {
		in.changed[l] = true
		if l >= 1 && l <= len(in.lines) {
			added.WriteString(in.lines[l-1] + "\n")
		}
	}
	in.added = added.String()

	if f.IsJava() {
		in.types = javaidx.TypesIn([]byte(src), f.NewPath)
		ctx := javaidx.ProjectContext(s.graph.Graph())
		ctx.Path = f.NewPath
		for _, x := range javaidx.Check([]byte(src), in.changed, ctx) {
			fr.Findings = append(fr.Findings, Finding{
				Line: x.Line, EndLine: x.EndLine, Severity: x.Severity, Category: x.Category, Title: x.Title,
				Detail: x.Detail, Suggestion: x.Suggestion, NodeID: innermost(in.types, x.Line), OnChangedLine: true,
				Source: "rule", Ref: x.ID,
			})
		}
	}
	for _, h := range kb.Detect(f.NewPath, in.lines, in.changed) {
		if slices.ContainsFunc(fr.Findings, func(x Finding) bool { return x.Line == h.Line && x.Ref == h.Rule.ID }) {
			continue
		}
		fr.Findings = append(fr.Findings, Finding{
			Line: h.Line, EndLine: h.Line, Severity: h.Rule.Severity, Category: h.Rule.Category, Title: h.Rule.Title,
			Detail: strings.Join(strings.Fields(h.Rule.Why), " "), Suggestion: strings.TrimRight(h.Rule.Good, "\n"),
			NodeID: innermost(in.types, h.Line), OnChangedLine: true, Source: "rule", Ref: h.Rule.ID,
		})
	}
	if strings.HasSuffix(f.NewPath, "pom.xml") || strings.HasSuffix(f.NewPath, "build.gradle") || strings.HasSuffix(f.NewPath, "build.gradle.kts") {
		if b, ok := ParseBuild(f.NewPath, src); ok {
			fr.Findings = append(fr.Findings, BuildFindings(b, src, in.changed)...)
		}
	}
	fr.Findings = append(fr.Findings, s.constantChanges(a, f, in)...)
	fr.Findings = append(fr.Findings, ConflictMarkers(in.lines, in.changed)...)
	for i := range fr.Findings {
		if fr.Findings[i].Snippet == "" {
			fr.Findings[i].Snippet = lineAt(in.lines, fr.Findings[i].Line)
		}
	}
	return fr, in, true
}

// modelPass asks the model for what rules cannot see, with a prompt that fits the context window.
func (s *Service) modelPass(a Analysis, f gitx.FileChange, fr FileReview, in fileInput, kb *knowledge.Base, build Build) FileReview {
	fr.Pending = false
	if trivial(in.lines, f.AddedLines) {
		fr.Skipped = "only imports, comments or formatting changed"
		return fr
	}
	guidelines := kb.Select(f.NewPath, in.src, in.added, 10)
	prompt := s.fileContext(a, f, in, fr.Findings, guidelines, build)

	system := prompts.Get("review-file") // part of the cache key, so editing the prompt re-reviews
	key := s.cache.key(system, s.llm.Model, prompt)
	out, hit := s.cache.get(key)
	if !hit {
		var err error
		out, err = s.llm.ChatJSON(system, prompt)
		if err != nil {
			fr.Error = "LLM review failed: " + err.Error()
			return fr
		}
		s.cache.put(key, out)
	} else {
		fr.Skipped = "model answer reused from cache (same code and context)"
	}

	fr.Summary = str(out["summary"])
	raw, _ := out["findings"].([]any)
	for _, item := range raw {
		x, ok := item.(map[string]any)
		if !ok {
			continue
		}
		line := clamp(num(x["line"], 1), len(in.lines))
		end := clamp(max(line, num(x["endLine"], line)), len(in.lines))
		// Findings on blank or brace-only lines are usually hallucinated positions: snap to the
		// nearest real code within two lines, or drop.
		if line = snapToCode(in.lines, line); line == 0 {
			continue
		}
		end = max(end, line)
		// Rules are authoritative for what they check: if the model claims one of those problems
		// and the rule did not fire, the model is wrong.
		if k := ruleCovered(str(x["ref"]), str(x["title"])); k != "" && !slices.ContainsFunc(fr.Findings, func(r Finding) bool { return r.Ref == k }) {
			continue
		}
		// A rule already explains this precisely (same lines, or the same rule anywhere in the
		// file, since the model is often a line or two off); drop the model's echo of it.
		ref := str(x["ref"])
		if slices.ContainsFunc(fr.Findings, func(r Finding) bool {
			return r.Source == "rule" && ((r.Line <= end+1 && r.EndLine >= line-1) || (ref != "" && r.Ref == ref))
		}) {
			continue
		}
		onChange := false
		for l := line - 2; l <= end+2; l++ {
			onChange = onChange || in.changed[l]
		}
		title := str(x["title"])
		if title == "" {
			title = "Issue"
		}
		fr.Findings = append(fr.Findings, Finding{
			Line: line, EndLine: end, Severity: normalise(str(x["severity"]), severities, "info"),
			Category: strings.ToLower(orDefault(str(x["category"]), "design")), Title: title,
			Detail: str(x["detail"]), Suggestion: str(x["suggestion"]), NodeID: innermost(in.types, line),
			OnChangedLine: onChange, Source: "llm", Ref: str(x["ref"]), Snippet: lineAt(in.lines, line),
		})
	}
	sort.SliceStable(fr.Findings, func(i, j int) bool {
		si, sj := slices.Index(severities, fr.Findings[i].Severity), slices.Index(severities, fr.Findings[j].Severity)
		if si != sj {
			return si < sj
		}
		return fr.Findings[i].Line < fr.Findings[j].Line
	})
	return fr
}

// deterministic lists the rule ids whose checks are exhaustive for their pattern.
var deterministic = map[string]string{
	"uninjected-dependency": "inject", "console-output": "system.out", "optional-get-unchecked": "optional.get",
	"sql-injection": "concatenat", "bigdecimal-double": "bigdecimal(double", "swallowed-exception": "empty catch",
	"tx-self-invocation": "self-invocation", "field-injection": "field injection", "literal-instead-of-constant": "duplicates constant",
}

// ruleCovered returns the rule id a model finding claims, when a deterministic rule owns it.
func ruleCovered(ref, title string) string {
	if _, ok := deterministic[ref]; ok {
		return ref
	}
	t := strings.ToLower(title)
	for id, kw := range deterministic {
		if strings.Contains(t, kw) || (id == "uninjected-dependency" && strings.Contains(t, "uninjected")) {
			return id
		}
	}
	return ""
}

func snapToCode(lines []string, n int) int {
	meaningful := func(i int) bool {
		if i < 1 || i > len(lines) {
			return false
		}
		t := strings.TrimSpace(lines[i-1])
		return t != "" && t != "{" && t != "}" && t != "};"
	}
	for _, d := range []int{0, 1, -1, 2, -2} {
		if meaningful(n + d) {
			return n + d
		}
	}
	return 0
}

func lineAt(lines []string, n int) string {
	if n < 1 || n > len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[n-1])
}

// fileContext builds the per-file prompt. Sections are ranked so that, when the file is large, the
// diff and the changed code always fit and background (overview, history) is trimmed first.
func (s *Service) fileContext(a Analysis, f gitx.FileChange, in fileInput, known []Finding, guidelines []knowledge.Rule, build Build) string {
	var sec []section
	add := func(title, body string, priority, keep int) {
		if strings.TrimSpace(body) != "" {
			sec = append(sec, section{title: title, body: body, priority: priority, keep: keep})
		}
	}

	overview := s.overview()
	if sum := build.Summary(); sum != "" {
		overview = "Build: " + sum + "\n" + overview
	}
	add("## Project overview", overview, 7, 300)
	add("## Commit", strings.TrimSpace(a.Commit.FullMessage)+"\n", 6, 200)

	if len(in.types) > 0 {
		own := map[string]bool{}
		for _, t := range in.types {
			own[t.ID] = true
		}
		injected := map[string]bool{}
		for _, e := range s.graph.Graph().Edges {
			if own[e.Source] && e.Kind == "injects" {
				injected[e.Target] = true
			}
		}
		seen := map[string]bool{}
		var related []javaidx.Node
		for _, t := range in.types {
			for _, n := range s.graph.Neighbours(t.ID) {
				if !own[n.ID] && !seen[n.ID] {
					seen[n.ID] = true
					related = append(related, n)
				}
			}
		}
		var b strings.Builder
		for i, n := range rankRelated(related, injected, in.added) {
			if i == 14 {
				break
			}
			fmt.Fprintf(&b, "[%s %s] %s\n", n.Stereotype, n.Kind, n.ID)
			for j, m := range n.Members {
				if j == 12 {
					break
				}
				b.WriteString("    " + m.Signature)
				if m.Value != "" {
					b.WriteString(" = " + m.Value)
				}
				b.WriteString("\n")
			}
		}
		add("## Related types, most relevant first (signatures only)", b.String(), 4, 600)

		var deps []string
		for _, id := range a.Impacted {
			for _, n := range s.graph.Neighbours(id) {
				if own[n.ID] {
					deps = append(deps, id)
					break
				}
			}
		}
		add("## Types that depend on this file (may break)", strings.Join(deps, ", ")+"\n", 5, 0)
	}

	if len(guidelines) > 0 {
		add("## Knowledge: bug patterns that resemble this code (reference, not a checklist)", knowledge.Format(guidelines), 3, 1500)
	}
	if len(known) > 0 {
		var b strings.Builder
		for _, x := range known {
			fmt.Fprintf(&b, "- line %d: %s\n", x.Line, x.Title)
		}
		add("## Already found by static analysis (do not repeat these; look for other problems)", b.String(), 2, 400)
	}
	if s.Ignores != nil {
		if ig := s.Ignores.ForPrompt(f.NewPath); len(ig) > 0 {
			add("## Marked intentional by the team (do NOT report these)", strings.Join(ig, "\n")+"\n", 2, 300)
		}
	}
	add("## Earlier findings in this file (they may already be fixed: report one only if the problem is still in the code below)", s.history(f.NewPath, a.Commit.SHA, 3), 6, 0)
	if others := otherChanges(a, f.Path, 6000); others != "" {
		add("## Other changes in this commit (for cross-file mistakes)", "```diff\n"+others+"```\n", 5, 800)
	}
	add("## Diff", "```diff\n"+f.Patch+"\n```\n", 1, 4000)
	add("## File "+f.NewPath+" after the commit (\">>\" = changed line)", "```\n"+numbered(in.lines, f.AddedLines, s.cfg.MaxFileLines)+"```\n", 1, 6000)

	// ~3 characters per token for code; keep room for the system prompt and the answer.
	budget := max(12000, (s.cfg.NumCtx-2500)*3)
	return assemble(sec, budget)
}

func (s *Service) overview() string {
	g := s.graph.Graph()
	by := map[string][]string{}
	var endpoints []string
	for _, n := range g.Nodes {
		by[n.Stereotype] = append(by[n.Stereotype], n.Name)
		endpoints = append(endpoints, n.Endpoints...)
	}
	keys := make([]string, 0, len(by))
	for k := range by {
		if k != "other" && k != "test" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	fmt.Fprintf(&b, "%d types. ", len(g.Nodes))
	for _, k := range keys {
		names := by[k]
		more := ""
		if len(names) > 15 {
			names, more = names[:15], " …"
		}
		fmt.Fprintf(&b, "%s: %s%s. ", k, strings.Join(names, ", "), more)
	}
	if len(endpoints) > 20 {
		endpoints = endpoints[:20]
	}
	if len(endpoints) > 0 {
		b.WriteString("\nEndpoints: " + strings.Join(endpoints, "; "))
	}
	return b.String() + "\n"
}

// otherChanges returns the added and removed lines of the commit's other source files, within budget bytes.
func otherChanges(a Analysis, self string, budget int) string {
	var b strings.Builder
	for _, f := range a.Files {
		if f.Path == self || f.Binary || !slices.ContainsFunc(reviewable, func(ext string) bool { return strings.HasSuffix(f.Path, ext) }) {
			continue
		}
		var part strings.Builder
		part.WriteString("--- " + f.Path + "\n")
		for _, line := range strings.Split(f.Patch, "\n") {
			if (strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-")) && !strings.HasPrefix(line, "+++") && !strings.HasPrefix(line, "---") {
				part.WriteString(line + "\n")
			}
		}
		if b.Len()+part.Len() > budget {
			b.WriteString("… (more files omitted)\n")
			break
		}
		b.WriteString(part.String())
	}
	return b.String()
}

func commitContext(a Analysis, files []FileReview) string {
	var b strings.Builder
	b.WriteString("Commit message: " + strings.TrimSpace(a.Commit.FullMessage) + "\n\nFiles:\n")
	for _, f := range a.Files {
		fmt.Fprintf(&b, "- %s %s (+%d lines)\n", f.ChangeType, f.Path, len(f.AddedLines))
	}
	var changed []string
	for _, c := range a.Changed {
		changed = append(changed, c.ID)
	}
	fmt.Fprintf(&b, "\nChanged types: %s\nTypes that depend on them: %s\n\nPer-file reviews:\n",
		strings.Join(changed, ", "), strings.Join(a.Impacted, ", "))
	for _, f := range files {
		b.WriteString("### " + f.Path + "\n" + orDefault(f.Summary, "(no summary)") + "\n")
		for _, x := range f.Findings {
			fmt.Fprintf(&b, "- [%s] line %d: %s\n", x.Severity, x.Line, x.Title)
		}
	}
	return b.String()
}

// numbered prefixes every line with its number and marks changed ones; long files keep only
// windows of 40 lines around the changes.
func numbered(lines []string, changed []int, maxLines int) string {
	isChanged := map[int]bool{}
	for _, l := range changed {
		isChanged[l] = true
	}
	near := func(n int) bool {
		for _, c := range changed {
			if c-n <= 40 && n-c <= 40 {
				return true
			}
		}
		return false
	}
	window := len(lines) > maxLines
	var b strings.Builder
	gap := false
	for i, text := range lines {
		n := i + 1
		if window && !near(n) {
			if !gap {
				b.WriteString("      ...\n")
			}
			gap = true
			continue
		}
		gap = false
		mark := "   "
		if isChanged[n] {
			mark = ">> "
		}
		fmt.Fprintf(&b, "%s%4d | %s\n", mark, n, text)
	}
	return b.String()
}

func innermost(types []javaidx.Node, line int) string {
	best := ""
	size := 1 << 30
	for _, t := range types {
		if line >= t.StartLine && line <= t.EndLine && t.LOC() < size {
			best, size = t.ID, t.LOC()
		}
	}
	if best == "" && len(types) > 0 {
		return types[0].ID
	}
	return best
}

func normalise(v string, allowed []string, def string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if slices.Contains(allowed, v) {
		return v
	}
	return def
}

func clamp(line, n int) int {
	return max(1, min(line, max(1, n)))
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
