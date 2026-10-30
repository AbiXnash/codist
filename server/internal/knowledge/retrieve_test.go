package knowledge

import (
	"strings"
	"testing"
)

// Each snippet must retrieve the entry that explains its bug within the top results.
var retrievalCases = []struct {
	name, code, want string
}{
	{"uninjected", "@Service\npublic class CustomerService {\n    private CustomerRepository customers;\n    public Customer get(Long id) { return customers.findById(id).orElseThrow(); }\n}", "dependency-not-injected"},
	{"bigdecimal", "BigDecimal price = new BigDecimal(19.99);\nif (total.equals(expected)) {}", "bigdecimal-precision"},
	{"n+1", "for (Order o : orderRepository.findAll()) {\n    total += o.getLines().size();\n}", "n-plus-one"},
	{"self-invocation", "public void importAll(List<Row> rows) { rows.forEach(r -> this.importOne(r)); }\n@Transactional\nprivate void importOne(Row r) { repo.save(r); }", "transactional-proxy-bypass"},
	{"sql", "String q = \"select u from User u where u.name = '\" + name + \"'\";\nem.createQuery(q).getResultList();", "sql-injection"},
	{"optional", "Order o = orderRepository.findById(id).get();", "optional-get-unchecked"},
	{"swallow", "try { gateway.charge(order); } catch (Exception e) { }", "swallowed-exception"},
	{"singleton state", "@Service\npublic class ReportService {\n    private List<Line> lines = new ArrayList<>();\n    public Report build(Query q) { lines.clear(); lines.addAll(q.run()); return new Report(lines); }\n}", "mutable-singleton-state"},
	{"remote in loop", "for (Txn t : txns) {\n    PaymentStatus s = restTemplate.getForObject(url + t.getId(), PaymentStatus.class);\n    log.info(\"status {}\", s);\n}", "remote-call-in-loop"},
	{"interrupted", "try { Thread.sleep(1000); } catch (InterruptedException e) { log.warn(\"interrupted\"); }", "interrupted-swallowed"},
	{"string ==", "if (order.getStatus() == \"PAID\") { ship(order); }", "reference-equality"},
	{"secret", "private static final String API_KEY = \"sk-live-123\";\nprivate String password = \"admin123\";", "hardcoded-secret"},
}

func TestRetrievalFindsTheRightKnowledge(t *testing.T) {
	kb, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(kb.Entries) < 80 {
		t.Fatalf("bugbase has %d entries", len(kb.Entries))
	}
	miss := 0
	for _, c := range retrievalCases {
		got := kb.Retrieve("src/main/java/x/Foo.java", c.code, c.code, 6)
		var ids []string
		found := false
		for _, e := range got {
			ids = append(ids, e.ID)
			if e.ID == c.want {
				found = true
			}
		}
		if !found {
			miss++
			t.Errorf("%s: want %s in %s", c.name, c.want, strings.Join(ids, ", "))
		}
	}
	t.Logf("retrieval: %d/%d cases hit", len(retrievalCases)-miss, len(retrievalCases))
}

func TestBugbaseEntriesAreWellFormed(t *testing.T) {
	kb, _ := Load()
	ids := map[string]bool{}
	for _, e := range kb.Entries {
		if e.ID == "" || e.Title == "" || len(e.Body) < 80 || e.Category == "" {
			t.Errorf("incomplete entry %+v", e)
		}
		if strings.HasPrefix(strings.ToLower(e.Body), "check ") || strings.HasPrefix(strings.ToLower(e.Body), "look for") {
			t.Errorf("%s reads like an instruction; describe the bug instead", e.ID)
		}
		if ids[e.ID] {
			t.Errorf("duplicate id %s", e.ID)
		}
		ids[e.ID] = true
	}
}
