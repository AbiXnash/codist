package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Runs real Maven: PMD and SpotBugs must find planted defects in a committed project.
func TestAnalyzersFindPlantedBugs(t *testing.T) {
	if testing.Short() {
		t.Skip("runs Maven")
	}
	if _, err := exec.LookPath("mvn"); err != nil {
		t.Skip("Maven not installed")
	}
	dir, data := t.TempDir(), t.TempDir()
	write := func(p, s string) {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755)
		os.WriteFile(filepath.Join(dir, p), []byte(s), 0o644)
	}
	write("pom.xml", `<project xmlns="http://maven.apache.org/POM/4.0.0"><modelVersion>4.0.0</modelVersion>
<groupId>t</groupId><artifactId>t</artifactId><version>1</version>
<properties><maven.compiler.release>17</maven.compiler.release><project.build.sourceEncoding>UTF-8</project.build.sourceEncoding></properties></project>`)
	write("src/main/java/x/Bad.java", `package x;
public class Bad {
    private String name;
    public int len() { return name.length(); }
    public boolean same(String a, String b) { return a == b; }
}
`)
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "one"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git: %s", out)
		}
	}
	sha, _ := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	r := New(dir, data, "auto")
	res := r.Analyze(string(sha[:40]))
	ran := strings.Join(res.Ran, ",")
	if !strings.Contains(ran, "pmd") || !strings.Contains(ran, "spotbugs") || len(res.Problems) > 0 {
		t.Fatalf("the analysers should run: ran %v, problems %v", res.Ran, res.Problems)
	}
	rules := map[string]bool{}
	for _, f := range res.Findings {
		if f.Path != "src/main/java/x/Bad.java" {
			t.Fatalf("path not mapped to the repository: %+v", f)
		}
		rules[f.Tool+":"+f.Rule] = true
	}
	if !rules["pmd:UseEqualsToCompareStrings"] || !rules["spotbugs:UWF_UNWRITTEN_FIELD"] && !rules["spotbugs:NP_UNWRITTEN_FIELD"] {
		t.Fatalf("expected PMD string == and SpotBugs unwritten field, got %v", rules)
	}
	if again := r.Analyze(string(sha[:40])); len(again.Findings) != len(res.Findings) {
		t.Fatal("cached result differs")
	}
	if _, err := os.Stat(filepath.Join(data, "analysis", string(sha[:40])+".json")); err != nil {
		t.Fatal("result not cached")
	}
}

// Runs the real language server when it is installed.
func TestJdtlsReportsCompilerProblems(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	if _, err := exec.LookPath("jdtls"); err != nil {
		t.Skip("jdtls not installed")
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src/main/java/x"), 0o755)
	os.WriteFile(filepath.Join(dir, "pom.xml"), []byte(`<project xmlns="http://maven.apache.org/POM/4.0.0"><modelVersion>4.0.0</modelVersion>
<groupId>t</groupId><artifactId>t</artifactId><version>1</version>
<properties><maven.compiler.release>17</maven.compiler.release></properties></project>`), 0o644)
	os.WriteFile(filepath.Join(dir, "src/main/java/x/Bad.java"), []byte("package x;\nimport java.util.List;\npublic class Bad {\n    public int n() { String s = 1; return missing(); }\n    private void unused() { int dead = 2; }\n}\n"), 0o644)
	r := &Runner{jdtlsBin: mustLook(t, "jdtls")}
	fs, err := r.jdtls(t.TempDir(), dir)
	if err != nil {
		t.Fatal(err)
	}
	errs, warns := 0, 0
	for _, f := range fs {
		t.Logf("%s:%d %s %s: %s", f.Path, f.Line, f.Severity, f.Rule, f.Message)
		if f.Path != "src/main/java/x/Bad.java" {
			t.Fatalf("path not mapped: %+v", f)
		}
		if f.Severity == "high" {
			errs++
		} else {
			warns++
		}
	}
	if errs < 2 || warns < 1 {
		t.Fatalf("expected the type error, the missing method and an unused warning; got %+v", fs)
	}
}

func mustLook(t *testing.T, name string) string {
	p, err := exec.LookPath(name)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
