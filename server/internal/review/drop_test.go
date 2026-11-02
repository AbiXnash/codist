package review

import (
	"strings"
	"testing"
)

func TestDropReason(t *testing.T) {
	lines := []string{"class A {", "  int total = compute();", "}"}
	rule := []Finding{{Source: "rule", Ref: "console-output", Line: 2, EndLine: 2}}
	cases := []struct {
		name       string
		x          map[string]any
		line, end  int
		existing   []Finding
		filtersOff bool
		want       string // part of the reason; "" keeps the finding
	}{
		{name: "real finding is kept", x: map[string]any{"title": "Race on total", "evidence": "int total = compute();"}, line: 2, end: 2},
		{name: "no code near the line", x: map[string]any{"title": "Race"}, want: "no code"},
		{name: "invented quote", x: map[string]any{"title": "Race", "evidence": "double price = 1;"}, line: 2, end: 2, want: "quotes code"},
		{name: "vague advice", x: map[string]any{"title": "Consider adding logging"}, line: 2, end: 2, want: "vague"},
		{name: "claims a rule that did not fire", x: map[string]any{"title": "Issue", "ref": "console-output"}, line: 2, end: 2, want: "did not fire"},
		{name: "echo of a rule", x: map[string]any{"title": "Printing to stdout", "ref": "console-output"}, line: 2, end: 2, existing: rule, want: "repeats"},
		{name: "filters off keeps an invented quote", x: map[string]any{"title": "Race", "evidence": "nope"}, line: 2, end: 2, filtersOff: true},
		{name: "filters off keeps an echo", x: map[string]any{"title": "Printing", "ref": "console-output"}, line: 2, end: 2, existing: rule, filtersOff: true},
		{name: "filters off still needs a position", x: map[string]any{"title": "Race"}, filtersOff: true, want: "no code"},
	}
	for _, c := range cases {
		got := dropReason(c.x, c.line, c.end, lines, c.existing, c.filtersOff)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
