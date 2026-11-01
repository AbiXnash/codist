package javaidx

import (
	"strings"
	"testing"
)

func titles(src string, ctx Context) string {
	changed := map[int]bool{}
	for i := range strings.Split(src, "\n") {
		changed[i+1] = true
	}
	var out []string
	for _, f := range Check([]byte(src), changed, ctx) {
		out = append(out, f.Title)
	}
	return strings.Join(out, "\n")
}

func TestReferenceEquality(t *testing.T) {
	ctx := Context{Returns: map[string]string{"getId": "Long", "getBookId": "Long", "count": "int"}}
	found := titles(`package a;
class A {
    boolean plan(Member m) { return m.getPlan() == "PREMIUM"; }
    boolean same(Loan l, Book b) { return l.getBookId() == b.getId(); }
}`, ctx)
	for _, want := range []string{"String compared with ==", "Long compared with =="} {
		if !strings.Contains(found, want) {
			t.Errorf("missing %q in:\n%s", want, found)
		}
	}
	clean := titles(`package a;
class A {
    boolean n(String s) { return s == null; }
    boolean c(X x, Y y) { return x.count() == y.count(); }
    boolean i(int a, int b) { return a == b; }
}`, ctx)
	if strings.Contains(clean, "compared with") {
		t.Errorf("primitives and null are fine:\n%s", clean)
	}
}

func TestHardcodedSecret(t *testing.T) {
	found := titles(`package a;
class A {
    private static final String MAIL_API_KEY = "sk_live_51Hd8fQ2eZvKYlo2C9x";
    private String dbPassword = "hunter2secret";
    private String weird = "AKIAIOSFODNN7EXAMPLE";
}`, Context{})
	for _, want := range []string{"MAIL_API_KEY", "dbPassword", "weird"} {
		if !strings.Contains(found, "Hard-coded secret: "+want) {
			t.Errorf("missing %s in:\n%s", want, found)
		}
	}
	clean := titles(`package a;
class A {
    private static final String API_KEY_HEADER = "X-Api-Key";
    private static final String PASSWORD_PROP = "app.password";
    private String password = "${DB_PASSWORD}";
    private String secretMessage = "keep this between us";
    private String token = "";
}`, Context{})
	if strings.Contains(clean, "Hard-coded secret") {
		t.Errorf("names and placeholders are not secrets:\n%s", clean)
	}
}

func TestDataOnly(t *testing.T) {
	data := []string{
		`package a; public record R(String a, int b) {}`,
		`package a; public interface Repo extends JpaRepository<B, Long> { Optional<B> findByIsbn(String i); }`,
		`package a; class E { private Long id; protected E() {} public E(Long id) { this.id = id; } public Long getId() { return id; } public void setId(Long i) { this.id = i; } }`,
	}
	for _, src := range data {
		if !DataOnly([]byte(src)) {
			t.Errorf("should be data only: %s", src)
		}
	}
	logic := []string{
		`package a; class S { int f(int x) { if (x > 1) { return 1; } return 0; } }`,
		`package a; class C { Object f() { return service.all(); } }`,
		`package a; interface R { @Query("select x") List<X> f(); }`,
		`package a; class E { boolean equals2(E o) { return id.equals(o.id); } }`,
	}
	for _, src := range logic {
		if DataOnly([]byte(src)) {
			t.Errorf("has behaviour: %s", src)
		}
	}
}
