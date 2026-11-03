package javaidx

import "testing"

func callLinks(t *testing.T, sources map[string]string) map[string]bool {
	t.Helper()
	files := map[string][]byte{}
	for path, src := range sources {
		files[path] = []byte(src)
	}
	out := map[string]bool{}
	for _, c := range BuildCallGraph(files, &Graph{}).Calls() {
		out[c.From.Class+"."+c.From.Name+">"+c.To.Class+"."+c.To.Name] = c.Confirmed
	}
	return out
}

func TestCallsLinkMethodsWithTheirCertainty(t *testing.T) {
	got := callLinks(t, map[string]string{
		"src/main/java/a/Controller.java": `package a;
class Controller {
  private Service service;
  void create() { service.save(); helper(); }
  void helper() { unknown().ping(); }
  void loop() { loop(); }
}`,
		"src/main/java/a/Service.java": `package a;
interface Service { void save(); }`,
		"src/main/java/a/ServiceImpl.java": `package a;
class ServiceImpl implements Service {
  public void save() {}
  void ping() {}
}`,
	})
	confirmed := []string{"Controller.create>Service.save", "Controller.create>ServiceImpl.save", "Controller.create>Controller.helper"}
	for _, link := range confirmed {
		if sure, ok := got[link]; !ok || !sure {
			t.Errorf("%s: found %v, confirmed %v, in %v", link, ok, sure, got)
		}
	}
	if sure, ok := got["Controller.helper>ServiceImpl.ping"]; !ok || sure {
		t.Errorf("a call on an unknown receiver should be possible only: found %v, confirmed %v, in %v", ok, sure, got)
	}
	if _, ok := got["Controller.loop>Controller.loop"]; ok {
		t.Error("a method calling itself is not a link between two methods")
	}
}

func TestCallsAreInAStableOrder(t *testing.T) {
	files := map[string][]byte{"src/main/java/a/A.java": []byte(`package a;
class A { void x() { y(); z(); } void y() { z(); } void z() {} }`)}
	first := BuildCallGraph(files, &Graph{}).Calls()
	for i := 0; i < 5; i++ {
		again := BuildCallGraph(files, &Graph{}).Calls()
		for j := range first {
			if first[j].From.Name != again[j].From.Name || first[j].To.Name != again[j].To.Name {
				t.Fatalf("order changed at %d", j)
			}
		}
	}
}
