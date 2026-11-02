package review

import (
	"os"
	"path/filepath"
	"regexp"
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

// quotesCode says a line the model quoted really is in the file, ignoring spacing.
func quotesCode(lines []string, quote string) bool {
	want := strings.Join(strings.Fields(numberedPrefix.ReplaceAllString(quote, "")), " ")
	if want == "" {
		return false
	}
	for _, l := range lines {
		if strings.Contains(strings.Join(strings.Fields(l), " "), want) {
			return true
		}
	}
	return false
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
