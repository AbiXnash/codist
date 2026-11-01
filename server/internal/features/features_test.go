package features

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilesOverrideInOrderAndNeverBreakTheApp(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a := write("a.json", `{"menus":{"team":false,"repo":false},"features":{"fix":false}}`)
	b := write("b.json", `{"menus":{"repo":true,"nope":false}}`)
	broken := write("c.json", `{not json`)

	f := Load(a, b, broken, filepath.Join(dir, "missing.json"))
	if f.Menus["team"] || !f.Menus["repo"] || f.Features["fix"] {
		t.Errorf("flags = %+v", f)
	}
	if !f.Menus["review"] || !f.Panels["ask"] {
		t.Errorf("what no file mentions stays on: %+v", f)
	}
	all := strings.Join(f.Problems, "\n")
	if !strings.Contains(all, "menus.nope") || !strings.Contains(all, "c.json") {
		t.Errorf("a typo and a broken file must be reported: %q", all)
	}
	if len(f.Sources) != 2 {
		t.Errorf("sources = %v", f.Sources)
	}
}
