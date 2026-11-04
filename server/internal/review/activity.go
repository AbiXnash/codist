package review

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// Activity is one thing Codist did: a model call with exactly what went in and came out, or a step
// without the model (rule checks, analysers, knowledge retrieval). Together they are the stream shown
// on the Activity screen, so nothing the AI is told or answers is hidden.
type Activity struct {
	ID        string   `json:"id"`
	At        int64    `json:"at"`
	Kind      string   `json:"kind"` // review-file, review-commit, fix, fix-check, edit, ask, test, rca, rule, runbook, step
	Title     string   `json:"title"`
	Status    string   `json:"status"` // running, done, failed, cancelled
	Model     string   `json:"model,omitempty"`
	SHA       string   `json:"sha,omitempty"`
	Path      string   `json:"path,omitempty"`
	Incident  string   `json:"incident,omitempty"`
	Knowledge []string `json:"knowledge,omitempty"` // bugbase entries retrieved for this prompt
	Note      string   `json:"note,omitempty"`      // what came of it, e.g. "3 findings"
	Error     string   `json:"error,omitempty"`
	TookMs    int64    `json:"tookMs"`
	PromptTok int      `json:"promptTokens,omitempty"`
	ReplyTok  int      `json:"replyTokens,omitempty"`
	System    string   `json:"system,omitempty"`
	User      string   `json:"user,omitempty"`
	Response  string   `json:"response,omitempty"`
}

// Label says what a model call is for; see Ollama.For.
type Label struct {
	Kind, Title, SHA, Path, Incident string
	Knowledge                        []string
}

// Recorder keeps the latest activity in memory and appends it to <data-dir>/<repo>/activity.jsonl.
type Recorder struct {
	path    string
	publish func(a Activity)
	mu      sync.Mutex
	items   []*Activity
	seq     int64
}

const activityKept = 500

// Activities is the recorder for this process; nil records nothing.
var Activities *Recorder

func NewRecorder(dataDir, repo string, publish func(a Activity)) *Recorder {
	r := &Recorder{path: filepath.Join(dataDir, repo, "activity.jsonl"), publish: publish, seq: time.Now().UnixMilli()}
	f, err := os.Open(r.path)
	if err != nil {
		return r
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var a Activity
		if json.Unmarshal(sc.Bytes(), &a) == nil {
			if a.Status == "running" {
				a.Status, a.Error = "failed", "Interrupted: Codist stopped first"
			}
			r.items = append(r.items, &a)
		}
	}
	if len(r.items) > activityKept {
		r.items = r.items[len(r.items)-activityKept:]
	}
	return r
}

func (r *Recorder) begin(a Activity) *Activity {
	if r == nil {
		return &a
	}
	r.mu.Lock()
	r.seq++
	a.ID, a.At = fmt.Sprintf("%x", r.seq), time.Now().UnixMilli()
	if a.Status == "" {
		a.Status = "running"
	}
	r.items = append(r.items, &a)
	if len(r.items) > activityKept {
		r.items = r.items[len(r.items)-activityKept:]
	}
	r.mu.Unlock()
	r.announce(a)
	return &a
}

func (r *Recorder) finish(a *Activity, response, errText string, promptTok, replyTok int) {
	if r == nil {
		return
	}
	r.mu.Lock()
	a.Response, a.Error, a.PromptTok, a.ReplyTok = response, errText, promptTok, replyTok
	a.TookMs = time.Now().UnixMilli() - a.At
	switch {
	case strings.Contains(errText, "context canceled"):
		a.Status = "cancelled"
	case errText != "":
		a.Status = "failed"
	default:
		a.Status = "done"
	}
	cp := *a
	r.mu.Unlock()
	r.persist(cp)
	r.announce(cp)
}

// Step records something done without the model.
func (r *Recorder) Step(title, sha, path, note string, took time.Duration) {
	if r == nil {
		return
	}
	a := r.begin(Activity{Kind: "step", Title: title, SHA: sha, Path: path, Note: note, Status: "done", TookMs: took.Milliseconds()})
	r.persist(*a)
}

func (r *Recorder) persist(a Activity) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_ = os.MkdirAll(filepath.Dir(r.path), 0o755)
	f, err := os.OpenFile(r.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(a)
	_, _ = f.Write(append(b, 0x0a))
}

func (r *Recorder) announce(a Activity) {
	if r.publish != nil {
		a.System, a.User, a.Response = "", "", ""
		r.publish(a)
	}
}

// List returns the latest activity, newest first, without the prompt and response bodies.
func (r *Recorder) List(limit int) []Activity {
	out := []Activity{}
	if r == nil {
		return out
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.items) - 1; i >= 0 && len(out) < limit; i-- {
		a := *r.items[i]
		a.System, a.User, a.Response = "", "", ""
		out = append(out, a)
	}
	return out
}

// TypicalFileMs is how long this model has recently taken to review one file on this machine: the
// median of its last 8 reviews, or 0 with fewer than 3 to go on. On saved runs of 14b it came within
// 25% of the next file's time 70% of the time, so the page shows it only as a rough estimate.
func (r *Recorder) TypicalFileMs(model string) int64 {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var took []int64
	for i := len(r.items) - 1; i >= 0 && len(took) < 8; i-- {
		if a := r.items[i]; a.Kind == "review-file" && a.Status == "done" && a.Model == model {
			took = append(took, a.TookMs)
		}
	}
	if len(took) < 3 {
		return 0
	}
	slices.Sort(took)
	return took[len(took)/2]
}

// Get returns one activity with everything that was sent and received.
func (r *Recorder) Get(id string) (Activity, bool) {
	if r == nil {
		return Activity{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.items) - 1; i >= 0; i-- {
		if r.items[i].ID == id {
			return *r.items[i], true
		}
	}
	return Activity{}, false
}
