package review

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"codist/internal/gitx"
)

type fakeReviews map[string]Review

func (f fakeReviews) Get(sha string) (Review, bool) { r, ok := f[sha]; return r, ok }

func titlesOf(points []Point) string {
	var out []string
	for _, p := range points {
		out = append(out, p.Title)
	}
	return strings.Join(out, "\n")
}

func TestTeamProfiles(t *testing.T) {
	var commits []gitx.AuthoredCommit
	reviews := fakeReviews{}
	add := func(sha, who, path string, lines int, msg string) {
		commits = append(commits, gitx.AuthoredCommit{SHA: sha, Author: who, Files: []gitx.FileLines{{Path: path, Added: lines}}})
		reviews[sha] = Review{Status: StatusDone, MessageProblem: msg}
	}
	for i := 0; i < 20; i++ {
		path := "order-service/src/main/java/OrderController.java"
		if i%2 == 1 {
			path = "order-service/src/test/java/OrderTest.java"
		}
		add(fmt.Sprintf("a%d", i), "Ana", path, 100, "")
	}
	for _, sha := range []string{"r1", "r2", "r3", "r4", "r5"} {
		add(sha, "Ravi", "pay-service/src/main/java/Pay.java", 500, "vague")
	}
	add("n1", "New", "pay-service/src/main/java/Other.java", 30, "vague")

	mine := func(id, who, sha, sev, ref string) Issue {
		return Issue{ID: id, State: "open", Severity: sev, FirstBy: who, FirstSeen: sha, Ref: ref, Title: ref, Suggestion: "Use Objects.equals(a, b). Then more."}
	}
	day := 24 * time.Hour.Milliseconds()
	issues := []Issue{
		mine("1", "Ravi", "r1", "high", "string-eq"),
		mine("2", "Ravi", "r2", "medium", "string-eq"),
		mine("3", "Ravi", "r3", "medium", "other"),
		mine("4", "New", "n1", "critical", "secret"),
		{ID: "5", State: "resolved", Severity: "high", FirstBy: "Ravi", FirstSeen: "r4", Ref: "x", Title: "x", FixedBy: "Ana", FirstTime: day, FixedAt: 3 * day},
		{ID: "6", State: "ignored", Severity: "critical", FirstBy: "Ana", FirstSeen: "a1", Ref: "y", Title: "y"},
	}
	stereotypes := map[string]string{"order-service/src/main/java/OrderController.java": "controller"}
	rep := Team(commits, issues, reviews, stereotypes, nil)

	by := map[string]DevProfile{}
	for _, p := range rep.People {
		by[p.Name] = p
	}
	ana, ravi, fresh := by["Ana"], by["Ravi"], by["New"]
	if ana.Commits != 20 || ana.Reviewed != 20 || ana.TestShare != 50 || ana.Added != 2000 {
		t.Errorf("Ana = %+v", ana)
	}
	if ana.Introduced != 0 {
		t.Errorf("an issue judged not a problem should not count against her: %+v", ana)
	}
	if ana.Skills[0].Name == "" || len(ana.Skills) != 2 {
		t.Errorf("skills = %+v", ana.Skills)
	}
	if ana.Fixed != 1 || ana.FixedOthers != 1 || ana.AvgFixDays != 2 {
		t.Errorf("fixes = %+v", ana)
	}
	for _, want := range []string{"Fixes other people", "Writes tests", "Keeps commits small", "Clear commit messages", "Fewer serious issues"} {
		if !strings.Contains(titlesOf(ana.Strengths), want) {
			t.Errorf("Ana should get %q:\n%s", want, titlesOf(ana.Strengths))
		}
	}
	for _, want := range []string{"Urgent issues", "Little test code", "Large commits", "Vague commit messages", "More serious issues", "Repeats: string-eq"} {
		if !strings.Contains(titlesOf(ravi.Improve), want) {
			t.Errorf("Ravi should get %q:\n%s", want, titlesOf(ravi.Improve))
		}
	}
	for _, p := range ravi.Improve {
		if strings.HasPrefix(p.Title, "Repeats") && p.Advice != "Use Objects.equals(a, b)." {
			t.Errorf("advice = %q", p.Advice)
		}
	}
	if strings.Contains(titlesOf(ravi.Improve), "Repeats: other") {
		t.Error("one commit is not a habit")
	}

	// One commit proves nothing about quality, but an open critical is still worth saying.
	if fresh.EnoughEvidence || fresh.Rate != 0 {
		t.Errorf("New = %+v", fresh)
	}
	if got := titlesOf(fresh.Improve); got != "Urgent issues still open" {
		t.Errorf("little evidence, so only the facts: %q", got)
	}
}

func TestNoComparisonWithoutSomeoneToCompareWith(t *testing.T) {
	var commits []gitx.AuthoredCommit
	reviews := fakeReviews{}
	for _, sha := range []string{"1", "2", "3", "4", "5"} {
		commits = append(commits, gitx.AuthoredCommit{SHA: sha, Author: "Solo", Files: []gitx.FileLines{{Path: "src/main/java/a/A.java", Added: 100}}})
		reviews[sha] = Review{Status: StatusDone}
	}
	rep := Team(commits, nil, reviews, nil, nil)
	got := titlesOf(rep.People[0].Strengths) + titlesOf(rep.People[0].Improve)
	if strings.Contains(got, "average") {
		t.Errorf("nobody to compare with: %s", got)
	}
}
