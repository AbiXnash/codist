// Package review maps commits onto the graph and reviews them with rules and the local LLM.
package review

import (
	"strings"

	"codist/internal/gitx"
)

const (
	StatusQueued  = "queued"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"
)

// Analysis is what a commit touched, mapped onto types.
// Impacted types depend on a changed type but were not changed themselves.
type Analysis struct {
	Commit   gitx.Commit       `json:"commit"`
	Files    []gitx.FileChange `json:"files"`
	Changed  []ChangedType     `json:"changed"`
	Impacted []string          `json:"impacted"`
}

type ChangedType struct {
	ID      string   `json:"id"`
	File    string   `json:"file"`
	Status  string   `json:"status"` // added, modified, deleted
	Members []string `json:"members"`
}

type Review struct {
	SHA        string       `json:"sha"`
	Status     string       `json:"status"`
	Model      string       `json:"model"`
	StartedAt  int64        `json:"startedAt"`
	FinishedAt int64        `json:"finishedAt"`
	Summary    string       `json:"summary"`
	Risk       string       `json:"risk"`
	Highlights []string     `json:"highlights"`
	Files      []FileReview `json:"files"`
	Error      string       `json:"error,omitempty"`
	// Commit message review: what is wrong with it, and a message that describes the change.
	MessageProblem   string `json:"messageProblem,omitempty"`
	SuggestedMessage string `json:"suggestedMessage,omitempty"`
}

type FileReview struct {
	Path     string    `json:"path"`
	Summary  string    `json:"summary"`
	Findings []Finding `json:"findings"`
	Error    string    `json:"error,omitempty"`
	Pending  bool      `json:"pending,omitempty"` // rule findings are in, the model is still working
	Skipped  string    `json:"skipped,omitempty"` // why the model was not asked (trivial change, cached…)
}

// Finding Source is "rule" for deterministic checks and "llm" for the model.
type Finding struct {
	Line          int    `json:"line"`
	EndLine       int    `json:"endLine"`
	Severity      string `json:"severity"`
	Category      string `json:"category"`
	Title         string `json:"title"`
	Detail        string `json:"detail"`
	Suggestion    string `json:"suggestion"`
	NodeID        string `json:"nodeId"`
	OnChangedLine bool   `json:"onChangedLine"`
	Source        string `json:"source"`
	Ref           string `json:"ref,omitempty"`     // knowledge-base guideline the finding matches
	Snippet       string `json:"snippet,omitempty"` // the flagged line as written, used to match ignores across commits
	Ignored       bool   `json:"ignored,omitempty"` // set when read, never stored
	IgnoreID      string `json:"ignoreId,omitempty"`
	Group         string `json:"group,omitempty"`     // findings with the same root cause share a group (set when read)
	GroupRoot     bool   `json:"groupRoot,omitempty"` // the finding that represents its group
	// Tracking across commits (set when read): the commit that first introduced it, and whether
	// the code is still there (open), gone (resolved) or ignored.
	FirstSeen   string `json:"firstSeen,omitempty"`
	State       string `json:"state,omitempty"`
	FixedIn     string `json:"fixedIn,omitempty"`
	FixedBy     string `json:"fixedBy,omitempty"`     // author of the fixing commit
	FixedAt     int64  `json:"fixedAt,omitempty"`     // its time (unix millis)
	FixedMsg    string `json:"fixedMsg,omitempty"`    // its subject
	Uncommitted bool   `json:"uncommitted,omitempty"` // fixed in the working tree, not committed yet
}

// Key identifies the kind of finding for ignores: its rule id, or its title for model findings.
func (f Finding) Key() string {
	if f.Ref != "" {
		return f.Ref
	}
	return "title:" + strings.ToLower(strings.TrimSpace(f.Title))
}

// Counts are issue totals per severity, for badges: each group counts once, ignored findings
// and findings carried over from earlier commits not at all.
func (r Review) Counts() map[string]int {
	out := map[string]int{}
	for _, f := range r.Files {
		for _, x := range f.Findings {
			if x.Ignored || (x.Group != "" && !x.GroupRoot) || (x.FirstSeen != "" && x.FirstSeen != r.SHA) {
				continue
			}
			out[x.Severity]++
		}
	}
	return out
}
