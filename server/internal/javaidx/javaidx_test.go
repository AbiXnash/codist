package javaidx

import (
	"fmt"
	"strings"
	"testing"
)

const orderService = `package com.acme.order;

import com.acme.customer.CustomerRepository;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import java.math.BigDecimal;

@Service
public class OrderService {
    private final OrderRepository orders;
    private final CustomerRepository customers;
    private final JdbcTemplate jdbc;
    private int ordersToday = 0;

    public OrderService(OrderRepository orders, CustomerRepository customers, JdbcTemplate jdbc) {
        this.orders = orders;
        this.customers = customers;
        this.jdbc = jdbc;
    }

    public Order create(String email) {
        var customer = customers.findByEmail(email).get();
        ordersToday++;
        Order o = new Order();
        o.setTotal(new BigDecimal(0.9));
        return orders.save(o);
    }

    public void cancel(Long id) {
        markRefund(id);
    }

    @Transactional
    private void markRefund(Long id) {
        try {
            jdbc.update("UPDATE orders SET status = 'REFUND' WHERE id = " + id);
        } catch (Exception e) {
        }
    }
}
`

const orderController = `package com.acme.order;

import org.springframework.web.bind.annotation.*;

@RestController
@RequestMapping("/api/orders")
public class OrderController {
    private final OrderService service;

    public OrderController(OrderService service) { this.service = service; }

    @PostMapping
    public Order create(@RequestBody CreateOrderRequest req) { return service.create(req.email()); }

    @GetMapping("/{id}")
    public Order get(@PathVariable Long id) { return null; }

    @RequestMapping(value = "/legacy", method = RequestMethod.PUT)
    public void legacy() {}
}
`

var sources = map[string][]byte{
	"src/main/java/com/acme/order/OrderService.java":    []byte(orderService),
	"src/main/java/com/acme/order/OrderController.java": []byte(orderController),
	"src/main/java/com/acme/order/Order.java": []byte(`package com.acme.order;
import jakarta.persistence.*;
@Entity @Table(name = "orders")
public class Order { @Id private Long id; public void setTotal(java.math.BigDecimal t) {} }`),
	"src/main/java/com/acme/order/OrderRepository.java": []byte(`package com.acme.order;
public interface OrderRepository extends org.springframework.data.jpa.repository.JpaRepository<Order, Long> {}`),
	"src/main/java/com/acme/order/CreateOrderRequest.java": []byte(`package com.acme.order;
public record CreateOrderRequest(String email) {}`),
	"src/main/java/com/acme/customer/CustomerRepository.java": []byte(`package com.acme.customer;
import org.springframework.data.jpa.repository.JpaRepository;
public interface CustomerRepository extends JpaRepository<Customer, Long> {
    java.util.Optional<Customer> findByEmail(String email);
}`),
	"src/main/java/com/acme/customer/Customer.java": []byte(`package com.acme.customer;
@jakarta.persistence.Entity
public class Customer { enum Tier { GOLD, SILVER } }`),
}

func TestIndex(t *testing.T) {
	g := Index(sources, "test")
	nodes := map[string]Node{}
	for _, n := range g.Nodes {
		nodes[n.ID] = n
	}
	want := map[string]string{
		"com.acme.order.OrderService":          "service",
		"com.acme.order.OrderController":       "controller",
		"com.acme.order.Order":                 "entity",
		"com.acme.order.OrderRepository":       "repository",
		"com.acme.order.CreateOrderRequest":    "dto",
		"com.acme.customer.CustomerRepository": "repository",
		"com.acme.customer.Customer":           "entity",
		"com.acme.customer.Customer.Tier":      "other",
	}
	for id, st := range want {
		n, ok := nodes[id]
		if !ok {
			t.Errorf("missing node %s", id)
			continue
		}
		if n.Stereotype != st {
			t.Errorf("%s: stereotype %q, want %q", id, n.Stereotype, st)
		}
	}
	if k := nodes["com.acme.order.OrderRepository"].Kind; k != "interface" {
		t.Errorf("OrderRepository kind = %q", k)
	}
	if k := nodes["com.acme.order.CreateOrderRequest"].Kind; k != "record" {
		t.Errorf("CreateOrderRequest kind = %q", k)
	}

	edges := map[string]string{}
	for _, e := range g.Edges {
		edges[e.Source+" -> "+e.Target] = e.Kind
	}
	for edge, kind := range map[string]string{
		"com.acme.order.OrderController -> com.acme.order.OrderService":       "injects",
		"com.acme.order.OrderService -> com.acme.order.OrderRepository":       "injects",
		"com.acme.order.OrderService -> com.acme.customer.CustomerRepository": "injects",
		"com.acme.order.OrderService -> com.acme.order.Order":                 "uses",
		"com.acme.order.OrderController -> com.acme.order.CreateOrderRequest": "uses",
		"com.acme.customer.CustomerRepository -> com.acme.customer.Customer":  "uses",
	} {
		if edges[edge] != kind {
			t.Errorf("edge %s = %q, want %q", edge, edges[edge], kind)
		}
	}
	if _, ok := edges["com.acme.customer.Customer -> com.acme.customer.Customer.Tier"]; ok {
		t.Error("edges to own nested types should be dropped")
	}

	ep := strings.Join(nodes["com.acme.order.OrderController"].Endpoints, "\n")
	for _, want := range []string{"POST /api/orders  → create()", "GET /api/orders/{id}  → get()", "PUT /api/orders/legacy  → legacy()"} {
		if !strings.Contains(ep, want) {
			t.Errorf("endpoints %q missing %q", ep, want)
		}
	}

	var sigs []string
	for _, m := range nodes["com.acme.order.OrderService"].Members {
		sigs = append(sigs, m.Signature)
	}
	if !strings.Contains(strings.Join(sigs, "\n"), "public Order create(String email)") {
		t.Errorf("signatures: %v", sigs)
	}
}

func allLines(src string) map[int]bool {
	m := map[int]bool{}
	for i := 1; i <= strings.Count(src, "\n")+1; i++ {
		m[i] = true
	}
	return m
}

func TestRules(t *testing.T) {
	ctx := Context{IsEntity: func(s string) bool { return s == "Order" || s == "Customer" }}
	got := map[string]int{}
	for _, f := range Check([]byte(orderService), allLines(orderService), ctx) {
		got[f.Title] = f.Line
	}
	for title, line := range map[string]int{
		"Optional.get() without a presence check":       23,
		"Mutable state in a singleton bean":             14,
		"new BigDecimal(double) is imprecise":           26,
		"Self-invocation bypasses @Transactional":       31,
		"@Transactional on a private method is ignored": 34,
		"SQL built by string concatenation":             37,
		"Exception swallowed by an empty catch block":   38,
	} {
		if got[title] != line {
			t.Errorf("%q at line %d, want %d (all: %v)", title, got[title], line, got)
		}
	}

	ctrl := map[string]bool{}
	for _, f := range Check([]byte(orderController), allLines(orderController), ctx) {
		ctrl[f.Title] = true
	}
	if !ctrl["@RequestBody is not validated"] || !ctrl["JPA entity Order returned from a controller"] {
		t.Errorf("controller findings: %v", ctrl)
	}

	if n := len(Check([]byte(orderService), map[int]bool{1: true}, ctx)); n != 0 {
		t.Errorf("findings outside changed lines should be dropped, got %d", n)
	}
}

const dataConstant = `package com.acme.common;
public final class DataConstant {
    public static final String LOG_PREFIX = "CUSTOMER";
    public static final int LIMIT = 10;
}`

const productService = `package com.acme.product;

import com.acme.common.DataConstant;

@org.springframework.stereotype.Service
public class ProductService {
    public void create(String req) {
        System.out.println(DataConstant.LOG_PREFIX + "Request: " + req);
    }
}`

const customerService = `package com.acme.customer;

import com.acme.common.DataConstant;

@org.springframework.stereotype.Service
public class CustomerService {
    public void create(String req) {
        System.out.println(DataConstant.LOG_PREFIX + "Request: " + req);
    }
}`

func TestDomainConstantAndConsole(t *testing.T) {
	g := Index(map[string][]byte{
		"src/main/java/com/acme/common/DataConstant.java":         []byte(dataConstant),
		"src/main/java/com/acme/product/ProductService.java":      []byte(productService),
		"src/main/java/com/acme/customer/CustomerService.java":    []byte(customerService),
		"src/main/java/com/acme/customer/CustomerController.java": []byte("package com.acme.customer;\nclass CustomerController {}"),
	}, "test")
	ctx := ProjectContext(&g)
	if ctx.Constants["DataConstant.LOG_PREFIX"].Value != `"CUSTOMER"` || ctx.Constants["DataConstant.LIMIT"].Value != "10" {
		t.Fatalf("constants: %+v", ctx.Constants)
	}
	if !ctx.Domains["customer"] || !ctx.Domains["product"] || ctx.Domains["common"] {
		t.Fatalf("domains: %v", ctx.Domains)
	}

	ctx.Path = "src/main/java/com/acme/product/ProductService.java"
	titles := map[string]RuleFinding{}
	for _, f := range Check([]byte(productService), allLines(productService), ctx) {
		titles[f.Title] = f
	}
	if f, ok := titles[`DataConstant.LOG_PREFIX is "CUSTOMER" but used in ProductService`]; !ok || f.Line != 8 {
		t.Errorf("foreign constant not flagged on line 8: %v", titles)
	}
	if f, ok := titles["Use a logger instead of System.out.println"]; !ok || !strings.Contains(f.Suggestion, `log.info("{}Request: {}", DataConstant.LOG_PREFIX, req)`) ||
		!strings.Contains(f.Suggestion, "LoggerFactory.getLogger(ProductService.class)") {
		t.Errorf("console rule: %+v", f)
	}

	ctx.Path = "src/main/java/com/acme/customer/CustomerService.java"
	for _, f := range Check([]byte(customerService), allLines(customerService), ctx) {
		if strings.Contains(f.Title, "but used in") {
			t.Errorf("CUSTOMER prefix in CustomerService is correct, got %q", f.Title)
		}
	}
}

const recon = `package com.acme.recon;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

@org.springframework.stereotype.Service
public class ReconService {
    private static final Logger log = LoggerFactory.getLogger(ReconService.class);
    private final GatewayClient gatewayClient;
    private final PaymentRepository paymentRepository;

    ReconService(GatewayClient g, PaymentRepository r) { this.gatewayClient = g; this.paymentRepository = r; }

    public void reconcile(java.util.List<Payment> payments) {
        for (Payment p : payments) {
            log.info("Checking payment {}", p.getId());
            Status s = gatewayClient.status(p.getId());
            log.info("Gateway said {}", s);
            if (!s.equals(p.getStatus())) {
                log.warn("Mismatch {}", p.getId());
            }
        }
        payments.forEach(p -> paymentRepository.findById(p.getId()));
    }
}`

func TestLoopRules(t *testing.T) {
	got := map[string]RuleFinding{}
	for _, f := range Check([]byte(recon), allLines(recon), Context{}) {
		got[f.ID+"@"+fmt.Sprint(f.Line)] = f
	}
	if f, ok := got["remote-call-in-loop@15"]; !ok || !strings.Contains(f.Title, "gatewayClient.status()") {
		t.Errorf("HTTP call in loop not flagged: %v", got)
	}
	if f, ok := got["logging-in-loop@15"]; !ok || !strings.Contains(f.Title, "3 log statement") {
		t.Errorf("logging in loop not flagged: %v", got)
	}
	if f, ok := got["remote-call-in-loop@23"]; !ok || !strings.Contains(f.Title, "paymentRepository.findById()") {
		t.Errorf("repository call in forEach not flagged: %v", got)
	}
}

const userCustomerService = `package com.demo.shop.customer;

import com.demo.shop.common.NotFoundException;
import com.demo.shop.customer.CustomerDto.CreateCustomerRequest;

import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;

@Service
public class CustomerService {
    private static final String LOG_PREFIX = "CUSTOMER";

    private CustomerRepository customers;

    @Transactional
    public Customer create(CreateCustomerRequest req) {
        System.out.println(LOG_PREFIX + "Request: " + req);
        if (customers.findByEmail(req.email()).isPresent()) {
            throw new IllegalStateException("Email already registered: " + req.email());
        }
        System.out.println("CUSTOMER" + "Request: " + req);
        endLog();
        return customers.save(new Customer(req.email(), req.name()));
    }

    @Transactional(readOnly = true)
    public Customer get(Long id) {
        return customers.findById(id).orElseThrow(() -> new NotFoundException("No customer " + id));
    }

    private static void endLog() {
        System.out.println("CUSTOMER" + "Service Ended");
    }
}`

func TestUserHiddenBugs(t *testing.T) {
	ctx := Context{Beans: map[string]string{"CustomerRepository": "repository"}}
	got := map[string][]int{}
	for _, f := range Check([]byte(userCustomerService), allLines(userCustomerService), ctx) {
		got[f.ID] = append(got[f.ID], f.Line)
	}
	want := map[string][]int{
		"uninjected-dependency":       {13},
		"literal-instead-of-constant": {21, 32},
		"duplicate-log":               {21},
		"log-helper-method":           {31},
	}
	for id, lines := range want {
		if fmt.Sprint(got[id]) != fmt.Sprint(lines) {
			t.Errorf("%s at %v, want %v (all: %v)", id, got[id], lines, got)
		}
	}

	// The original version, with a constructor, is fine.
	injected := strings.Replace(userCustomerService, "private CustomerRepository customers;",
		"private final CustomerRepository customers;\n    CustomerService(CustomerRepository c) { this.customers = c; }", 1)
	for _, f := range Check([]byte(injected), allLines(injected), ctx) {
		if f.ID == "uninjected-dependency" {
			t.Errorf("injected field reported: %v", f.Title)
		}
	}
}

func TestModulesWithSameClassNames(t *testing.T) {
	entity := "package payroll;\npublic class Employee {}\n"
	repo := "package payroll;\npublic interface EmployeeRepository {}\n"
	controller := `package payroll;
import org.springframework.web.bind.annotation.RestController;
@RestController
public class EmployeeController {
    private final EmployeeRepository repository;
    EmployeeController(EmployeeRepository repository) { this.repository = repository; }
    Employee one() { return null; }
}
`
	g := Index(map[string][]byte{
		"rest/src/main/java/payroll/Employee.java":            []byte(entity),
		"rest/src/main/java/payroll/EmployeeRepository.java":  []byte(repo),
		"rest/src/main/java/payroll/EmployeeController.java":  []byte(controller),
		"links/src/main/java/payroll/Employee.java":           []byte(entity),
		"links/src/main/java/payroll/EmployeeRepository.java": []byte(repo),
		"links/src/main/java/payroll/EmployeeController.java": []byte(controller),
		"links/src/main/java/payroll/Order.java":              []byte("package payroll;\npublic class Order { Employee owner; }\n"),
		"nonrest/src/main/java/payroll/Lonely.java":           []byte("package payroll;\npublic class Lonely { Order o; Employee e; }\n"),
	}, "test")

	seen := map[string]bool{}
	for _, n := range g.Nodes {
		if seen[n.ID] {
			t.Errorf("duplicate node id %s", n.ID)
		}
		seen[n.ID] = true
	}
	if !seen["rest:payroll.Employee"] || !seen["links:payroll.Employee"] {
		t.Errorf("clashing types should be module-qualified: %v", seen)
	}
	if !seen["payroll.Order"] {
		t.Errorf("a unique type keeps its plain name: %v", seen)
	}

	edge := func(from, to string) bool {
		for _, e := range g.Edges {
			if e.Source == from && e.Target == to {
				return true
			}
		}
		return false
	}
	if !edge("rest:payroll.EmployeeController", "rest:payroll.EmployeeRepository") {
		t.Errorf("controller should inject its own module's repository: %+v", g.Edges)
	}
	if edge("rest:payroll.EmployeeController", "links:payroll.EmployeeRepository") {
		t.Error("link crossed modules")
	}
	if !edge("payroll.Order", "links:payroll.Employee") {
		t.Errorf("unique type should link within its module: %+v", g.Edges)
	}
	if !edge("nonrest:payroll.Lonely", "payroll.Order") && !edge("payroll.Lonely", "payroll.Order") {
		t.Errorf("a name declared once links from anywhere: %+v", g.Edges)
	}
	for _, e := range g.Edges {
		if e.Source == "payroll.Lonely" && strings.Contains(e.Target, "Employee") {
			t.Errorf("an ambiguous name should not be linked: %+v", e)
		}
	}

	path := "rest/src/main/java/payroll/Employee.java"
	if got := g.Qualify(TypesIn([]byte(entity), path)); got[0].ID != "rest:payroll.Employee" {
		t.Errorf("Qualify = %s", got[0].ID)
	}
	gone := "evolution/src/main/java/payroll/Employee.java"
	if got := g.Qualify(TypesIn([]byte(entity), gone)); got[0].ID == "rest:payroll.Employee" || got[0].ID == "links:payroll.Employee" {
		t.Errorf("a type from another module must not take this graph's ID: %s", got[0].ID)
	}
}

func TestMainAndTestSameName(t *testing.T) {
	src := "package a;\npublic class Helper {}\n"
	user := "package a;\npublic class User { Helper h; }\n"
	g := Index(map[string][]byte{
		"src/main/java/a/Helper.java":   []byte(src),
		"src/test/java/a/Helper.java":   []byte(src),
		"src/test/java/a/UserTest.java": []byte("package a;\npublic class UserTest { Helper h; }\n"),
		"src/main/java/a/User.java":     []byte(user),
	}, "test")
	if len(g.Nodes) != 4 {
		t.Fatalf("nodes = %d", len(g.Nodes))
	}
	want := map[string]string{"a.User": "src/main/java/a/Helper.java:a.Helper", "a.UserTest": "src/test/java/a/Helper.java:a.Helper"}
	for from, to := range want {
		found := false
		for _, e := range g.Edges {
			if e.Source == from && e.Target == to {
				found = true
			}
		}
		if !found {
			t.Errorf("missing %s -> %s in %+v", from, to, g.Edges)
		}
	}
}

func TestDeadCodeAndRecommendations(t *testing.T) {
	src := `package shop;
import java.util.List;
import java.util.Map;
public class Bad {
    private int unusedField;
    private final List<String> names = List.of();
    // FIXME this breaks for empty carts
    // public int total(Cart cart) {
    //     return cart.sum();
    // }
    // if (cart == null) {
    //     throw new IllegalStateException();
    // }
    // Explain why prices are rounded here, in words.
    public int a(List<String> list, OrderRepository orderRepository) {
        int never = 3;
        if (true) { log.info("x" + list); }
        if (list.size() == 0) { }
        else { }
        if (list.isEmpty()) { return 1; } else { return 1; }
    }
    public boolean b(String s) {
        if (s.isEmpty()) { return true; } else { return false; }
    }
    int c(List<String> xs, List<Long> ids, OrderRepository orderRepository) {
        for (String x : xs) { for (String y : xs) { names.size(); } }
        for (Long id : ids) { orderRepository.findById(id); }
        try { names.size(); } catch (RuntimeException e) { log.error("failed"); }
        return 1;
    }
    private void neverCalled() { names.size(); }
    int d() { return 1; }
}`
	got := map[string]bool{}
	var lines []string
	all := map[int]bool{}
	for i := 1; i < 60; i++ {
		all[i] = true
	}
	for _, f := range Check([]byte(src), all, Context{Path: "src/main/java/shop/Bad.java"}) {
		got[f.ID] = true
		lines = append(lines, f.Title)
	}
	for _, id := range []string{"commented-out-code", "bug-comment", "unused-field", "unused-import", "unused-local", "unused-method", "constant-condition",
		"empty-block", "identical-branches", "simplify-boolean-return", "nested-loops", "query-in-loop", "better-method", "better-log"} {
		if !got[id] {
			t.Errorf("%s not reported; got %v", id, lines)
		}
	}
	// Prose comments are not code, and used things are not unused.
	clean := `package shop;
import java.util.List;
class Good {
    // Prices are rounded half up because finance reports use that rule.
    private final List<String> names = List.of();
    int n() { int k = names.size(); return k; }
}`
	if fs := Check([]byte(clean), all, Context{Path: "src/main/java/shop/Good.java"}); len(fs) != 0 {
		t.Errorf("false positives: %+v", fs)
	}
	g := Index(map[string][]byte{"src/main/java/shop/Bad.java": []byte(src)}, "t")
	for _, m := range g.Nodes[0].Members {
		if m.Name == "c" && (m.Cost == nil || m.Cost.LoopIO != 1 || m.Cost.DBCalls != 1 || m.Cost.Estimate != "O(n) round trips") {
			t.Errorf("cost of c: %+v", m.Cost)
		}
	}
}

func TestFeignLinksAndStaleRoutes(t *testing.T) {
	g := Index(map[string][]byte{
		"customer-service/src/main/java/c/CustomerController.java": []byte(`package c;
@RestController @RequestMapping("/api/v2/customers")
class CustomerController { @GetMapping("/{id}") String get(Long id) { return ""; } }`),
		"order-service/src/main/java/o/CustomerClient.java": []byte(`package o;
@FeignClient(name = "customer-service", url = "x")
interface CustomerClient { @GetMapping("/api/v2/customers/{customerId}") String get(Long id); }`),
		"report-service/src/main/java/r/OldClient.java": []byte(`package r;
@FeignClient(name = "customer-service", contextId = "old")
interface OldClient { @GetMapping("/api/customers/{id}") String get(Long id); }`),
		"report-service/src/main/java/r/ElsewhereClient.java": []byte(`package r;
@FeignClient(name = "billing-service")
interface ElsewhereClient { @GetMapping("/x") String get(); }`),
	}, "t")
	feign := map[string]bool{}
	for _, e := range g.Edges {
		if e.Kind == "feign" {
			feign[e.Source+">"+e.Target] = true
		}
	}
	if !feign["o.CustomerClient>c.CustomerController"] || len(feign) != 1 {
		t.Fatalf("feign edges (variable names must not matter): %v", feign)
	}
	ctx := ProjectContext(&g)
	if r := ctx.FeignUnserved["OldClient"]; len(r) != 1 || r[0] != "GET /api/customers/{}" {
		t.Fatalf("stale route: %v", ctx.FeignUnserved)
	}
	if _, reported := ctx.FeignUnserved["ElsewhereClient"]; reported {
		t.Fatal("a service in another repository cannot be judged")
	}
	all := map[int]bool{1: true, 2: true, 3: true}
	ctx.Path = "report-service/src/main/java/r/OldClient.java"
	src := []byte("package r;\n@FeignClient(name = \"customer-service\")\ninterface OldClient { @GetMapping(\"/api/customers/{id}\") String get(Long id); }")
	found := false
	for _, f := range Check(src, all, ctx) {
		found = found || (f.ID == "feign-route-missing" && f.Severity == "critical")
	}
	if !found {
		t.Fatal("stale route not reported")
	}
}

func TestLoopRulesOnlyCountWhatRunsPerItem(t *testing.T) {
	src := `package a;
class S {
    List<R> once() {
        return repository.findByStatus("A").stream().sorted().limit(5).map(R::from).toList();
    }
    void forEachSource() {
        for (Order o : repository.findAll()) { total += o.amount(); }
    }
    void notADatabase(List<Item> items) {
        for (Item item : items) { item.getPrice(); item.findOption(); }
    }
    void perItem(List<Long> ids) {
        ids.stream().map(id -> repository.findById(id)).toList();
    }
    void perIteration(List<Long> ids) {
        for (Long id : ids) { em.find(id); }
    }
}
`
	lines := map[int]bool{}
	var titles []string
	for _, f := range Check([]byte(src), allLines(src), Context{}) {
		lines[f.Line] = true
		titles = append(titles, fmt.Sprint(f.Line, " ", f.Title))
	}
	all := strings.Join(titles, "\n")
	for _, line := range []int{4, 7, 10} {
		if lines[line] {
			t.Errorf("line %d runs once or is no database call:\n%s", line, all)
		}
	}
	if !strings.Contains(all, "13 Database call in a loop: repository.findById()") {
		t.Errorf("a query per stream item must still be flagged:\n%s", all)
	}
	if !strings.Contains(all, "16 Database call in a loop: em.find()") {
		t.Errorf("a query per iteration must still be flagged:\n%s", all)
	}
	if strings.Contains(all, "Nested loops") {
		t.Errorf("a chain of stream steps is one pass:\n%s", all)
	}
}
