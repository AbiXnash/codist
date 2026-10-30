package prompts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEveryPromptLoadsAndRenders(t *testing.T) {
	Configure("", "")
	all := All()
	if len(all) < 14 {
		t.Fatalf("expected the built-in prompts, got %d", len(all))
	}
	for _, p := range all {
		if p.Body == "" || p.Description == "" || p.Source != "built-in" {
			t.Fatalf("incomplete prompt %+v", p)
		}
		if strings.HasPrefix(p.Body, "---") {
			t.Fatalf("%s: front matter leaked into the body", p.Name)
		}
		if p.Format == "json" && !strings.Contains(strings.ToLower(p.Body), "json") {
			t.Fatalf("%s: a JSON prompt must ask for JSON", p.Name)
		}
	}
	got := Render("retry-bad-imports", Items([]string{"a.B", "c.D"}))
	if !strings.Contains(got, "a.B, c.D") || strings.Contains(got, "{{") {
		t.Fatalf("template not rendered: %q", got)
	}
	if got := Render("retry-broken-references", Items([]string{"Foo.bar is used by X"})); !strings.Contains(got, "- Foo.bar is used by X") {
		t.Fatalf("list not rendered: %q", got)
	}
}

func TestTeamOverrideWinsAndReloads(t *testing.T) {
	personal, team := t.TempDir(), t.TempDir()
	Configure(personal, team)
	defer Configure("", "")
	os.WriteFile(filepath.Join(personal, "ask.md"), []byte("---\ndescription: mine\n---\nPersonal ask"), 0o644)
	if p := Load("ask"); p.Body != "Personal ask" || p.Source != "personal" {
		t.Fatalf("personal override: %+v", p)
	}
	path := filepath.Join(team, "ask.md")
	os.WriteFile(path, []byte("Team ask"), 0o644)
	if p := Load("ask"); p.Body != "Team ask" || p.Source != "team" {
		t.Fatalf("team override should win: %+v", p)
	}
	os.WriteFile(path, []byte("Team ask v2"), 0o644)
	later := time.Now().Add(2 * time.Second)
	os.Chtimes(path, later, later)
	if got := Get("ask"); got != "Team ask v2" {
		t.Fatalf("edited override not reloaded: %q", got)
	}
	os.WriteFile(path, []byte("   "), 0o644)
	os.Chtimes(path, later.Add(time.Second), later.Add(time.Second))
	if p := Load("ask"); p.Source != "personal" {
		t.Fatalf("an empty override should be skipped: %+v", p)
	}
}
