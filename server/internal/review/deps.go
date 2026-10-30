package review

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Build is what Codist knows about the project's build: versions and dependencies.
type Build struct {
	Tool       string
	Java       string
	SpringBoot string
	Deps       []Dep
}

type Dep struct {
	Group, Artifact, Version, Scope string
}

type pom struct {
	Parent struct {
		GroupID    string `xml:"groupId"`
		ArtifactID string `xml:"artifactId"`
		Version    string `xml:"version"`
	} `xml:"parent"`
	Properties struct {
		Entries []struct {
			XMLName xml.Name
			Value   string `xml:",chardata"`
		} `xml:",any"`
	} `xml:"properties"`
	Dependencies []struct {
		GroupID    string `xml:"groupId"`
		ArtifactID string `xml:"artifactId"`
		Version    string `xml:"version"`
		Scope      string `xml:"scope"`
	} `xml:"dependencies>dependency"`
}

var gradleDep = regexp.MustCompile(`(?m)^\s*(implementation|api|compileOnly|runtimeOnly|testImplementation|developmentOnly)\s*\(?\s*["']([^:"']+):([^:"']+)(?::([^"']+))?["']`)
var gradleBoot = regexp.MustCompile(`org\.springframework\.boot["']?\)?\s*version\s*["']([^"']+)`)
var gradleJava = regexp.MustCompile(`(?:languageVersion\s*=\s*JavaLanguageVersion\.of\(|sourceCompatibility\s*=\s*['"]?(?:JavaVersion\.VERSION_)?)(\d+)`)

// ParseBuild reads pom.xml or build.gradle(.kts) content.
func ParseBuild(path, content string) (Build, bool) {
	switch {
	case strings.HasSuffix(path, "pom.xml"):
		var p pom
		if err := xml.Unmarshal([]byte(content), &p); err != nil {
			return Build{}, false
		}
		b := Build{Tool: "Maven"}
		props := map[string]string{}
		for _, e := range p.Properties.Entries {
			props[e.XMLName.Local] = strings.TrimSpace(e.Value)
		}
		b.Java = firstNonEmpty(props["java.version"], props["maven.compiler.release"], props["maven.compiler.source"])
		if p.Parent.ArtifactID == "spring-boot-starter-parent" {
			b.SpringBoot = p.Parent.Version
		}
		for _, d := range p.Dependencies {
			v := d.Version
			if strings.HasPrefix(v, "${") {
				v = props[strings.Trim(v, "${}")]
			}
			b.Deps = append(b.Deps, Dep{Group: d.GroupID, Artifact: d.ArtifactID, Version: v, Scope: d.Scope})
		}
		return b, true
	case strings.HasSuffix(path, "build.gradle"), strings.HasSuffix(path, "build.gradle.kts"):
		b := Build{Tool: "Gradle"}
		if m := gradleBoot.FindStringSubmatch(content); m != nil {
			b.SpringBoot = m[1]
		}
		if m := gradleJava.FindStringSubmatch(content); m != nil {
			b.Java = m[1]
		}
		for _, m := range gradleDep.FindAllStringSubmatch(content, -1) {
			scope := map[string]string{"testImplementation": "test", "runtimeOnly": "runtime", "compileOnly": "provided", "developmentOnly": "runtime"}[m[1]]
			b.Deps = append(b.Deps, Dep{Group: m[2], Artifact: m[3], Version: m[4], Scope: scope})
		}
		return b, true
	}
	return Build{}, false
}

// Summary is a one-paragraph description for prompts.
func (b Build) Summary() string {
	var parts []string
	if b.SpringBoot != "" {
		parts = append(parts, "Spring Boot "+b.SpringBoot)
	}
	if b.Java != "" {
		parts = append(parts, "Java "+b.Java)
	}
	var deps []string
	for _, d := range b.Deps {
		s := d.Artifact
		if d.Version != "" {
			s += " " + d.Version
		}
		if d.Scope != "" && d.Scope != "compile" {
			s += " (" + d.Scope + ")"
		}
		deps = append(deps, s)
	}
	if len(deps) > 0 {
		parts = append(parts, "dependencies: "+strings.Join(deps, ", "))
	}
	return strings.Join(parts, "; ")
}

// BuildFindings are deterministic checks on the build file: end-of-life platforms and known vulnerable versions.
func BuildFindings(b Build, content string, changed map[int]bool) []Finding {
	lines := strings.Split(content, "\n")
	lineOf := func(needle string) int {
		for i, l := range lines {
			if strings.Contains(l, needle) {
				return i + 1
			}
		}
		return 1
	}
	var out []Finding
	add := func(line int, sev, title, detail, fix, ref string) {
		if changed != nil && !changed[line] {
			return
		}
		out = append(out, Finding{Line: line, EndLine: line, Severity: sev, Category: "dependency", Title: title,
			Detail: detail, Suggestion: fix, OnChangedLine: true, Source: "rule", Ref: ref})
	}

	if major(b.SpringBoot) == 2 {
		add(lineOf("spring-boot-starter-parent"), "high", "Spring Boot "+b.SpringBoot+" is end of life",
			"Spring Boot 2.x no longer gets security fixes (OSS support ended November 2023).",
			"Upgrade to Spring Boot 3.x (requires Java 17+ and the jakarta.* namespace).", "eol-spring-boot-2")
	}
	if major(b.SpringBoot) >= 3 && b.Java != "" && atoi(strings.TrimPrefix(b.Java, "1.")) < 17 {
		add(lineOf("java.version"), "high", "Spring Boot 3 needs Java 17 or newer",
			"The project targets Java "+b.Java+", which Spring Boot 3 does not support.", "<java.version>21</java.version>", "java-version-boot3")
	}
	for _, d := range b.Deps {
		at := lineOf("<artifactId>" + d.Artifact + "</artifactId>")
		if b.Tool == "Gradle" {
			at = lineOf(d.Group + ":" + d.Artifact)
		}
		switch {
		case d.Artifact == "log4j-core" && versionLess(d.Version, "2.17.1"):
			add(at, "critical", "log4j-core "+d.Version+" is vulnerable (Log4Shell)", "CVE-2021-44228 and follow-ups allow remote code execution.", "Upgrade log4j-core to 2.17.1 or newer (or remove the explicit version and use the Boot-managed one).", "cve-log4shell")
		case d.Artifact == "commons-text" && versionLess(d.Version, "1.10.0"):
			add(at, "high", "commons-text "+d.Version+" is vulnerable (Text4Shell)", "CVE-2022-42889: string interpolation can execute code.", "Upgrade commons-text to 1.10.0 or newer.", "cve-text4shell")
		case d.Artifact == "snakeyaml" && versionLess(d.Version, "2.0"):
			add(at, "high", "snakeyaml "+d.Version+" allows unsafe deserialization", "CVE-2022-1471: constructing arbitrary types from YAML.", "Upgrade snakeyaml to 2.0 or newer.", "cve-snakeyaml")
		case d.Artifact == "jackson-databind" && versionLess(d.Version, "2.13.4"):
			add(at, "high", "jackson-databind "+d.Version+" has known deserialization CVEs", "Old databind versions have many gadget-chain vulnerabilities.", "Remove the explicit version to use the Boot-managed one, or upgrade to 2.15+.", "cve-jackson")
		case (d.Artifact == "h2" || d.Artifact == "spring-boot-devtools") && (d.Scope == "" || d.Scope == "compile"):
			add(at, "medium", d.Artifact+" is on the production classpath", d.Artifact+" belongs in development or tests; in production it adds attack surface (H2 console, devtools remote).", "<scope>"+map[string]string{"h2": "runtime", "spring-boot-devtools": "runtime"}[d.Artifact]+"</scope> and <optional>true</optional> for devtools, or test scope.", "dep-dev-only")
		case d.Group == "junit" && d.Artifact == "junit":
			add(at, "low", "JUnit 4 alongside JUnit 5", "Spring Boot 3 uses JUnit 5; JUnit 4 tests run only through the vintage engine.", "Migrate tests to org.junit.jupiter and remove junit:junit.", "dep-junit4")
		case d.Version != "" && !strings.HasPrefix(d.Version, "${") && b.SpringBoot != "" && strings.HasPrefix(d.Group, "org.springframework"):
			add(at, "low", "Version pinned for a Boot-managed dependency", d.Artifact+" "+d.Version+" overrides the version Spring Boot "+b.SpringBoot+" tested together.", "Remove the <version> and let the Spring Boot BOM manage it.", "dep-pinned-managed")
		}
	}
	return out
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

func major(v string) int { return atoi(strings.SplitN(v, ".", 2)[0]) }

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// versionLess compares dotted numeric versions; unknown versions are not reported.
func versionLess(v, than string) bool {
	if v == "" || strings.ContainsAny(v, "${") {
		return false
	}
	a, b := strings.Split(v, "."), strings.Split(than, ".")
	for i := 0; i < len(b); i++ {
		var x int
		if i < len(a) {
			fmt.Sscanf(a[i], "%d", &x)
		}
		y := atoi(b[i])
		if x != y {
			return x < y
		}
	}
	return false
}
