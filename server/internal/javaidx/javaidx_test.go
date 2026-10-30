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
