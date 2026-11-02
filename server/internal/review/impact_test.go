package review

import (
	"strings"
	"testing"

	"codist/internal/gitx"
	"codist/internal/javaidx"
)

func TestImpactFollowsCallsToRoutesAndTests(t *testing.T) {
	sources := map[string][]byte{
		"src/main/java/a/PlanService.java": []byte(`package a;
public class PlanService {
    public int limit(String plan) {
        return 1;
    }
    public int unused() { return 2; }
}`),
		"src/main/java/a/Borrowing.java": []byte(`package a;
public interface Borrowing { void borrow(String who); }`),
		"src/main/java/a/LoanService.java": []byte(`package a;
public class LoanService implements Borrowing {
    private final PlanService plans;
    LoanService(PlanService plans) { this.plans = plans; }
    public void borrow(String who) {
        int n = plans.limit(who);
    }
}`),
		"src/main/java/a/LoanController.java": []byte(`package a;
import org.springframework.web.bind.annotation.*;
@RestController
@RequestMapping("/loans")
public class LoanController {
    private final Borrowing service;
    LoanController(Borrowing service) { this.service = service; }
    @PostMapping
    public void borrow(@RequestBody String who) {
        service.borrow(who);
    }
}`),
		"src/test/java/a/LoanServiceTest.java": []byte(`package a;
class LoanServiceTest {
    void borrows() {
        LoanService s = new LoanService(null);
        s.borrow("x");
    }
}`),
		"src/main/java/a/Unrelated.java": []byte(`package a;
import java.util.List;
class Unrelated {
    void x(List<String> l) { l.size(); }
    int limit() { return 0; }
}`),
	}
	g := javaidx.Index(sources, "t")
	cg := javaidx.BuildCallGraph(sources, &g)
	path := "src/main/java/a/PlanService.java"
	a := Analysis{Commit: gitx.Commit{SHA: "s"}, Files: []gitx.FileChange{{Path: path, NewPath: path, AddedLines: []int{4}}}}
	rep := buildImpact(a, sources, cg, func(string) (string, bool) {
		return "package a;\npublic class PlanService {\n    public int limit(String p, int extra) { return 1; }\n    public int gone() { return 3; }\n    public int unused() { return 2; }\n}", true
	})

	byName := map[string]ChangedMethod{}
	for _, c := range rep.Changed {
		byName[c.Method] = c
	}
	limit, ok := byName["limit"]
	if !ok || limit.Status != "signature" {
		t.Fatalf("changed = %+v", rep.Changed)
	}
	if _, ok := byName["unused"]; ok {
		t.Error("a method whose lines did not change is not changed")
	}
	if byName["gone"].Status != "removed" {
		t.Errorf("gone = %+v", byName["gone"])
	}
	got := map[string]ImpactPlace{}
	for _, p := range rep.Callers {
		got[p.Class+"."+p.Method] = p
	}
	if p := got["LoanService.borrow"]; !p.Confirmed || p.Depth != 1 {
		t.Errorf("direct caller: %+v (all %+v)", p, rep.Callers)
	}
	if p := got["LoanController.borrow"]; !p.Confirmed {
		t.Errorf("the controller calls it through the interface: %+v (all %+v)", p, rep.Callers)
	}
	if _, ok := got["Unrelated.x"]; ok {
		t.Error("l.size() has nothing to do with this")
	}
	if len(rep.Routes) != 1 || !strings.Contains(rep.Routes[0].Route, "/loans") {
		t.Errorf("routes = %+v", rep.Routes)
	}
	if limit.Tests != 1 || limit.TestedBy[0] != "LoanServiceTest.borrows" {
		t.Errorf("tests = %+v", limit)
	}
	text := impactSection(rep, path)
	for _, want := range []string{"limit() [signature]", "LoanService.borrow", "/loans", "LoanServiceTest.borrows", "gone() [removed]"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
}

func TestImpactCrossesServicesThroughFeign(t *testing.T) {
	sources := map[string][]byte{
		"customer-service/src/main/java/c/CustomerController.java": []byte(`package c;
import org.springframework.web.bind.annotation.*;
@RestController
@RequestMapping("/customers")
public class CustomerController {
    @GetMapping("/{id}")
    public String get(@PathVariable Long id) { return "x"; }
}`),
		"order-service/src/main/java/o/CustomerClient.java": []byte(`package o;
import org.springframework.cloud.openfeign.FeignClient;
import org.springframework.web.bind.annotation.*;
@FeignClient(name = "customer-service")
public interface CustomerClient {
    @GetMapping("/customers/{customerId}")
    String find(@PathVariable Long customerId);
}`),
		"order-service/src/main/java/o/OrderService.java": []byte(`package o;
public class OrderService {
    private final CustomerClient customers;
    OrderService(CustomerClient customers) { this.customers = customers; }
    public String place(Long id) { return customers.find(id); }
}`),
	}
	g := javaidx.Index(sources, "t")
	cg := javaidx.BuildCallGraph(sources, &g)
	got := []string{}
	for _, r := range cg.CallersOf("customer-service/src/main/java/c/CustomerController.java", "CustomerController", "get", 6) {
		got = append(got, r.Method.Class+"."+r.Method.Name)
	}
	joined := strings.Join(got, ",")
	if !strings.Contains(joined, "CustomerClient.find") || !strings.Contains(joined, "OrderService.place") {
		t.Errorf("should reach the other service: %s", joined)
	}
}
