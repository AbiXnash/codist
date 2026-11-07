package app

import (
	"testing"
	"time"

	"github.com/AbiXnash/codist-git"
	"github.com/AbiXnash/codist-git/testrepo"
	"github.com/AbiXnash/codist-review/store"
	"github.com/AbiXnash/codist/internal/config"
)

// A restart picks up where Codist stopped: reviews cut off mid-way are done again, finished ones
// are not, and the model only reads the commits it has not read.
func TestBacklogAfterRestart(t *testing.T) {
	tr := testrepo.New(t)
	repo, _ := git.Open(tr.Dir)
	st, err := store.Open(t.TempDir(), "shop")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now()
	st.SetReview(store.Review{SHA: tr.Fix, Status: "running", Model: "fake", Started: now}) // stopped while the model read it
	st.SetReview(store.Review{SHA: tr.Bug, Status: "done", Started: now})                   // checked, not read by the model
	st.SetReview(store.Review{SHA: tr.Init, Status: "done", Model: "fake", Started: now})   // finished
	a := &App{cfg: config.Defaults(tr.Dir), repo: repo, store: st, checks: newQueue(), models: newQueue()}
	a.backlog()
	if a.checks.length() != 1 || !a.checks.waiting(tr.Fix) {
		t.Errorf("checks: %d queued", a.checks.length())
	}
	if a.models.length() != 2 || !a.models.waiting(tr.Fix) || !a.models.waiting(tr.Bug) || a.models.waiting(tr.Init) {
		t.Errorf("model: %d queued", a.models.length())
	}
}
