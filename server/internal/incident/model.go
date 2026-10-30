package incident

import "codist/internal/gitx"

// Location is a place in the project's code that appears in the errors.
type Location struct {
	Path      string         `json:"path"`
	Line      int            `json:"line"`
	Class     string         `json:"class"`
	Method    string         `json:"method"`
	NodeID    string         `json:"nodeId"`
	Snippet   string         `json:"snippet"`
	Origin    bool           `json:"origin"` // the top project frame of an error: where it was thrown
	Hits      int            `json:"hits"`   // how many occurrences pass through here
	Blame     gitx.BlameLine `json:"blame"`
	Issues    []string       `json:"issues,omitempty"` // open Codist issues near this line
	MethodEnd int            `json:"-"`
	MethodBeg int            `json:"-"`
}

// Suspect is a commit that may have caused the incident.
type Suspect struct {
	SHA     string   `json:"sha"`
	Author  string   `json:"author"`
	Time    int64    `json:"time"`
	Message string   `json:"message"`
	Score   float64  `json:"score"`
	Reasons []string `json:"reasons"`
}

type Evidence struct {
	Kind string `json:"kind"` // log, code, commit, issue
	Text string `json:"text"`
	Path string `json:"path,omitempty"`
	Line int    `json:"line,omitempty"`
	SHA  string `json:"sha,omitempty"`
}

// Cause is one explanation of the incident, with the evidence that supports it.
type Cause struct {
	Title       string     `json:"title"`
	Explanation string     `json:"explanation"`
	Confidence  string     `json:"confidence"` // high, medium, low
	Evidence    []Evidence `json:"evidence"`
	Fix         string     `json:"fix"`
	Path        string     `json:"path,omitempty"`
	Line        int        `json:"line,omitempty"`
	SHA         string     `json:"sha,omitempty"`
}

// Report is the RCA write-up, editable by the engineer.
type Report struct {
	Summary    string `json:"summary"`
	Timeline   string `json:"timeline"`
	Impact     string `json:"impact"`
	RootCause  string `json:"rootCause"`
	Fix        string `json:"fix"`
	Prevention string `json:"prevention"`
}

type Similar struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Score     float64 `json:"score"`
	RootCause string  `json:"rootCause"`
	CreatedAt int64   `json:"createdAt"`
}

type Incident struct {
	ID        string       `json:"id"`
	Title     string       `json:"title"`
	CreatedAt int64        `json:"createdAt"`
	Status    string       `json:"status"` // analysing, done, failed, confirmed
	Error     string       `json:"error,omitempty"`
	LogBytes  int          `json:"logBytes"`
	Excerpt   string       `json:"excerpt"` // the log lines around the errors, kept for the record
	Errors    []ErrorEvent `json:"errors"`
	Locations []Location   `json:"locations"`
	Suspects  []Suspect    `json:"suspects"`
	Similar   []Similar    `json:"similar"`
	Causes    []Cause      `json:"causes"`
	Timeline  []string     `json:"timeline"`
	Report    Report       `json:"report"`
	Confirmed int          `json:"confirmed"` // index of the confirmed cause, -1 when none
	Runbook   string       `json:"runbook,omitempty"`
	Model     string       `json:"model"`
	Tokens    []string     `json:"tokens"`
}
