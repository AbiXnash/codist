package review

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"codist/internal/gitx"
)

var numberedPrefix = regexp.MustCompile(`^\s*(>>)?\s*\d*\s*\|?\s*`)

// vagueAdvice says a model finding only suggests thinking about something. The prompt forbids these and
// small models write them anyway. In the evaluation, a model asked to double-check its own findings
// dropped real bugs too, so this stays a plain rule instead of a second model call.
func vagueAdvice(title string) bool {
	t := strings.ToLower(strings.TrimSpace(title))
	for _, start := range []string{"consider ", "you may want", "you might want", "it may be", "review the", "ensure that"} {
		if strings.HasPrefix(t, start) {
			return true
		}
	}
	return false
}

// quotesCode says what the model quoted really is in the file, ignoring all spacing and line breaks: a
// model often joins a statement that spans several lines into one.
func quotesCode(lines []string, quote string) bool {
	var parts []string
	for _, l := range strings.Split(quote, "\n") {
		parts = append(parts, numberedPrefix.ReplaceAllString(l, ""))
	}
	want := strings.Join(strings.Fields(strings.Join(parts, " ")), "")
	if want == "" {
		return false
	}
	return strings.Contains(strings.Join(strings.Fields(strings.Join(lines, " ")), ""), want)
}

// quotedNear says the quote appears within reach of the line the model names. A model that quotes real
// code from somewhere else in the file is usually talking about something other than that line.
func quotedNear(lines []string, quote string, line, reach int) bool {
	var parts []string
	for _, l := range strings.Split(quote, "\n") {
		parts = append(parts, numberedPrefix.ReplaceAllString(l, ""))
	}
	want := strings.Join(strings.Fields(strings.Join(parts, " ")), "")
	if want == "" {
		return false
	}
	var whole strings.Builder
	starts := make([]int, len(lines))
	for i, l := range lines {
		starts[i] = whole.Len()
		whole.WriteString(strings.Join(strings.Fields(l), ""))
	}
	text := whole.String()
	for from := 0; ; {
		at := strings.Index(text[from:], want)
		if at < 0 {
			return false
		}
		at += from
		first := sort.Search(len(starts), func(i int) bool { return starts[i] > at }) // 1-based line of the match
		if first >= line-reach && first <= line+reach {
			return true
		}
		from = at + 1
	}
}

// aboutPattern lists what a quote has to show for a bug pattern to apply. A BigDecimal precision problem
// needs a construction or comparison; merely reading a BigDecimal field proves nothing.
var aboutPattern = map[string]*regexp.Regexp{
	"bigdecimal-precision": regexp.MustCompile(`new\s+BigDecimal|\.equals\(|doubleValue|floatValue|BigDecimal\.valueOf|\bdouble\b|\bfloat\b`),
}

// repoStandards reads what the repository says about how its code should be written, with the
// conventions other AI tools already use, so a team does not have to write it twice.
func (s *Service) repoStandards() string {
	return readStandards(s.repo)
}

// Standards is the same text for other features, such as Ask.
func Standards(repo *gitx.Repo) string { return readStandards(repo) }

func readStandards(repo *gitx.Repo) string {
	for _, name := range []string{filepath.Join(".codist", "standards.md"), "AGENTS.md", "CLAUDE.md", "CONTRIBUTING.md"} {
		var text string
		if b, err := os.ReadFile(filepath.Join(repo.Root, name)); err == nil && !repo.Viewing() {
			text = string(b)
		} else if src, ok := repo.ReadFile(repo.TrackedRef(), filepath.ToSlash(name)); ok {
			text = src
		}
		if strings.TrimSpace(text) != "" {
			return trimTo(text, 3000) + "\n"
		}
	}
	return ""
}

var (
	nullClaim = regexp.MustCompile(`(?i)\bnull|\bnpe\b|nullpointer`)
	// A model describing what the change did has named no problem. "Removed ..." stays out: a removed
	// check is often the bug itself.
	changeTitle = regexp.MustCompile(`(?i)^(improved|simplified|added|adding|renamed|refactored|updated|changed|moved|replaced|introduced|extracted|explicitly passing|use of)\b`)
	receiver    = regexp.MustCompile(`\b([A-Za-z_]\w*)\s*(?:\.\s*[A-Za-z_]\w*|\[)`)
	removedNull = regexp.MustCompile(`(?m)^-[^-].*\bnull\b`)
)

// weakCeiling is the highest severity a claim Codist could not back keeps, so it lands among the notes
// without being thrown away: in the evaluation, these sometimes were the real bug.
const weakCeiling = "low"

// weighClaim checks a model finding against the code. It returns the severity to show and, when the
// code does not back the claim, why. Hand-reading 95 located bugs found 0 real catches among those
// flagged only by the title copied from the null pattern, and 0 among those that only described a change.
func weighClaim(severity, title, ref, evidence string, lines []string, line int, patch string) (string, string) {
	doubt := ""
	switch {
	case changeTitle.MatchString(strings.TrimSpace(title)):
		doubt = "This describes the change and names no problem in it."
	case (ref == "null-dereference" || nullClaim.MatchString(title)) && !nullBacked(evidence, lines, line, patch):
		doubt = "Codist could not find where this value becomes null in this code."
	}
	if doubt == "" {
		return severity, ""
	}
	if slices.Index(severities, severity) < slices.Index(severities, weakCeiling) {
		severity = weakCeiling
	}
	return severity, doubt
}

// nullBacked says the code gives a value a way to be null: the change removed a null check, or nearby
// code tests the value for null, sets it to null, or takes it from a call that returns null on a miss.
func nullBacked(evidence string, lines []string, line int, patch string) bool {
	if removedNull.MatchString(patch) {
		return true
	}
	from, to := max(0, line-31), min(len(lines), line+30)
	near := strings.Join(lines[from:to], "\n")
	for _, m := range receiver.FindAllStringSubmatch(evidence, -1) {
		v := regexp.QuoteMeta(m[1])
		source := `(?m)\b` + v + `\s*[!=]=\s*null\b|\bnull\s*[!=]=\s*` + v + `\b|\b` + v + `\s*=\s*null\b|@Nullable\b[^;\n]*\b` + v +
			`\b|\b` + v + `\s*=[^;\n]*\.(get|find\w*|remove|poll|peek\w*|getProperty|getenv|getParameter|getHeader|getAttribute|lookup)\(`
		if regexp.MustCompile(source).MatchString(near) {
			return true
		}
	}
	return false
}
