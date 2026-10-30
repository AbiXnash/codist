package review

import (
	"fmt"
	"strings"

	"codist/internal/tools"
)

// sameAs maps analyser rules to Codist rules that report the same defect, so one problem is not
// listed twice. When Codist already reported it in the file, the analyser's finding is dropped.
var sameAs = map[string]string{
	"spotbugs:UWF_UNWRITTEN_FIELD": "uninjected-dependency", "spotbugs:NP_UNWRITTEN_FIELD": "uninjected-dependency",
	"spotbugs:UWF_FIELD_NOT_INITIALIZED_IN_CONSTRUCTOR": "uninjected-dependency",
	"spotbugs:DMI_BIGDECIMAL_CONSTRUCTED_FROM_DOUBLE":   "bigdecimal-double", "pmd:AvoidDecimalLiteralsInBigDecimalConstructor": "bigdecimal-double",
	"pmd:EmptyCatchBlock": "swallowed-exception", "spotbugs:DE_MIGHT_IGNORE": "swallowed-exception",
	"pmd:AvoidPrintStackTrace": "console-output", "spotbugs:SQL_NONCONSTANT_STRING_PASSED_TO_EXECUTE": "sql-injection",
	"spotbugs:NP_NULL_ON_SOME_PATH_FROM_RETURN_VALUE": "optional-get-unchecked",
}

// mergeAnalysis adds the analysers' findings on the changed lines of a file to Codist's own.
func mergeAnalysis(own []Finding, found []tools.Finding, path string, in fileInput) []Finding {
	have := map[string]bool{}
	for _, f := range own {
		have[f.Ref] = true
	}
	out := own
	for _, t := range found {
		if t.Path != path || !touches(in.changed, t.Line, t.EndLine) {
			continue
		}
		ref := t.Tool + ":" + t.Rule
		if eq, ok := sameAs[ref]; ok && have[eq] {
			continue
		}
		if have[ref] && hasFinding(out, t.Line, ref) {
			continue
		}
		name := map[string]string{"pmd": "PMD", "spotbugs": "SpotBugs"}[t.Tool]
		detail := t.Message
		if t.URL != "" {
			detail += " (" + name + " " + t.Rule + ": " + t.URL + ")"
		}
		out = append(out, Finding{Line: t.Line, EndLine: t.EndLine, Severity: t.Severity, Category: t.Category,
			Title: fmt.Sprintf("%s (%s)", firstSentence(t.Message), name), Detail: detail, Source: "rule", Ref: ref,
			OnChangedLine: true, Snippet: lineAt(in.lines, t.Line)})
		have[ref] = true
	}
	return out
}

func touches(changed map[int]bool, from, to int) bool {
	for l := from; l <= max(from, to); l++ {
		if changed[l] {
			return true
		}
	}
	return false
}

func firstSentence(s string) string {
	s = strings.TrimSpace(strings.Split(s, "\n")[0])
	if i := strings.Index(s, ". "); i > 0 {
		s = s[:i]
	}
	if len(s) > 90 {
		s = s[:87] + "…"
	}
	return strings.TrimSuffix(s, ".")
}
