package review

import (
	"strings"
	"testing"

	"codist/internal/javaidx"
)

func TestQuotesCode(t *testing.T) {
	lines := []string{"class A {", "    int  total = price * 2;", "}"}
	for _, q := range []string{"int total = price * 2;", "  2 | int total = price * 2;", ">> 2 | int  total = price * 2;", "price * 2"} {
		if !quotesCode(lines, q) {
			t.Errorf("%q is in the file", q)
		}
	}
	for _, q := range []string{"int total = price * 3;", "repository.save(order);", "  "} {
		if quotesCode(lines, q) {
			t.Errorf("%q is not in the file", q)
		}
	}
}

func TestCodeFactsAnswersFromTheCode(t *testing.T) {
	sources := map[string][]byte{
		"src/main/java/a/PlanService.java": []byte(`package a;
public class PlanService {
    public int limit(String plan) { return 1; }
}`),
		"src/main/java/a/LoanController.java": []byte(`package a;
import org.springframework.web.bind.annotation.*;
@RestController
public class LoanController {
    private final PlanService plans;
    LoanController(PlanService plans) { this.plans = plans; }
    @PostMapping("/loans")
    public int borrow(String who) { return plans.limit(who); }
}`),
		"src/test/java/a/PlanTest.java": []byte(`package a;
class PlanTest {
    void t() { PlanService s = new PlanService(); s.limit("y"); }
}`),
	}
	g := javaidx.Index(sources, "t")
	cg := javaidx.BuildCallGraph(sources, &g)
	got := CodeFacts("What happens if I modify limit in PlanService?", "", &g, cg)
	for _, want := range []string{"PlanService.limit()", "LoanController.borrow", "/loans", "PlanTest.t"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if byClass := CodeFacts("Who depends on PlanService?", "", &g, cg); !strings.Contains(byClass, "PlanService.limit()") {
		t.Errorf("a class name should bring its methods:\n%s", byClass)
	}
	if none := CodeFacts("How do I run the tests?", "", &g, cg); none != "" {
		t.Errorf("no names, no facts: %q", none)
	}
}

func TestVagueAdviceIsDropped(t *testing.T) {
	for _, title := range []string{"Consider using a constant", "consider pagination", "Ensure that this works"} {
		if !vagueAdvice(title) {
			t.Errorf("%q says nothing is wrong", title)
		}
	}
	for _, title := range []string{"Check-then-act race", "Considerable memory use per request", "Magic number"} {
		if vagueAdvice(title) {
			t.Errorf("%q names a problem", title)
		}
	}
}

func TestReviewPromptNowAsksForEvidence(t *testing.T) {
	if !strings.Contains(FilePrompt(), "evidence") {
		t.Error("the review prompt must ask for evidence, or the check never runs")
	}
}
