package incident

import (
	"strings"
	"testing"
)

const sampleLog = `2026-10-30 10:15:02.118  INFO 4242 --- [shop] [nio-8081-exec-3] c.d.shop.customer.CustomerController  : POST /api/customers
2026-10-30 10:15:02.131 ERROR 4242 --- [shop] [nio-8081-exec-3] o.a.c.c.C.[.[.[/].[dispatcherServlet]    : Servlet.service() for servlet [dispatcherServlet] threw exception
java.lang.NullPointerException: Cannot invoke "com.demo.shop.customer.CustomerRepository.findByEmail(String)" because "this.customers" is null
	at com.demo.shop.customer.CustomerService.create(CustomerService.java:18)
	at com.demo.shop.customer.CustomerService$$SpringCGLIB$$0.create(<generated>)
	at com.demo.shop.customer.CustomerController.create(CustomerController.java:22)
	at org.springframework.web.servlet.FrameworkServlet.service(FrameworkServlet.java:885)
	... 48 more
2026-10-30 10:15:09.551 ERROR 4242 --- [shop] [nio-8081-exec-7] o.a.c.c.C.[.[.[/].[dispatcherServlet]    : Servlet.service() for servlet [dispatcherServlet] threw exception
java.lang.NullPointerException: Cannot invoke "com.demo.shop.customer.CustomerRepository.findByEmail(String)" because "this.customers" is null
	at com.demo.shop.customer.CustomerService.create(CustomerService.java:18)
	at com.demo.shop.customer.CustomerService$$SpringCGLIB$$0.create(<generated>)
	at com.demo.shop.customer.CustomerController.create(CustomerController.java:22)
2026-10-30 10:16:40.002 ERROR 4242 --- [shop] [scheduling-1] c.demo.shop.recon.ReconService           : Recon failed for batch 88123
org.springframework.web.client.ResourceAccessException: I/O error on GET request for "http://gateway/status/77": Read timed out
	at org.springframework.web.client.RestTemplate.doExecute(RestTemplate.java:915)
	at com.demo.shop.recon.PaymentGatewayClient.status(PaymentGatewayClient.java:31)
	at com.demo.shop.recon.ReconService.reconcile(ReconService.java:29)
Caused by: java.net.SocketTimeoutException: Read timed out
	at java.base/sun.nio.ch.NioSocketImpl.timedRead(NioSocketImpl.java:288)
2026-10-30 10:17:00.000  WARN 4242 --- [shop] [main] o.h.e.j.s.SqlExceptionHelper : SQL Warning Code: 0
`

func TestParse(t *testing.T) {
	ev := Parse(sampleLog)
	if len(ev) < 2 {
		t.Fatalf("events: %+v", ev)
	}
	npe := ev[0]
	if npe.Exception != "java.lang.NullPointerException" || npe.Count != 2 || len(npe.Frames) < 3 || npe.Frames[0].Line != 18 {
		t.Errorf("npe: %+v", npe)
	}
	var recon *ErrorEvent
	for i := range ev {
		if ev[i].Exception == "org.springframework.web.client.ResourceAccessException" {
			recon = &ev[i]
		}
	}
	if recon == nil || len(recon.CausedBy) != 1 || recon.CausedBy[0] != "java.net.SocketTimeoutException: Read timed out" {
		t.Errorf("recon: %+v", recon)
	}
	tokens := Tokens(ev, func(c string) bool { return len(c) > 13 && c[:13] == "com.demo.shop" })
	for _, want := range []string{"ex:NullPointerException", "at:CustomerService.create", "ex:SocketTimeoutException"} {
		if !tokens[want] {
			t.Errorf("missing token %s in %v", want, tokens)
		}
	}
}

func TestNamedFields(t *testing.T) {
	f := namedFields([]ErrorEvent{{Message: `Cannot invoke "com.demo.CustomerRepository.findByEmail(String)" because "this.customers" is null`}})
	if !f["customers"] || len(f) != 1 {
		t.Fatalf("fields: %v", f)
	}
}

// Spring Boot 3 logs: the stack trace follows the ERROR line, frames are module-qualified, and the
// servlet's ERROR line repeats the exception text.
func TestParseSpringBootJava21Logs(t *testing.T) {
	log := `2026-10-30T23:46:32.790+05:30 ERROR 58870 --- [order-app] [scheduling-1] o.s.s.s.TaskUtils$LoggingErrorHandler    : Unexpected error occurred in scheduled task

java.lang.NullPointerException: Cannot invoke "java.math.BigDecimal.multiply(java.math.BigDecimal)" because "unit" is null
	at com.demo.orders.order.PricingService.price(PricingService.java:27) ~[!/:0.0.1-SNAPSHOT]
	at com.demo.orders.order.PricingJob.refresh(PricingJob.java:21) ~[!/:0.0.1-SNAPSHOT]
	at java.base/jdk.internal.reflect.DirectMethodHandleAccessor.invoke(DirectMethodHandleAccessor.java:103) ~[na:na]
	at org.springframework.scheduling.support.ScheduledMethodRunnable.runInternal(ScheduledMethodRunnable.java:130) ~[spring-context-6.2.19.jar!/:6.2.19]
	at java.base/java.lang.Thread.run(Thread.java:1583) ~[na:na]

2026-10-30T23:46:49.742+05:30 ERROR 58870 --- [order-app] [http-nio-8081-exec-2] o.a.c.c.C.[.[.[/].[dispatcherServlet]    : Servlet.service() for servlet [dispatcherServlet] in context with path [] threw exception [Request processing failed: java.lang.NullPointerException: Cannot invoke "com.demo.orders.quote.QuoteData.price()" because the return value of "com.demo.orders.quote.QuoteResponse.data()" is null] with root cause

java.lang.NullPointerException: Cannot invoke "com.demo.orders.quote.QuoteData.price()" because the return value of "com.demo.orders.quote.QuoteResponse.data()" is null
	at com.demo.orders.order.PricingService.price(PricingService.java:26) ~[!/:0.0.1-SNAPSHOT]
	at com.demo.orders.order.OrderController.price(OrderController.java:28) ~[!/:0.0.1-SNAPSHOT]
	at java.base/jdk.internal.reflect.DirectMethodHandleAccessor.invoke(DirectMethodHandleAccessor.java:103) ~[na:na]
	at app//org.apache.tomcat.util.threads.TaskThread$WrappingRunnable.run(TaskThread.java:61) ~[tomcat-embed-core-10.1.jar!/:na]
`
	ev := Parse(log)
	if len(ev) != 2 {
		t.Fatalf("want 2 events, got %d: %+v", len(ev), ev)
	}
	for _, e := range ev {
		if e.Exception != "java.lang.NullPointerException" || len(e.Frames) != 5 && len(e.Frames) != 4 || e.Frames[0].Class != "com.demo.orders.order.PricingService" {
			t.Fatalf("event not parsed whole: %+v", e)
		}
	}
	if ev[0].Logger == "" || ev[0].FirstTime == "" {
		t.Fatalf("the ERROR line's logger and time belong to the event: %+v", ev[0])
	}
	if ev[1].Frames[3].Class != "org.apache.tomcat.util.threads.TaskThread$WrappingRunnable" {
		t.Fatalf("app// prefix not stripped: %+v", ev[1].Frames[3])
	}
}

func TestNullPatternSpansDifferentValues(t *testing.T) {
	at := []Frame{{Class: "com.demo.orders.order.PricingService", Method: "price", Line: 26}}
	ev := []ErrorEvent{
		{Exception: "java.lang.NullPointerException", Message: `Cannot invoke "com.demo.orders.quote.QuoteData.price()" because the return value of "com.demo.orders.quote.QuoteResponse.data()" is null`, Frames: at, Count: 7},
		{Exception: "java.lang.NullPointerException", Message: `Cannot invoke "String.trim()" because the return value of "com.demo.orders.quote.QuoteResponse.message()" is null`, Frames: at, Count: 1},
		{Exception: "java.lang.NullPointerException", Message: `Cannot invoke "java.math.BigDecimal.multiply(java.math.BigDecimal)" because "unit" is null`, Frames: at, Count: 1},
	}
	p := NullPattern(ev)
	if !strings.Contains(p, "9 NullPointerExceptions in PricingService.price on 3 different null values") || !strings.Contains(p, "QuoteResponse.data() (×7)") || !strings.Contains(p, "local variable unit") {
		t.Fatalf("pattern: %q", p)
	}
	if NullPattern(ev[:1]) != "" {
		t.Fatal("one null value is not a pattern")
	}
}
