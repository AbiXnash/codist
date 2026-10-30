// Package tools runs established Java analysers on a commit, through Maven, so their findings join
// Codist's own and are given to the model as evidence:
//
//   - PMD 7 with a curated rule set (defects, not style), via a generated runner POM pointed at the
//     commit's sources, so the project's own build is not involved.
//   - SpotBugs on the compiled classes (bytecode analysis: null paths, unwritten fields, resource leaks,
//     concurrency), run in an export of the commit.
//
// Each commit is exported with git archive into the data directory; the working tree is never touched.
// Results are cached per commit. Plugins are downloaded by Maven on first use.
package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed pmd-ruleset.xml
var pmdRuleset string

const (
	pmdPlugin      = "org.apache.maven.plugins:maven-pmd-plugin:3.26.0"
	spotbugsPlugin = "com.github.spotbugs:spotbugs-maven-plugin:4.8.6.4"
	timeout        = 5 * time.Minute
)

// Finding is one result of an analyser, on a repository path.
type Finding struct {
	Tool     string `json:"tool"` // pmd or spotbugs
	Rule     string `json:"rule"`
	Category string `json:"category"`
	Severity string `json:"severity"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
	EndLine  int    `json:"endLine"`
	Message  string `json:"message"`
	URL      string `json:"url,omitempty"`
}

// Result is what the analysers reported for one commit.
type Result struct {
	SHA      string    `json:"sha"`
	Findings []Finding `json:"findings"`
	Ran      []string  `json:"ran"`
	Problems []string  `json:"problems,omitempty"` // why an analyser did not run
	Took     int64     `json:"tookMs"`
}

// Runner analyses commits of one repository.
type Runner struct {
	Root    string // the repository
	DataDir string // <data-dir>/<repo>
	Enabled map[string]bool
	mvn     string
	mu      sync.Mutex
}

// New enables the requested tools ("auto" = every tool Maven can run here, "off" = none).
func New(root, dataDir, which string) *Runner {
	r := &Runner{Root: root, DataDir: dataDir, Enabled: map[string]bool{}}
	which = strings.ToLower(strings.TrimSpace(which))
	if which == "off" || which == "none" {
		return r
	}
	mvn, err := exec.LookPath("mvn")
	if err != nil {
		slog.Info("static analysers off: Maven (mvn) not found")
		return r
	}
	r.mvn = mvn
	for _, t := range strings.Split(which, ",") {
		switch t = strings.TrimSpace(t); t {
		case "", "auto":
			r.Enabled["pmd"], r.Enabled["spotbugs"] = true, true
		case "pmd", "spotbugs":
			r.Enabled[t] = true
		}
	}
	return r
}

// Active lists the enabled tools.
func (r *Runner) Active() []string {
	var out []string
	for _, t := range []string{"pmd", "spotbugs"} {
		if r.Enabled[t] {
			out = append(out, t)
		}
	}
	return out
}

// Analyze runs the enabled analysers on a commit (cached). Only Maven projects are supported.
func (r *Runner) Analyze(sha string) Result {
	res := Result{SHA: sha, Findings: []Finding{}}
	if len(r.Active()) == 0 {
		return res
	}
	cache := filepath.Join(r.DataDir, "analysis", sha+".json")
	if b, err := os.ReadFile(cache); err == nil && json.Unmarshal(b, &res) == nil {
		return res
	}
	r.mu.Lock() // one Maven build at a time
	defer r.mu.Unlock()
	start := time.Now()

	work := filepath.Join(r.DataDir, "analysis", "work-"+sha[:min(12, len(sha))])
	src := filepath.Join(work, "src")
	defer os.RemoveAll(work)
	if err := r.export(sha, src); err != nil {
		res.Problems = append(res.Problems, "export: "+err.Error())
		return res
	}
	if _, err := os.Stat(filepath.Join(src, "pom.xml")); err != nil {
		res.Problems = append(res.Problems, "not a Maven project (no pom.xml at the root); analysers skipped")
		return r.save(cache, res, start)
	}
	if r.Enabled["pmd"] {
		if fs, err := r.pmd(work, src); err != nil {
			res.Problems = append(res.Problems, "pmd: "+err.Error())
		} else {
			res.Findings = append(res.Findings, fs...)
			res.Ran = append(res.Ran, "pmd")
		}
	}
	if r.Enabled["spotbugs"] {
		if fs, err := r.spotbugs(src); err != nil {
			res.Problems = append(res.Problems, "spotbugs: "+err.Error())
		} else {
			res.Findings = append(res.Findings, fs...)
			res.Ran = append(res.Ran, "spotbugs")
		}
	}
	for _, p := range res.Problems {
		slog.Warn("static analysis", "sha", sha[:7], "problem", p)
	}
	slog.Info("static analysis", "sha", sha[:7], "ran", res.Ran, "findings", len(res.Findings), "took", time.Since(start).Round(time.Millisecond))
	return r.save(cache, res, start)
}

func (r *Runner) save(path string, res Result, start time.Time) Result {
	res.Took = time.Since(start).Milliseconds()
	if len(res.Ran) == 0 && len(res.Problems) > 0 && !strings.Contains(strings.Join(res.Problems, " "), "not a Maven") {
		return res // do not cache failures: the next review tries again
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	b, _ := json.MarshalIndent(res, "", "  ")
	_ = os.WriteFile(path, b, 0o644)
	return res
}

func (r *Runner) export(sha, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	archive := exec.Command("git", "-C", r.Root, "archive", "--format=tar", sha)
	untar := exec.Command("tar", "-x", "-C", dir)
	pipe, err := archive.StdoutPipe()
	if err != nil {
		return err
	}
	untar.Stdin = pipe
	if err := untar.Start(); err != nil {
		return err
	}
	if err := archive.Run(); err != nil {
		return err
	}
	return untar.Wait()
}

func (r *Runner) maven(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.mvn, append([]string{"-q", "-B"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return string(out), fmt.Errorf("timed out after %s", timeout)
	}
	return string(out), err
}

// pmd runs PMD through a runner POM, so the project's build configuration does not matter.
func (r *Runner) pmd(work, src string) ([]Finding, error) {
	runner := filepath.Join(work, "pmd")
	if err := os.MkdirAll(runner, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(runner, "ruleset.xml"), []byte(pmdRuleset), 0o644); err != nil {
		return nil, err
	}
	var roots strings.Builder
	for _, d := range sourceRoots(src, "test") {
		roots.WriteString("<testSourceRoot>" + xmlEscape(d) + "</testSourceRoot>")
	}
	main := sourceRoots(src, "main")
	if len(main) == 0 {
		return []Finding{}, nil
	}
	pom := fmt.Sprintf(`<project xmlns="http://maven.apache.org/POM/4.0.0"><modelVersion>4.0.0</modelVersion>
<groupId>codist</groupId><artifactId>pmd-runner</artifactId><version>1</version><packaging>jar</packaging>
<build><sourceDirectory>%s</sourceDirectory><plugins><plugin>
<groupId>org.apache.maven.plugins</groupId><artifactId>maven-pmd-plugin</artifactId><version>%s</version>
<configuration><rulesets><ruleset>%s</ruleset></rulesets><format>xml</format><targetJdk>%s</targetJdk>
<linkXRef>false</linkXRef><includeTests>true</includeTests><testSourceRoots>%s</testSourceRoots></configuration>
</plugin></plugins></build></project>`, xmlEscape(main[0]), strings.Split(pmdPlugin, ":")[2],
		xmlEscape(filepath.Join(runner, "ruleset.xml")), javaVersion(src), roots.String())
	if err := os.WriteFile(filepath.Join(runner, "pom.xml"), []byte(pom), 0o644); err != nil {
		return nil, err
	}
	if out, err := r.maven(runner, "pmd:pmd"); err != nil {
		return nil, fmt.Errorf("%v: %s", err, lastLines(out, 4))
	}
	b, err := os.ReadFile(filepath.Join(runner, "target", "pmd.xml"))
	if err != nil {
		return nil, err
	}
	var rep struct {
		Files []struct {
			Name       string `xml:"name,attr"`
			Violations []struct {
				Begin    int    `xml:"beginline,attr"`
				End      int    `xml:"endline,attr"`
				Rule     string `xml:"rule,attr"`
				Ruleset  string `xml:"ruleset,attr"`
				Priority int    `xml:"priority,attr"`
				URL      string `xml:"externalInfoUrl,attr"`
				Text     string `xml:",chardata"`
			} `xml:"violation"`
		} `xml:"file"`
	}
	if err := xml.Unmarshal(b, &rep); err != nil {
		return nil, err
	}
	var out []Finding
	for _, f := range rep.Files {
		path := relPath(src, f.Name)
		for _, v := range f.Violations {
			out = append(out, Finding{Tool: "pmd", Rule: v.Rule, Category: pmdCategory(v.Ruleset), Severity: pmdSeverity(v.Priority),
				Path: path, Line: v.Begin, EndLine: max(v.Begin, v.End), Message: strings.TrimSpace(v.Text), URL: v.URL})
		}
	}
	return out, nil
}

// spotbugs compiles the export and analyses its bytecode.
func (r *Runner) spotbugs(src string) ([]Finding, error) {
	if out, err := r.maven(src, "-DskipTests", "-Dmaven.test.skip=true", "compile", spotbugsPlugin+":spotbugs",
		"-Dspotbugs.xmlOutput=true", "-Dspotbugs.effort=Max", "-Dspotbugs.threshold=Medium", "-Dspotbugs.failOnError=false"); err != nil {
		return nil, fmt.Errorf("the project does not compile at this commit or SpotBugs failed: %s", lastLines(out, 4))
	}
	reports, _ := filepath.Glob(filepath.Join(src, "target", "spotbugsXml.xml"))
	more, _ := filepath.Glob(filepath.Join(src, "*", "target", "spotbugsXml.xml")) // multi-module
	reports = append(reports, more...)
	var out []Finding
	for _, rp := range reports {
		b, err := os.ReadFile(rp)
		if err != nil {
			continue
		}
		var rep struct {
			Bugs []struct {
				Type     string `xml:"type,attr"`
				Priority string `xml:"priority,attr"`
				Category string `xml:"category,attr"`
				Short    string `xml:"ShortMessage"`
				Long     string `xml:"LongMessage"`
				Lines    []struct {
					Path  string `xml:"sourcepath,attr"`
					Start string `xml:"start,attr"`
					End   string `xml:"end,attr"`
					Role  string `xml:"role,attr"`
				} `xml:"SourceLine"`
			} `xml:"BugInstance"`
		}
		if err := xml.Unmarshal(b, &rep); err != nil {
			continue
		}
		for _, bug := range rep.Bugs {
			if len(bug.Lines) == 0 || ignoredSpotBugs[bug.Type] {
				continue
			}
			sl := bug.Lines[0]
			for _, l := range bug.Lines { // the primary line has no role
				if l.Role == "" {
					sl = l
					break
				}
			}
			path := findSource(src, sl.Path)
			if path == "" {
				continue
			}
			start, _ := strconv.Atoi(sl.Start)
			end, _ := strconv.Atoi(sl.End)
			msg := strings.TrimSpace(bug.Long)
			if msg == "" {
				msg = strings.TrimSpace(bug.Short)
			}
			out = append(out, Finding{Tool: "spotbugs", Rule: bug.Type, Category: spotbugsCategory(bug.Category),
				Severity: spotbugsSeverity(bug.Priority, bug.Category), Path: path, Line: start, EndLine: max(start, end), Message: msg,
				URL: "https://spotbugs.readthedocs.io/en/latest/bugDescriptions.html#" + strings.ToLower(strings.ReplaceAll(bug.Type, "_", "-"))})
		}
	}
	return out, nil
}

// Exposing internal arrays and dates is reported on almost every entity and DTO; it drowns the rest.
var ignoredSpotBugs = map[string]bool{"EI_EXPOSE_REP": true, "EI_EXPOSE_REP2": true}

func sourceRoots(src, kind string) []string {
	var out []string
	_ = filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || d.Name() == "target" || d.Name() == "node_modules" {
			return filepath.SkipDir
		}
		if strings.HasSuffix(p, filepath.Join("src", kind, "java")) {
			out = append(out, p)
			return filepath.SkipDir
		}
		return nil
	})
	return out
}

// findSource maps a SpotBugs source path (package-relative) to a repository path.
func findSource(src, pkgPath string) string {
	for _, root := range sourceRoots(src, "main") {
		if _, err := os.Stat(filepath.Join(root, pkgPath)); err == nil {
			return relPath(src, filepath.Join(root, pkgPath))
		}
	}
	return ""
}

func relPath(src, abs string) string {
	if real, err := filepath.EvalSymlinks(src); err == nil {
		if rel, err := filepath.Rel(real, abs); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	rel, err := filepath.Rel(src, abs)
	if err != nil {
		return abs
	}
	return filepath.ToSlash(rel)
}

// javaVersion reads the project's Java version from the POM, for PMD's parser.
func javaVersion(src string) string {
	b, _ := os.ReadFile(filepath.Join(src, "pom.xml"))
	s := string(b)
	for _, tag := range []string{"java.version", "maven.compiler.release", "maven.compiler.source"} {
		if i := strings.Index(s, "<"+tag+">"); i >= 0 {
			v := s[i+len(tag)+2:]
			if j := strings.Index(v, "<"); j > 0 {
				return strings.TrimPrefix(strings.TrimSpace(v[:j]), "1.")
			}
		}
	}
	return "17"
}

func pmdSeverity(p int) string {
	switch {
	case p <= 1:
		return "high"
	case p <= 3:
		return "medium"
	default:
		return "low"
	}
}

func pmdCategory(ruleset string) string {
	switch strings.ToLower(ruleset) {
	case "security":
		return "security"
	case "performance":
		return "performance"
	case "multithreading":
		return "bug"
	case "error prone":
		return "bug"
	default:
		return "design"
	}
}

func spotbugsSeverity(priority, category string) string {
	switch {
	case category == "SECURITY" || (priority == "1" && (category == "CORRECTNESS" || category == "MT_CORRECTNESS")):
		return "high"
	case priority == "1" || category == "CORRECTNESS" || category == "MT_CORRECTNESS":
		return "medium"
	default:
		return "low"
	}
}

func spotbugsCategory(c string) string {
	switch c {
	case "SECURITY", "MALICIOUS_CODE":
		return "security"
	case "PERFORMANCE":
		return "performance"
	case "CORRECTNESS", "MT_CORRECTNESS", "BAD_PRACTICE":
		return "bug"
	default:
		return "design"
	}
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
