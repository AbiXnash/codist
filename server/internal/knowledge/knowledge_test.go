package knowledge

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinRulesLoadAndAreComplete(t *testing.T) {
	kb, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(kb.Rules) < 90 {
		t.Fatalf("only %d rules", len(kb.Rules))
	}
	seen := map[string]bool{}
	for _, r := range kb.Rules {
		if seen[r.ID] {
			t.Errorf("duplicate id %s", r.ID)
		}
		seen[r.ID] = true
		if r.Check == "" || r.Why == "" || r.Category == "" {
			t.Errorf("%s is missing check/why/category", r.ID)
		}
		if _, ok := severityRank[r.Severity]; !ok {
			t.Errorf("%s has severity %q", r.ID, r.Severity)
		}
		for _, trig := range r.Triggers {
			if strings.HasPrefix(trig, "re:") && len(r.matchers) < len(r.Triggers) {
				t.Errorf("%s has an invalid regex trigger %q", r.ID, trig)
			}
		}
	}
}

func TestSelectPicksRelevantRules(t *testing.T) {
	kb, _ := Load()
	added := "        product.setStock(product.getStock() - req.quantity());\n        System.out.println(DataConstant.LOG_PREFIX + \"Request: \" + req);\n"
	ids := map[string]bool{}
	for _, r := range kb.Select("src/main/java/com/demo/ProductService.java", added, added, 10) {
		ids[r.ID] = true
	}
	for _, want := range []string{"lost-update", "console-output", "copy-paste-wrong-constant", "sensitive-logging"} {
		if !ids[want] {
			t.Errorf("expected %s in %v", want, ids)
		}
	}
	for _, r := range kb.Select("src/main/resources/application.yml", "spring:\n  jpa:\n    hibernate:\n      ddl-auto: update\n", "      ddl-auto: update\n", 8) {
		if r.Applies[0] == "java" {
			t.Errorf("java rule %s selected for yaml", r.ID)
		}
	}
}

func TestOverrideAndExport(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "team.yaml"), []byte(`
- id: console-output
  title: Team rule replaces built-in
  category: logging
  severity: medium
  triggers: ['System.out']
  check: c
  why: w
- id: team-money
  title: Use Money type
  category: design
  severity: low
  triggers: ['BigDecimal']
  check: c
  why: w
  bad: BigDecimal x;
  good: Money x;
`), 0o644)
	kb, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, r := range kb.Rules {
		if r.ID == "console-output" && r.Title == "Team rule replaces built-in" {
			found++
		}
		if r.ID == "team-money" {
			found++
		}
	}
	if found != 2 {
		t.Fatalf("override/extra rules not applied")
	}
	var buf bytes.Buffer
	n, err := kb.ExportTraining(&buf, "system")
	if err != nil || n < 150 || strings.Count(buf.String(), "\n") != n {
		t.Fatalf("export: n=%d err=%v", n, err)
	}
}
