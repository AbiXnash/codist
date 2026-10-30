package review

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"codist/internal/gitx"
	"codist/internal/javaidx"
	"codist/prompts"
)

// Ask answers a question about a file (or the whole project when path is empty).
func (x *Fixer) Ask(path, question, context string) (string, error) {
	var user strings.Builder
	user.WriteString(context)
	if path != "" {
		content, ok, err := x.repo.ReadWorking(path)
		if err == nil && ok {
			user.WriteString("\n## File " + path + "\n")
			for i, l := range strings.Split(content, "\n") {
				fmt.Fprintf(&user, "%4d | %s\n", i+1, l)
			}
		}
	}
	user.WriteString("\n## Question\n" + question + "\n")
	answer, err := x.ask.ChatText(prompts.Get("ask"), user.String())
	if err != nil {
		return "", err
	}
	// The file is shown numbered; drop numbers the model copies into its code blocks.
	return fence.ReplaceAllStringFunc(answer, func(block string) string {
		nl := strings.IndexByte(block, '\n')
		return block[:nl+1] + stripLineNumbers(block[nl+1:])
	}), nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

func trimTo(s string, n int) string {
	if len(s) > n {
		return s[:n-1] + "…"
	}
	return s
}

// Proposal is a generated fix waiting for the user's approval. A grouped issue can span several
// files; they are committed together.
type Proposal struct {
	ID      string   `json:"id"`
	SHA     string   `json:"sha"`
	Path    string   `json:"path"`  // first file, for single-file fixes
	Paths   []string `json:"paths"` // every file the fix touches
	Finding Finding  `json:"finding"`
	Patch   string   `json:"patch"`
	Message string   `json:"message"`
	Branch  string   `json:"branch"`
	Working bool     `json:"working"` // fixes uncommitted changes: applied to the working tree, not committed
	edits   []edit
	created time.Time
}

type edit struct {
	path, original, proposed string
	created                  bool // a new file (regression test, runbook)
}

// Fixer turns findings into patches and commits them once approved.
type Fixer struct {
	repo    *gitx.Repo
	llm     *Ollama // fixes and edits
	ask     *Ollama // questions
	store   *Store
	Ignores *Ignores

	mu         sync.Mutex
	proposals  map[string]*Proposal
	prefetched map[string]*Proposal
	prefetchQ  chan string

	Known   func() map[string]bool // fully qualified names of the project's types
	Rules   RuleContext            // re-runs the rules, so fixes are not proposed for issues already gone
	Busy    func() bool            // true while reviews are running; prefetch waits
	OnReady func(sha, key string)  // a prefetched fix became available
}

func NewFixer(repo *gitx.Repo, fix, ask *Ollama, store *Store) *Fixer {
	x := &Fixer{repo: repo, llm: fix, ask: ask, store: store, proposals: map[string]*Proposal{},
		prefetched: map[string]*Proposal{}, prefetchQ: make(chan string, 64)}
	go x.prefetchWorker()
	return x
}

// Enabled is false when Codist follows a remote mirror: there is no working tree of yours to change.
func (x *Fixer) Enabled() bool { return !x.repo.Remote }

var fence = regexp.MustCompile("(?s)```[a-zA-Z]*\\n(.*?)```")

func (x *Fixer) ready() error {
	if !x.Enabled() {
		return errors.New("fixes need a local checkout (CODIST_REPO); this repository is a remote mirror")
	}
	if b := x.repo.CurrentBranch(); b != x.repo.Branch {
		return fmt.Errorf("the working tree is on %q but Codist follows %q; switch back to commit fixes", b, x.repo.Branch)
	}
	return nil
}

// Propose fixes one finding, or the whole group it belongs to (every file, one commit).
// It works on the working tree as it is now: each place is found again by its line of code, and
// places that no longer exist (already fixed) are skipped. Prefetched and mechanical fixes return
// immediately; only the rest wait for the model.
func (x *Fixer) Propose(sha, path string, line int, title string) (*Proposal, error) {
	if err := x.ready(); err != nil {
		return nil, err
	}
	key := fixKey(sha, path, line, title)
	if p := x.takePrefetched(key); p != nil {
		return p, nil
	}
	plan, err := x.plan(sha, path, line, title)
	if err != nil {
		return nil, err
	}
	if why, ok := notFixable[plan.target.Ref]; ok {
		return nil, errors.New(why)
	}
	p, err := x.build(plan)
	if err == nil && isWorking(sha) {
		p.Working = true
		p.Message = strings.Replace(p.Message, "Found by Codist reviewing working", "Found by Codist in your uncommitted changes", 1)
	}
	return p, err
}

// fixPlan is everything needed to produce a fix for one issue.
type fixPlan struct {
	sha     string
	target  Finding
	members []placed
	current map[string]string // working-tree content of each file
}

func (x *Fixer) plan(sha, path string, line int, title string) (*fixPlan, error) {
	rv, ok := x.store.Get(sha)
	if isWorking(sha) {
		rv, ok = x.workingReview(), true
	}
	if !ok {
		return nil, errors.New("commit has not been reviewed")
	}
	if x.Ignores != nil {
		rv = x.Ignores.Decorate(rv)
	}
	rv = AssignGroups(rv)
	target, ok := findFinding(rv, path, line, title)
	if !ok {
		return nil, errors.New("finding not found in the review")
	}
	var members []placed
	for _, fr := range rv.Files {
		for _, f := range fr.Findings {
			if (target.Group != "" && f.Group == target.Group) || (target.Group == "" && fr.Path == path && f.Line == line && f.Title == title) {
				members = append(members, placed{fr.Path, f})
			}
		}
	}
	current := map[string]string{}
	for _, m := range members {
		if _, ok := current[m.path]; ok {
			continue
		}
		if src, exists, err := x.repo.ReadWorking(m.path); err == nil && exists {
			current[m.path] = src
		}
	}
	members = relocate(members, current)
	members = x.stillFlagged(members, current)
	if len(members) == 0 {
		return nil, errors.New("this issue is already fixed in the working tree")
	}
	return &fixPlan{sha: rv.SHA, target: target, members: members, current: current}, nil
}

// stillFlagged drops rule findings the rules no longer report on the current code. The flagged line
// can survive a fix made elsewhere, e.g. the constant it reads was given the right value.
func (x *Fixer) stillFlagged(members []placed, current map[string]string) []placed {
	if x.Rules == nil {
		return members
	}
	ctx, kb, ok := x.Rules()
	if !ok {
		return members
	}
	scans := map[string][]Finding{}
	var out []placed
	gone, live := map[string]bool{}, map[string]bool{}
	for _, m := range members {
		if !Recheckable(m.path, m.f) {
			out = append(out, m)
			continue
		}
		scan, done := scans[m.path]
		if !done {
			scan = RuleScan(m.path, []byte(current[m.path]), ctx, kb)
			scans[m.path] = scan
		}
		if fires(scan, m.f.Ref, m.f.Line) {
			out = append(out, m)
			live[m.f.Ref] = true
		} else {
			gone[m.f.Ref] = true
		}
	}
	// Model findings on a rule's pattern go with the rule: once it no longer fires, they are moot.
	// Without the rule's own findings in the group, ask whether the rule fires anywhere in the project.
	var project map[string]bool
	firesAnywhere := func(ref string) bool {
		if project == nil {
			project = map[string]bool{}
			for p, src := range x.repo.JavaFilesWorking() {
				for _, f := range RuleScan(p, src, ctx, kb) {
					project[f.Ref] = true
				}
			}
		}
		return project[ref]
	}
	kept := out[:0]
	for _, m := range out {
		if m.f.Source != "rule" {
			owned := m.f.Ref
			if !gone[owned] && !live[owned] {
				owned = ruleCovered(m.f.Ref, m.f.Title)
			}
			if owned != "" && !live[owned] && (gone[owned] || (ruleCovered(owned, "") != "" && !firesAnywhere(owned))) {
				continue
			}
		}
		kept = append(kept, m)
	}
	return kept
}

// Instant reports whether a finding's issue has a mechanical fix (no model needed).
func Instant(f Finding) bool {
	return mechanical[f.Ref] || f.Ref == "copy-paste-wrong-constant"
}

func (x *Fixer) build(p *fixPlan) (*Proposal, error) {
	members, current, target := p.members, p.current, p.target
	var files []string
	byFile := map[string][]Finding{}
	for _, m := range members {
		if _, seen := byFile[m.path]; !seen {
			files = append(files, m.path)
		}
		byFile[m.path] = append(byFile[m.path], m.f)
	}

	// Rules with a mechanical fix skip the model entirely.
	if edits, msg, ok := x.autofixWrongConstant(members); ok {
		return x.store_(p.sha, target, msg, edits)
	}
	if edits, ok := x.autofixMechanical(members, current); ok {
		msg := commitMessage(target, files[0], p.sha)
		if len(members) > 1 {
			msg = strings.Replace(msg, "\n\n", fmt.Sprintf("\n\nFixes %d places in %d file(s).\n", len(members), len(files)), 1)
		}
		return x.store_(p.sha, target, msg, edits)
	}

	var overview strings.Builder
	if len(members) > 1 {
		fmt.Fprintf(&overview, "## One issue in %d places (fix it consistently)\n", len(members))
		for _, m := range members {
			fmt.Fprintf(&overview, "- %s:%d %s\n", m.path, m.f.Line, m.f.Title)
			if m.f.Suggestion != "" {
				fmt.Fprintf(&overview, "  suggested: %s\n", strings.Join(strings.Fields(trimTo(m.f.Suggestion, 300)), " "))
			}
		}
	}

	var edits []edit
	var done strings.Builder // diffs of files already fixed, shown to the next ones
	for _, file := range files {
		cur := current[file]
		var user strings.Builder
		user.WriteString(overview.String())
		for _, f := range byFile[file] {
			fmt.Fprintf(&user, "\n## Finding to fix in this file (%s, %s) at lines %d-%d\n%s\n%s\n", f.Severity, f.Category, f.Line, f.EndLine, f.Title, f.Detail)
			if f.Suggestion != "" {
				user.WriteString("Suggested fix:\n" + f.Suggestion + "\n")
			}
		}
		if done.Len() > 0 {
			user.WriteString("\n## Changes already made to other files for this issue (stay consistent, e.g. use new names)\n```diff\n" + done.String() + "```\n")
		}
		proposed, patch, err := x.generate(prompts.Get("fix"), user.String(), file, cur)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		// Do not hand the user a fix that breaks other files or adds a constant that already exists:
		// retry once with the reason, then refuse.
		if broken := x.brokenReferences(file, cur, proposed, files); len(broken) > 0 {
			retry := user.String() + "\n" + prompts.Render("retry-broken-references", prompts.Items(broken)) + "\n"
			if proposed, patch, err = x.generate(prompts.Get("fix"), retry, file, cur); err != nil {
				return nil, fmt.Errorf("%s: %w", file, err)
			}
			if broken = x.brokenReferences(file, cur, proposed, files); len(broken) > 0 {
				return nil, fmt.Errorf("the model's fix would break other code (%s); nothing was changed, fix it by hand or try again", strings.Join(broken, "; "))
			}
		}
		if dups := duplicateConstants(cur, proposed); len(dups) > 0 {
			retry := user.String() + "\n" + prompts.Render("retry-duplicate-constants", prompts.Items(dups)) + "\n"
			if proposed, patch, err = x.generate(prompts.Get("fix"), retry, file, cur); err != nil {
				return nil, fmt.Errorf("%s: %w", file, err)
			}
			if dups = duplicateConstants(cur, proposed); len(dups) > 0 {
				return nil, fmt.Errorf("the model's fix adds a constant that already exists (%s); nothing was changed", strings.Join(dups, "; "))
			}
		}
		edits = append(edits, edit{path: file, original: cur, proposed: proposed})
		done.WriteString(patch)
	}

	msg := commitMessage(target, files[0], p.sha)
	if len(files) > 1 {
		msg = strings.Replace(msg, "\n\n", fmt.Sprintf("\n\nFixes %d places in %d files.\n", len(members), len(files)), 1)
	}
	return x.store_(p.sha, target, msg, edits)
}

func fixKey(sha, path string, line int, title string) string {
	return fmt.Sprintf("%s|%s|%d|%s", sha, path, line, title)
}

// Prefetch prepares model fixes for every issue of a reviewed commit in the background, so Fix
// opens instantly. Mechanical fixes need no preparation. It runs one at a time and waits while
// reviews are running, so it never slows a review down.
func (x *Fixer) Prefetch(sha string) {
	if !x.Enabled() || x.Busy == nil {
		return
	}
	rv, ok := x.store.Get(sha)
	if !ok || rv.Status != StatusDone {
		return
	}
	if x.Ignores != nil {
		rv = x.Ignores.Decorate(rv)
	}
	rv = AssignGroups(rv)
	for _, fr := range rv.Files {
		if !strings.HasSuffix(fr.Path, ".java") {
			continue
		}
		for _, f := range fr.Findings {
			if f.Ignored || (f.Group != "" && !f.GroupRoot) || Instant(f) {
				continue
			}
			select {
			case x.prefetchQ <- fixKey(rv.SHA, fr.Path, f.Line, f.Title):
			default: // queue full: skip, Fix will generate on demand
			}
		}
	}
}

func (x *Fixer) prefetchWorker() {
	for key := range x.prefetchQ {
		for x.Busy() {
			time.Sleep(3 * time.Second)
		}
		parts := strings.SplitN(key, "|", 4)
		var line int
		fmt.Sscan(parts[2], &line)
		plan, err := x.plan(parts[0], parts[1], line, parts[3])
		if err != nil {
			continue
		}
		p, err := x.build(plan)
		if err != nil {
			continue
		}
		x.mu.Lock()
		x.prefetched[key] = p
		x.mu.Unlock()
		if x.OnReady != nil {
			x.OnReady(parts[0], key)
		}
	}
}

// takePrefetched returns a prepared fix if its files have not changed since it was made.
func (x *Fixer) takePrefetched(key string) *Proposal {
	x.mu.Lock()
	p, ok := x.prefetched[key]
	if ok {
		delete(x.prefetched, key)
	}
	x.mu.Unlock()
	if !ok {
		return nil
	}
	for _, e := range p.edits {
		if cur, _, err := x.repo.ReadWorking(e.path); err != nil || cur != e.original {
			return nil
		}
	}
	return p
}

// Ready lists the issues of a commit whose fix is available instantly: "instant" for mechanical
// fixes, "ready" for prefetched model fixes. Keys are path|line|title.
func (x *Fixer) Ready(sha string) map[string]string {
	out := map[string]string{}
	x.mu.Lock()
	for key := range x.prefetched {
		if strings.HasPrefix(key, sha+"|") {
			out[strings.TrimPrefix(key, sha+"|")] = "ready"
		}
	}
	x.mu.Unlock()
	return out
}

// ProposeEdit applies a free-form instruction from the Ask tab to the working-tree file.
func (x *Fixer) ProposeEdit(path, instruction string) (*Proposal, error) {
	if err := x.ready(); err != nil {
		return nil, err
	}
	current, exists, err := x.repo.ReadWorking(path)
	if err != nil || !exists {
		return nil, fmt.Errorf("%s does not exist in the working tree", path)
	}
	proposed, _, err := x.generate(prompts.Get("edit"), "## Change requested by the developer\n"+instruction+"\n", path, current)
	if err != nil {
		return nil, err
	}
	file := strings.TrimSuffix(path[strings.LastIndex(path, "/")+1:], ".java")
	msg := fmt.Sprintf("refactor(%s): %s\n\nRequested in Codist: %s", file, lowerFirst(trimTo(firstLine(instruction), 60)), instruction)
	f := Finding{Title: firstLine(instruction), Detail: instruction, Severity: "info", Category: "edit"}
	return x.store_("", f, msg, []edit{{path: path, original: current, proposed: proposed}})
}

// generate asks the model for the whole updated file and checks it before anyone sees it.
func (x *Fixer) generate(system, task, path, current string) (string, string, error) {
	answer, err := x.llm.ChatText(system, task+fileBlock(path, current))
	if err != nil {
		return "", "", err
	}
	m := fence.FindStringSubmatch(answer)
	if m == nil {
		return "", "", errors.New("the model did not return a code block; try again")
	}
	proposed := stripLineNumbers(m[1])
	if strings.HasSuffix(current, "\n") && !strings.HasSuffix(proposed, "\n") {
		proposed += "\n"
	}
	if proposed == current {
		return "", "", errors.New("the model returned the file unchanged; try again or fix it by hand")
	}
	if strings.HasSuffix(path, ".java") && javaidx.Valid([]byte(current)) && !javaidx.Valid([]byte(proposed)) {
		return "", "", errors.New("the model's version does not parse as Java; nothing was changed, try again")
	}
	if lc, lp := strings.Count(current, "\n"), strings.Count(proposed, "\n"); lp < lc/2 {
		return "", "", errors.New("the model's version dropped most of the file; nothing was changed, try again")
	}
	var bad []string
	if proposed, bad = x.checkImports(current, proposed); len(bad) > 0 {
		retry := task + "\n" + prompts.Render("retry-bad-imports", prompts.Items(bad)) + "\n"
		if answer, err = x.llm.ChatText(system, retry+fileBlock(path, current)); err != nil {
			return "", "", err
		}
		if m = fence.FindStringSubmatch(answer); m == nil {
			return "", "", errors.New("the model did not return a code block; try again")
		}
		proposed = stripLineNumbers(m[1])
		if strings.HasSuffix(current, "\n") && !strings.HasSuffix(proposed, "\n") {
			proposed += "\n"
		}
		if proposed, bad = x.checkImports(current, proposed); len(bad) > 0 {
			return "", "", fmt.Errorf("the model's fix imports classes that do not exist (%s); nothing was changed", strings.Join(bad, ", "))
		}
		if strings.HasSuffix(path, ".java") && !javaidx.Valid([]byte(proposed)) {
			return "", "", errors.New("the model's version does not parse as Java; nothing was changed, try again")
		}
	}
	patch, err := gitx.UnifiedDiff(path, current, proposed)
	return proposed, patch, err
}

func (x *Fixer) store_(sha string, f Finding, msg string, edits []edit) (*Proposal, error) {
	var patch strings.Builder
	var paths []string
	for _, e := range edits {
		d, err := gitx.UnifiedDiff(e.path, e.original, e.proposed)
		if err != nil {
			return nil, err
		}
		patch.WriteString(d)
		paths = append(paths, e.path)
	}
	p := &Proposal{ID: newID(), SHA: sha, Path: paths[0], Paths: paths, Finding: f, Patch: patch.String(),
		Message: msg, Branch: x.repo.Branch, edits: edits, created: time.Now()}
	x.mu.Lock()
	for id, old := range x.proposals {
		if time.Since(old.created) > time.Hour {
			delete(x.proposals, id)
		}
	}
	x.proposals[p.ID] = p
	x.mu.Unlock()
	return p, nil
}

// Apply writes every file of the approved fix and commits exactly those files.
func (x *Fixer) Apply(id, message string) (string, error) {
	x.mu.Lock()
	p, ok := x.proposals[id]
	if ok {
		delete(x.proposals, id)
	}
	x.mu.Unlock()
	if !ok {
		return "", errors.New("this fix expired; generate it again")
	}
	if b := x.repo.CurrentBranch(); b != p.Branch {
		return "", fmt.Errorf("the working tree is on %q, not %q", b, p.Branch)
	}
	for _, e := range p.edits {
		if e.created {
			if x.repo.ExistsWorking(e.path) {
				return "", fmt.Errorf("%s was created meanwhile; nothing was changed", e.path)
			}
			continue
		}
		current, _, err := x.repo.ReadWorking(e.path)
		if err != nil {
			return "", err
		}
		if current != e.original {
			return "", fmt.Errorf("%s was edited after the fix was generated; nothing was changed", e.path)
		}
	}
	if strings.TrimSpace(message) == "" {
		message = p.Message
	}
	restore := func() {
		for _, e := range p.edits {
			if e.created {
				x.repo.RemoveNew(e.path)
			} else {
				_ = x.repo.WriteWorking(e.path, e.original)
			}
		}
	}
	for _, e := range p.edits {
		var err error
		if e.created {
			err = x.repo.CreateWorking(e.path, e.proposed)
		} else {
			err = x.repo.WriteWorking(e.path, e.proposed)
		}
		if err != nil {
			restore()
			return "", err
		}
	}
	if p.Working {
		return "", nil // your changes are not committed yet, so neither is the fix: commit them together
	}
	sha, err := x.repo.CommitFiles(p.Paths, message)
	if err != nil {
		restore() // leave the tree as it was
		return "", err
	}
	return sha, nil
}

func (x *Fixer) Discard(id string) {
	x.mu.Lock()
	delete(x.proposals, id)
	x.mu.Unlock()
}

func findFinding(rv Review, path string, line int, title string) (Finding, bool) {
	for _, f := range rv.Files {
		if f.Path != path {
			continue
		}
		for _, x := range f.Findings {
			if x.Line == line && x.Title == title {
				return x, true
			}
		}
	}
	return Finding{}, false
}

var numberedLine = regexp.MustCompile(`(?m)^\s{0,4}\d{1,5} \| `)

// stripLineNumbers removes "  12 | " prefixes if the model echoed the numbered input.
func stripLineNumbers(s string) string {
	lines := strings.Split(s, "\n")
	hits := 0
	for _, l := range lines {
		if numberedLine.MatchString(l) {
			hits++
		}
	}
	if hits < len(lines)/2 {
		return s
	}
	return numberedLine.ReplaceAllString(s, "")
}

func commitMessage(f Finding, path, sha string) string {
	title := f.Title
	if len(title) > 60 {
		title = title[:57] + "…"
	}
	file := path[strings.LastIndex(path, "/")+1:]
	msg := fmt.Sprintf("fix(%s): %s\n\n%s\n\nFound by Codist reviewing %s", strings.TrimSuffix(file, ".java"), lowerFirst(title), f.Detail, shortSHA(sha))
	if f.Ref != "" {
		msg += " (guideline " + f.Ref + ")"
	}
	return msg + "."
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// brokenReferences lists members the proposed version removed that other files (outside the fix)
// still reference, e.g. DataConstant.LOG_PREFIX used in CustomerService.java.
func (x *Fixer) brokenReferences(path, before, after string, editing []string) []string {
	if !strings.HasSuffix(path, ".java") {
		return nil
	}
	members := func(src string) map[string]string { // "Type.member" -> type
		out := map[string]string{}
		for _, t := range javaidx.TypesIn([]byte(src), path) {
			for _, m := range t.Members {
				if strings.Contains(m.Signature, "private ") {
					continue
				}
				for _, name := range strings.Split(m.Name, ", ") {
					out[t.Name+"."+name] = t.Name
				}
			}
		}
		return out
	}
	old, kept := members(before), members(after)
	var removed []string
	for ref := range old {
		if _, ok := kept[ref]; !ok {
			removed = append(removed, ref)
		}
	}
	if len(removed) == 0 {
		return nil
	}
	sort.Strings(removed)
	var out []string
	for file, src := range x.repo.JavaFilesWorking() {
		if file == path || slices.Contains(editing, file) {
			continue
		}
		text := string(src)
		for _, ref := range removed {
			typ, name, _ := strings.Cut(ref, ".")
			direct := regexp.MustCompile(`\b` + regexp.QuoteMeta(ref) + `\b`).MatchString(text)
			// Instance methods: a call to .name( in a file that mentions the type.
			viaType := strings.Contains(text, typ) && regexp.MustCompile(`\.`+regexp.QuoteMeta(name)+`\(`).MatchString(text)
			if direct || viaType {
				out = append(out, fmt.Sprintf("%s was removed but %s still uses it", ref, file[strings.LastIndex(file, "/")+1:]))
			}
		}
	}
	sort.Strings(out)
	return out
}

var assertsException = regexp.MustCompile(`(assertThrows\(\s*|isInstanceOf\(\s*|expected\s*=\s*|assertThatExceptionOfType\(\s*)(?:[\w.]+\.)?(\w+Exception)\.class`)

// testProblems lists reasons a generated regression test is wrong.
func testProblems(test, forbid string) []string {
	var out []string
	if forbid != "" {
		for _, m := range assertsException.FindAllStringSubmatch(test, -1) {
			if m[2] == forbid {
				out = append(out, "it asserts that "+forbid+" is thrown, which is the defect itself; assert the correct result instead")
			}
		}
	}
	if strings.Contains(test, "@InjectMocks") {
		out = append(out, "it uses @InjectMocks, which hides injection defects; construct the class with new and pass the mocks")
	}
	return out
}

// ProposeTest writes a regression test for a defect as a new file, shown as a diff to approve.
// forbid is the exception the defect throws: a test asserting it would lock the bug in.
func (x *Fixer) ProposeTest(sourcePath, defect, build string, forbid ...string) (*Proposal, error) {
	if err := x.ready(); err != nil {
		return nil, err
	}
	src, ok, err := x.repo.ReadWorking(sourcePath)
	if err != nil || !ok {
		return nil, fmt.Errorf("%s does not exist in the working tree", sourcePath)
	}
	if !strings.Contains(sourcePath, "src/main/java/") || !strings.HasSuffix(sourcePath, ".java") {
		return nil, errors.New("regression tests are written for classes under src/main/java")
	}
	base := strings.TrimSuffix(strings.Replace(sourcePath, "src/main/java/", "src/test/java/", 1), ".java")
	testPath := base + "RegressionTest.java"
	for i := 2; x.repo.ExistsWorking(testPath); i++ {
		testPath = fmt.Sprintf("%sRegression%dTest.java", base, i)
	}
	cls := testPath[strings.LastIndex(testPath, "/")+1:]
	cls = strings.TrimSuffix(cls, ".java")

	var user strings.Builder
	fmt.Fprintf(&user, "## Defect\n%s\n\n## Build\n%s\n\n## Test class to write\n%s (file %s), same package as the class under test.\n\n## Class under test: %s\n```java\n%s\n```\n",
		defect, build, cls, testPath, sourcePath, src)
	exc := ""
	if len(forbid) > 0 {
		exc = forbid[0][strings.LastIndex(forbid[0], ".")+1:]
	}
	var test string
	for attempt := 0; attempt < 2; attempt++ {
		answer, err := x.llm.ChatText(prompts.Get("regression-test"), user.String())
		if err != nil {
			return nil, err
		}
		m := fence.FindStringSubmatch(answer)
		if m == nil {
			return nil, errors.New("the model did not return a code block; try again")
		}
		test = stripLineNumbers(m[1])
		problems := testProblems(test, exc)
		if len(problems) == 0 {
			break
		}
		if attempt == 1 {
			return nil, errors.New("the generated test is not a valid regression test (" + strings.Join(problems, "; ") + "); nothing was changed, try again")
		}
		user.WriteString("\n" + prompts.Render("retry-bad-test", prompts.Items(problems)) + "\n")
	}
	if !strings.HasSuffix(test, "\n") {
		test += "\n"
	}
	if !javaidx.Valid([]byte(test)) {
		return nil, errors.New("the generated test does not parse as Java; nothing was changed, try again")
	}
	if !strings.Contains(test, "@Test") {
		return nil, errors.New("the model did not write a test method; try again")
	}
	// Name the class after the file, whatever the model chose.
	test = regexp.MustCompile(`\b(class\s+)\w+`).ReplaceAllString(test, "${1}"+cls)
	f := Finding{Title: "Regression test for " + firstLine(defect), Detail: defect, Severity: "info", Category: "test"}
	file := strings.TrimSuffix(sourcePath[strings.LastIndex(sourcePath, "/")+1:], ".java")
	msg := fmt.Sprintf("test(%s): add regression test\n\nReproduces: %s\n\nWritten by Codist.", file, trimTo(firstLine(defect), 200))
	return x.store_("", f, msg, []edit{{path: testPath, proposed: test, created: true}})
}

// ProposeNewFile proposes adding a file (a runbook, a report) as an approved commit.
func (x *Fixer) ProposeNewFile(path, content, message, title string) (*Proposal, error) {
	if err := x.ready(); err != nil {
		return nil, err
	}
	if x.repo.ExistsWorking(path) {
		return nil, fmt.Errorf("%s already exists", path)
	}
	f := Finding{Title: title, Severity: "info", Category: "docs"}
	return x.store_("", f, message, []edit{{path: path, proposed: content, created: true}})
}

func fileBlock(path, current string) string {
	var b strings.Builder
	b.WriteString("\n## File " + path + " (line numbers are for reference only, do not include them)\n")
	for i, l := range strings.Split(current, "\n") {
		fmt.Fprintf(&b, "%4d | %s\n", i+1, l)
	}
	return b.String()
}

var importLine = regexp.MustCompile(`(?m)^import\s+(?:static\s+)?([\w.]+)\s*;\s*\n`)

// checkImports validates imports the fix added that point into the project's own packages. An
// import of a type that does not exist is removed when a type of that name lives in the file's own
// package (the model guessed a sub-package); otherwise it is reported.
func (x *Fixer) checkImports(before, after string) (string, []string) {
	if x.Known == nil {
		return after, nil
	}
	known := x.Known()
	if len(known) == 0 {
		return after, nil
	}
	root := ""
	for fqn := range known {
		pkg := fqn[:max(0, strings.LastIndex(fqn, "."))]
		if root == "" {
			root = pkg
			continue
		}
		for root != "" && pkg != root && !strings.HasPrefix(pkg, root+".") {
			if i := strings.LastIndex(root, "."); i > 0 {
				root = root[:i]
			} else {
				root = ""
			}
		}
	}
	pkg := ""
	if m := regexp.MustCompile(`(?m)^package\s+([\w.]+)\s*;`).FindStringSubmatch(after); m != nil {
		pkg = m[1]
	}
	had := map[string]bool{}
	for _, m := range importLine.FindAllStringSubmatch(before, -1) {
		had[m[1]] = true
	}
	var bad []string
	out := importLine.ReplaceAllStringFunc(after, func(line string) string {
		name := importLine.FindStringSubmatch(line)[1]
		if had[name] || root == "" || !strings.HasPrefix(name, root+".") || known[name] {
			return line
		}
		simple := name[strings.LastIndex(name, ".")+1:]
		if known[pkg+"."+simple] {
			return "" // same package: no import needed
		}
		if parent := name[:strings.LastIndex(name, ".")]; known[parent] {
			return line // nested type or static member
		}
		bad = append(bad, name)
		return line
	})
	return out, bad
}

// InjectionRegressionTest writes the standard regression test for a dependency that Spring never
// injected: start the context, unwrap the bean's proxy and assert the field is set. It fails while
// the defect exists and passes once the dependency is injected. No model involved.
func (x *Fixer) InjectionRegressionTest(sourcePath, field, title string) (*Proposal, error) {
	if err := x.ready(); err != nil {
		return nil, err
	}
	src, ok, err := x.repo.ReadWorking(sourcePath)
	if err != nil || !ok || !strings.Contains(sourcePath, "src/main/java/") {
		return nil, fmt.Errorf("%s is not a source file under src/main/java", sourcePath)
	}
	pkg := ""
	if m := regexp.MustCompile(`(?m)^package\s+([\w.]+)\s*;`).FindStringSubmatch(src); m != nil {
		pkg = m[1]
	}
	cls := strings.TrimSuffix(sourcePath[strings.LastIndex(sourcePath, "/")+1:], ".java")
	base := strings.TrimSuffix(strings.Replace(sourcePath, "src/main/java/", "src/test/java/", 1), ".java")
	testPath := base + "WiringTest.java"
	for i := 2; x.repo.ExistsWorking(testPath); i++ {
		testPath = fmt.Sprintf("%sWiring%dTest.java", base, i)
	}
	testCls := strings.TrimSuffix(testPath[strings.LastIndex(testPath, "/")+1:], ".java")
	bean := strings.ToLower(cls[:1]) + cls[1:]
	method := field + "IsInjected"
	test := fmt.Sprintf(`package %s;

import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.test.util.AopTestUtils;
import org.springframework.test.util.ReflectionTestUtils;

import static org.assertj.core.api.Assertions.assertThat;

/** Regression test: %s. Fails while %s.%s is not injected. */
@SpringBootTest
class %s {

    @Autowired
    %s %s;

    @Test
    void %s() {
        Object target = AopTestUtils.getTargetObject(%s); // look through Spring's proxy
        assertThat(ReflectionTestUtils.getField(target, "%s")).isNotNull();
    }
}
`, pkg, strings.TrimSuffix(title, "."), cls, field, testCls, cls, bean, method, bean, field)
	if !javaidx.Valid([]byte(test)) {
		return nil, errors.New("could not build the wiring test")
	}
	f := Finding{Title: "Regression test: " + cls + "." + field + " is injected", Detail: title, Severity: "info", Category: "test"}
	msg := fmt.Sprintf("test(%s): assert %s is injected\n\nRegression test for: %s\n\nWritten by Codist.", cls, field, title)
	return x.store_("", f, msg, []edit{{path: testPath, proposed: test, created: true}})
}
