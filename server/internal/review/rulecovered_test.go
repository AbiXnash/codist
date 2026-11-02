package review

import "testing"

func TestInjectionFindingsAreNotMistakenForUninjectedDependencies(t *testing.T) {
	for _, title := range []string{"SQL injection in findByName", "Command injection risk", "Log injection"} {
		if got := ruleCovered("", title); got == "uninjected-dependency" {
			t.Errorf("%q was treated as %s", title, got)
		}
	}
	if got := ruleCovered("", "Uninjected dependency on Mailer"); got != "uninjected-dependency" {
		t.Errorf("got %q", got)
	}
}
