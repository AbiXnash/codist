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

func TestQuotesCodeAcrossLines(t *testing.T) {
	lines := []string{"    if (request.priority() > 0) {", "        throw new IllegalArgumentException(\"priority must not be negative\");", "    }", "    save();"}
	for _, q := range []string{
		`if (request.priority() > 0) { throw new IllegalArgumentException("priority must not be negative"); }`,
		"if (request.priority() > 0) {\n  throw new IllegalArgumentException(\"priority must not be negative\");\n}",
		" 1 | if (request.priority() > 0) {\n>> 2 | throw new IllegalArgumentException(\"priority must not be negative\");",
	} {
		if !quotesCode(lines, q) {
			t.Errorf("%q spans lines of the file", q)
		}
	}
	chained := []string{"return repository.findAll().stream()", "        .limit(5)", "        .toList();"}
	if !quotesCode(chained, "return repository.findAll().stream().limit(5).toList();") {
		t.Error("a chain joined without spaces is still the same code")
	}
	if quotesCode(lines, `if (request.priority() < 0) { throw new IllegalArgumentException("priority must not be negative"); }`) {
		t.Error("a quote with different code is not in the file")
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

func TestDropReasonKeepsQuotesHonest(t *testing.T) {
	lines := []string{"class A {", "  BigDecimal total() {", "    return item.getAmount();", "  }", "", "", "", "", "", "", "", "", "", "", "", "  int y = 2;", "}"}
	bad := map[string]any{"title": "Precision", "ref": "bigdecimal-precision", "evidence": "item.getAmount()"}
	if why := dropReason(bad, 3, 3, lines, nil, false); why != "its quote does not show that problem" {
		t.Errorf("reading a BigDecimal proves nothing: %q", why)
	}
	if why := dropReason(map[string]any{"title": "Wrong sum", "evidence": "int y = 2;"}, 3, 3, lines, nil, false); why != "quotes code far from the line it names" {
		t.Errorf("real code from elsewhere: %q", why)
	}
	if why := dropReason(map[string]any{"title": "Wrong sum", "evidence": "return item.getAmount();"}, 3, 3, lines, nil, false); why != "" {
		t.Errorf("a true quote at its line stays: %q", why)
	}
}
