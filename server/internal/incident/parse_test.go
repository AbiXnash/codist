package incident

import "testing"

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
