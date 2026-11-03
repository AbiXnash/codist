package watch

import "testing"

func TestBigPullGivesTheModelTheNewestCommits(t *testing.T) {
	n := maxCatchUp + 5
	for i := 0; i < n; i++ {
		if want := i >= 5; modelFor(i, n) != want {
			t.Errorf("commit %d of %d: model %v", i, n, !want)
		}
	}
	if !modelFor(0, 3) {
		t.Error("a small pull gets the model on every commit")
	}
}
