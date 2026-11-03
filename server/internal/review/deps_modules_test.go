package review

import (
	"slices"
	"testing"
)

func TestReadBuildFindsDependenciesInMavenModules(t *testing.T) {
	files := map[string]string{
		"pom.xml": `<project><parent><artifactId>spring-boot-starter-parent</artifactId><version>3.3.5</version></parent><artifactId>root</artifactId>
			<properties><java.version>21</java.version><guava.version>33.0</guava.version><spring-cloud.version>2023.0.3</spring-cloud.version></properties>
			<modules><module>common</module><module>svc/order</module></modules></project>`,
		"common/pom.xml": `<project><artifactId>common</artifactId><dependencies>
			<dependency><groupId>org.springframework.boot</groupId><artifactId>spring-boot-starter-web</artifactId></dependency></dependencies></project>`,
		"svc/order/pom.xml": `<project><artifactId>order</artifactId><dependencies>
			<dependency><groupId>x</groupId><artifactId>common</artifactId><version>${project.version}</version></dependency>
			<dependency><groupId>org.springframework.boot</groupId><artifactId>spring-boot-starter-web</artifactId></dependency>
			<dependency><groupId>com.google</groupId><artifactId>guava</artifactId><version>${guava.version}</version></dependency></dependencies></project>`,
	}
	b, file, _, ok := ReadBuild(func(p string) (string, bool) { s, has := files[p]; return s, has })
	if !ok || file != "pom.xml" || b.SpringBoot != "3.3.5" || b.Java != "21" {
		t.Fatalf("root: %v %q %+v", ok, file, b)
	}
	if !slices.Equal(b.Modules, []string{"common", "svc/order"}) {
		t.Fatalf("modules: %v", b.Modules)
	}
	if len(b.Deps) != 2 {
		t.Fatalf("a module of this project is not a dependency, and repeats merge: %+v", b.Deps)
	}
	web, guava := b.Deps[0], b.Deps[1]
	if web.Artifact != "spring-boot-starter-web" || !slices.Equal(web.Modules, []string{"common", "svc/order"}) {
		t.Errorf("merged across modules: %+v", web)
	}
	if guava.Version != "33.0" {
		t.Errorf("a version from the parent's properties should resolve: %+v", guava)
	}
}

func TestReadBuildFindsGradleSubprojects(t *testing.T) {
	files := map[string]string{
		"build.gradle":           `plugins { id 'org.springframework.boot' version '3.4.1' }`,
		"settings.gradle":        "rootProject.name = 'x'\ninclude ':api', ':libs:core'\n",
		"api/build.gradle":       "dependencies {\n implementation 'com.a:one:1.0'\n}\n",
		"libs/core/build.gradle": "dependencies {\n testImplementation 'com.b:two:2.0'\n}\n",
	}
	b, _, _, ok := ReadBuild(func(p string) (string, bool) { s, has := files[p]; return s, has })
	if !ok || len(b.Deps) != 2 || !slices.Equal(b.Modules, []string{"api", "libs/core"}) {
		t.Fatalf("%v %+v", ok, b)
	}
}

func TestReadBuildSingleModuleStillWorks(t *testing.T) {
	files := map[string]string{"pom.xml": `<project><dependencies><dependency><groupId>g</groupId><artifactId>a</artifactId><version>1</version></dependency></dependencies></project>`}
	b, _, _, ok := ReadBuild(func(p string) (string, bool) { s, has := files[p]; return s, has })
	if !ok || len(b.Deps) != 1 || len(b.Modules) != 0 || b.Deps[0].Modules != nil {
		t.Fatalf("%v %+v", ok, b)
	}
}

func TestReadBuildNamesAParentOutsideTheRepository(t *testing.T) {
	files := map[string]string{"pom.xml": `<project><parent><groupId>com.acme</groupId><artifactId>company-parent</artifactId><version>4.2</version></parent><artifactId>app</artifactId>
		<dependencies><dependency><groupId>org.springframework.boot</groupId><artifactId>spring-boot-starter-web</artifactId></dependency></dependencies></project>`}
	b, _, _, ok := ReadBuild(func(p string) (string, bool) { s, has := files[p]; return s, has })
	if !ok || b.Outside != "com.acme:company-parent:4.2" || b.SpringBoot != "" || !unresolved(b) {
		t.Fatalf("%+v", b)
	}
}

func TestParseEffectiveReadsEveryProject(t *testing.T) {
	got := parseEffective(`<?xml version="1.0"?><projects><project><dependencies><dependency><groupId>a</groupId><artifactId>one</artifactId><version>1.2</version></dependency></dependencies></project>
		<project><dependencies><dependency><groupId>b</groupId><artifactId>two</artifactId><version>${x}</version></dependency><dependency><groupId>c</groupId><artifactId>three</artifactId><version>3</version></dependency></dependencies></project></projects>`)
	if got["a:one"] != "1.2" || got["c:three"] != "3" || got["b:two"] != "" {
		t.Fatalf("%v", got)
	}
	if one := parseEffective(`<project><dependencies><dependency><groupId>a</groupId><artifactId>one</artifactId><version>9</version></dependency></dependencies></project>`); one["a:one"] != "9" {
		t.Fatalf("%v", one)
	}
}
