package app

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"codist/internal/config"
	"codist/internal/git"
	"codist/internal/testrepo"
)

// fakeModel is a model server: it has one model, "fake", and answers reviews and incident
// questions with canned replies. fail makes every chat answer 500.
type fakeModel struct {
	mu     sync.Mutex
	fail   bool
	review string
	cause  string
	chats  int
}

func (f *fakeModel) start(t *testing.T) string {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			io.WriteString(w, `{"models":[{"name":"fake"}]}`)
			return
		}
		var req struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.chats++
		if f.fail {
			http.Error(w, "out of memory", 500)
			return
		}
		reply := f.review
		if strings.Contains(req.Messages[0].Content, "production error") {
			reply = f.cause
		}
		b, _ := json.Marshal(map[string]any{"message": map[string]string{"content": reply}})
		w.Write(b)
	}))
	t.Cleanup(s.Close)
	return s.URL
}

type env struct {
	t     *testing.T
	repo  *testrepo.Repo
	app   *App
	url   string
	model *fakeModel
}

func start(t *testing.T, change func(*config.Config)) *env {
	t.Helper()
	tr := testrepo.New(t)
	fm := &fakeModel{review: `{"findings":[]}`, cause: `{"summary":"S","impact":"I","rootCause":"R","causes":[
		{"title":"Discount failed","explanation":"E","confidence":"high","fix":"F",
		 "evidence":["java.lang.IllegalStateException: discount failed","public void applyDiscount() { total = total * 9 / 10; }","not in the logs"]},
		{"title":"Guess","explanation":"G","confidence":"high","fix":"F2","evidence":["made up"]}]}`}
	cfg := config.Defaults(tr.Dir)
	cfg.DataDir, cfg.ModelURL, cfg.Model, cfg.PollMs = t.TempDir(), fm.start(t), "fake", 50
	if change != nil {
		change(&cfg)
	}
	repo, err := git.Open(tr.Dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	a, err := New(ctx, cfg, repo)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(func() {
		srv.Close()
		cancel()
		time.Sleep(50 * time.Millisecond) // let the lanes see the cancel before the store closes
		a.Close()
	})
	return &env{t: t, repo: tr, app: a, url: srv.URL, model: fm}
}

// call sends a request and decodes a JSON answer into out, returning the status code.
func (e *env) call(method, path, body string, out any) int {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.url+path, strings.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			e.t.Fatalf("%s %s: %v in %.300s", method, path, err, raw)
		}
	}
	return resp.StatusCode
}

func (e *env) get(path string, out any) int { return e.call(http.MethodGet, path, "", out) }

// waitFor polls until ok says yes, failing after 30 seconds.
func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(30 * time.Second); time.Now().Before(end); time.Sleep(40 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

type commitItem struct {
	Commit struct{ SHA, Message string }
	Status string
	Model  string
	Counts map[string]int
	Fixed  int
}

func (e *env) commits() []commitItem {
	var cs []commitItem
	e.get("/api/commits", &cs)
	return cs
}

// reviewed waits until every commit on the branch is done, by the model when withModel.
func (e *env) reviewed(n int, withModel bool) []commitItem {
	var cs []commitItem
	waitFor(e.t, "reviews", func() bool {
		cs = e.commits()
		if len(cs) != n {
			return false
		}
		for _, c := range cs {
			if c.Status != "done" || withModel && c.Model != "fake" {
				return false
			}
		}
		return true
	})
	return cs
}

const incidentLogs = `2026-11-06 10:00:01 ERROR c.s.o.OrderController - request failed
java.lang.IllegalStateException: discount failed
	at com.shop.order.OrderService.discount(OrderService.java:31)
	at com.shop.order.OrderController.get(OrderController.java:16)
	at org.springframework.web.servlet.FrameworkServlet.service(FrameworkServlet.java:897)
Caused by: java.lang.ArithmeticException: / by zero
	at com.shop.order.Order.applyDiscount(Order.java:9)
`

func TestReviewLifecycle(t *testing.T) {
	e := start(t, nil)
	cs := e.reviewed(3, true)
	if cs[0].Commit.SHA != e.repo.Fix || cs[0].Fixed != 1 || cs[1].Counts["medium"] != 1 {
		t.Errorf("commits: %+v", cs)
	}
	var limited []commitItem
	if e.get("/api/commits?limit=1", &limited); len(limited) != 1 {
		t.Errorf("limit: %d", len(limited))
	}

	// The bug commit: what changed, what it affects, and its finding, fixed by the next commit.
	var bug struct {
		Analysis struct {
			Changed []struct {
				ID      string
				Members []string
			}
			Impacted []string
			Files    []struct {
				ChangeType string
				OldPath    string
				DeletedAt  []int
			}
		}
		Review struct {
			Status string
			Model  string
			Files  []struct {
				Path     string
				Findings []map[string]any
			}
		}
	}
	if e.get("/api/commits/"+e.repo.Bug, &bug) != 200 {
		t.Fatal("bug commit")
	}
	if len(bug.Analysis.Changed) != 1 || bug.Analysis.Changed[0].ID != "com.shop.order.OrderService" ||
		strings.Join(bug.Analysis.Changed[0].Members, ",") != "discount" {
		t.Errorf("changed: %+v", bug.Analysis.Changed)
	}
	if strings.Join(bug.Analysis.Impacted, ",") != "com.shop.order.OrderController" {
		t.Errorf("impacted: %v", bug.Analysis.Impacted)
	}
	if bug.Analysis.Files[0].ChangeType != "MODIFY" || bug.Analysis.Files[0].OldPath != testrepo.ServicePath || bug.Analysis.Files[0].DeletedAt == nil {
		t.Errorf("files: %+v", bug.Analysis.Files)
	}
	f := bug.Review.Files[0].Findings
	if bug.Review.Status != "done" || bug.Review.Model != "fake" || len(f) != 1 || f[0]["ref"] != "empty-catch" ||
		f[0]["fixedIn"] != e.repo.Fix || f[0]["fixedBy"] != "Dev" || f[0]["line"] != float64(testrepo.BugLine) {
		t.Errorf("review: %+v", bug.Review)
	}
	var fix struct{ Resolved []map[string]any }
	if e.get("/api/commits/"+e.repo.Fix, &fix); len(fix.Resolved) != 1 || fix.Resolved[0]["fixedIn"] != e.repo.Fix {
		t.Errorf("the fix resolves the issue: %+v", fix.Resolved)
	}
	var init struct {
		Analysis struct{ Files []map[string]any }
	}
	if e.get("/api/commits/"+e.repo.Init, &init); len(init.Analysis.Files) != len(testrepo.Files) || init.Analysis.Files[0]["oldPath"] != nil {
		t.Errorf("first commit adds every file: %d", len(init.Analysis.Files))
	}
	if e.get("/api/commits/nope", nil) != 404 || e.call("POST", "/api/commits/nope/review", "", nil) != 404 {
		t.Error("unknown commit")
	}
	var impact map[string]any
	if e.get("/api/commits/"+e.repo.Bug+"/impact", &impact); impact["sha"] != e.repo.Bug {
		t.Errorf("impact %v", impact)
	}

	// Issues: the fixed one, and the slow call the first-run check found in the old code.
	var issues struct {
		Issues []map[string]any
		Counts map[string]int
	}
	e.get("/api/issues", &issues)
	if issues.Counts["resolved"] != 1 || issues.Counts["open"] != 1 {
		t.Fatalf("counts %v", issues.Counts)
	}
	var open map[string]any
	for _, is := range issues.Issues {
		if is["state"] == "open" {
			open = is
		}
	}
	if open["ref"] != "slow-call-in-loop" || open["firstSeen"] != e.repo.Init || open["lastSeen"] != e.repo.Init ||
		open["category"] != "performance" || open["urgency"] != "Fix soon" || open["firstBy"] != "Dev" {
		t.Errorf("open issue %+v", open)
	}
	id := open["id"].(string)
	if e.call("POST", "/api/issues/"+id+"/resolve", "", nil) != 200 {
		t.Error("resolve")
	}
	e.get("/api/issues?state=resolved", &issues)
	if len(issues.Issues) != 2 || (issues.Issues[0]["manual"] != true && issues.Issues[1]["manual"] != true) {
		t.Errorf("resolved by hand: %+v", issues.Issues)
	}
	if e.call("DELETE", "/api/issues/"+id+"/resolve", "", nil) != 200 || e.call("POST", "/api/issues/nope/resolve", "", nil) != 404 {
		t.Error("reopen, or an unknown issue")
	}
	if e.get("/api/issues?state=open", &issues); len(issues.Issues) != 1 {
		t.Errorf("reopened: %d", len(issues.Issues))
	}

	// Asking for a review again puts it at the front of the model lane.
	var queued map[string]any
	if e.call("POST", "/api/commits/"+e.repo.Bug[:7]+"/review", "", &queued); queued["sha"] != e.repo.Bug || queued["queued"] != true {
		t.Errorf("queued %v", queued)
	}
	e.reviewed(3, true)

	// A new commit is found, checked and read by the model while Codist runs.
	e.repo.Commit("Log orders", map[string]string{"order-service/src/main/java/com/shop/order/Log.java": "package com.shop.order;\n\nclass Log {\n    void a() {\n        System.out.println(1);\n    }\n}\n"})
	cs = e.reviewed(4, true)
	if cs[0].Commit.Message != "Log orders" || cs[0].Counts["low"] != 1 {
		t.Errorf("new commit %+v", cs[0])
	}
	waitFor(t, "the graph to see the new class", func() bool {
		var g struct{ Nodes []any }
		e.get("/api/graph", &g)
		return len(g.Nodes) == 9
	})
}

func TestGraphRepoAndFiles(t *testing.T) {
	e := start(t, nil)
	var g struct {
		Nodes []struct {
			ID, Stereotype, FeignTarget, Kind string
			Members                           []struct{ Name, Kind string }
			Endpoints                         []string
		}
		Edges  []graphEdge
		Source string
	}
	e.get("/api/graph", &g)
	if len(g.Nodes) != 8 || g.Source != e.repo.Fix {
		t.Fatalf("%d nodes from %s", len(g.Nodes), g.Source)
	}
	kinds := map[string]string{}
	for _, ed := range g.Edges {
		kinds[ed.Source+"->"+ed.Target] = ed.Kind
		if ed.Kind == "feign" && strings.Join(ed.Via, ",") != "POST /payments/{}" {
			t.Errorf("feign via %v", ed.Via)
		}
	}
	for link, kind := range map[string]string{
		"com.shop.order.PaymentClient->com.shop.payment.PaymentController": "feign",
		"com.shop.order.OrderController->com.shop.order.OrderService":      "injects",
		"com.shop.order.OrderService->com.shop.order.Order":                "uses",
	} {
		if kinds[link] != kind {
			t.Errorf("%s is %q, want %s", link, kinds[link], kind)
		}
	}
	for _, n := range g.Nodes {
		if n.ID == "com.shop.order.OrderService" {
			if n.Members[0].Kind != "field" || n.Members[2].Kind != "constructor" {
				t.Errorf("members in order: %+v", n.Members)
			}
		}
		if n.ID == "com.shop.order.PaymentClient" && (n.FeignTarget != "payment-service" || n.Kind != "interface") {
			t.Errorf("client %+v", n)
		}
		if n.ID == "com.shop.payment.PaymentController" && strings.Join(n.Endpoints, "") != "POST /payments/{id}  → charge()" {
			t.Errorf("endpoints %v", n.Endpoints)
		}
	}
	for i := 0; i < 2; i++ { // the second answer comes from the cache
		var calls []struct {
			From, To  struct{ Class, Name string }
			Confirmed bool
		}
		e.get("/api/graph/calls", &calls)
		found := false
		for _, c := range calls {
			found = found || c.From.Class == "PaymentClient" && c.To.Class == "PaymentController" && c.Confirmed
		}
		if !found {
			t.Errorf("no Feign call in %d calls", len(calls))
		}
	}

	var rep struct {
		Name, Branch     string
		Files, JavaFiles int
		Types, Commits   int
		Services         []map[string]any
		Endpoints        []map[string]any
		Calls            []map[string]any
		Costly           []map[string]any
		Hotspots         []map[string]any
		Contributors     []map[string]any
	}
	e.get("/api/repo", &rep)
	if rep.Branch != "main" || rep.JavaFiles != 8 || rep.Types != 8 || rep.Commits != 3 || len(rep.Services) != 2 {
		t.Errorf("repo %+v", rep)
	}
	if len(rep.Calls) != 1 || rep.Calls[0]["from"] != "order-service" || rep.Calls[0]["to"] != "payment-service" {
		t.Errorf("calls %+v", rep.Calls)
	}
	if len(rep.Endpoints) != 2 || len(rep.Costly) == 0 || rep.Costly[0]["method"] != "payAll" || len(rep.Hotspots) == 0 {
		t.Errorf("endpoints %v costly %v", rep.Endpoints, rep.Costly)
	}
	if rep.Contributors[0]["name"] != "Dev" || rep.Contributors[0]["commits"] != float64(3) {
		t.Errorf("contributors %v", rep.Contributors)
	}

	var file struct{ Content string }
	if e.get("/api/file?path="+testrepo.ServicePath, &file); file.Content != testrepo.ServiceFix {
		t.Error("file from disk")
	}
	if e.get("/api/file?path="+testrepo.ServicePath+"&sha="+e.repo.Bug, &file); file.Content != testrepo.ServiceBug {
		t.Error("file at a commit")
	}
	if e.get("/api/file?path=missing.java", nil) != 404 {
		t.Error("missing file")
	}
	var working map[string]any
	if e.get("/api/working", &working); working["findings"] != float64(0) {
		t.Errorf("working %v", working)
	}
}

func TestBranches(t *testing.T) {
	e := start(t, nil)
	var refs []map[string]any
	e.get("/api/refs", &refs)
	checked := map[string]bool{}
	for _, r := range refs {
		checked[r["name"].(string)] = r["checkedOut"].(bool)
	}
	if !checked["main"] || checked[testrepo.Branch] || len(refs) != 2 {
		t.Errorf("refs %v", refs)
	}
	if e.call("POST", "/api/refs/follow", `{"name":"`+testrepo.Branch+`"}`, nil) != 200 {
		t.Fatal("follow")
	}
	var st map[string]any
	if e.get("/api/status", &st); st["viewing"] != true || st["branch"] != testrepo.Branch || st["checkedOut"] != "main" || st["head"] != e.repo.Ext {
		t.Errorf("status while viewing %v", st)
	}
	var g struct{ Nodes []any }
	if e.get("/api/graph", &g); len(g.Nodes) != 9 {
		t.Errorf("the branch has AuditService: %d nodes", len(g.Nodes))
	}
	var file struct{ Content string }
	if e.get("/api/file?path=order-service/src/main/java/com/shop/order/AuditService.java", &file); !strings.Contains(file.Content, "AuditService") {
		t.Error("a file read from the viewed branch")
	}
	var cs []commitItem
	if e.get("/api/commits", &cs); len(cs) != 4 || cs[0].Commit.SHA != e.repo.Ext {
		t.Errorf("the branch's commits: %d", len(cs))
	}
	var rep map[string]any
	if e.get("/api/repo", &rep); rep["branch"] != testrepo.Branch {
		t.Errorf("repo branch %v", rep["branch"])
	}
	e.call("POST", "/api/refs/follow", `{"name":"main"}`, nil)
	if e.get("/api/status", &st); st["viewing"] != false || st["branch"] != "main" {
		t.Errorf("back on main: %v", st)
	}
	if e.call("POST", "/api/refs/follow", `{}`, nil) != 400 || e.call("POST", "/api/refs/follow", `{"name":"nope"}`, nil) != 404 {
		t.Error("bad follow")
	}
}

func TestIncidents(t *testing.T) {
	e := start(t, nil)
	e.reviewed(3, true) // so the model is free
	var made map[string]string
	if e.call("POST", "/api/incidents", `{"logs":`+quote(incidentLogs)+`}`, &made) != 200 {
		t.Fatal("create")
	}
	id := made["id"]
	type incident struct {
		Incident struct {
			Title, Status string
			Errors        []struct {
				Exception, Message string
				CausedBy           []string
			}
			Locations []struct {
				Path   string
				Line   int
				Origin bool
				Blame  struct{ SHA string }
			}
			Suspects []struct{ SHA string }
			Causes   []struct {
				Title, Confidence, Explanation string
				Evidence                       []struct{ Kind, Text string }
			}
			Report struct{ Summary, RootCause, Fix string }
			Model  string
		}
		Markdown string
	}
	var in incident
	waitFor(t, "the analysis", func() bool {
		e.get("/api/incidents/"+id, &in)
		return in.Incident.Status == "done"
	})
	j := in.Incident
	// The innermost cause names the incident and gives the place it went wrong.
	if j.Title != "ArithmeticException: / by zero" || len(j.Errors) != 1 || j.Errors[0].Exception != "IllegalStateException" {
		t.Errorf("title %q, errors %+v", j.Title, j.Errors)
	}
	order := "order-service/src/main/java/com/shop/order/Order.java"
	if len(j.Locations) != 1 || j.Locations[0].Path != order || j.Locations[0].Line != 9 || !j.Locations[0].Origin || j.Locations[0].Blame.SHA != e.repo.Init {
		t.Errorf("locations %+v", j.Locations)
	}
	if j.Suspects[0].SHA != e.repo.Init {
		t.Errorf("the commit that wrote the failing line is the first suspect: %+v", j.Suspects)
	}
	if len(j.Causes) != 2 || len(j.Causes[0].Evidence) != 2 || j.Causes[0].Evidence[0].Kind != "log" || j.Causes[0].Evidence[1].Kind != "code" ||
		!strings.Contains(j.Causes[0].Explanation, "1 quoted lines were not") || j.Causes[1].Confidence != "low" {
		t.Errorf("causes %+v", j.Causes)
	}
	if j.Report.RootCause != "R" || j.Report.Fix != "F" || j.Model != "fake" {
		t.Errorf("report %+v", j.Report)
	}
	for _, want := range []string{"## Root cause", "## Errors", "## Where in the code", "## Fix"} {
		if !strings.Contains(in.Markdown, want) {
			t.Errorf("markdown lacks %s", want)
		}
	}
	var list []map[string]any
	if e.get("/api/incidents", &list); len(list) != 1 || list[0]["topCause"] != "Discount failed" || list[0]["occurrences"] != float64(1) {
		t.Errorf("list %v", list)
	}

	e.call("PUT", "/api/incidents/"+id, `{"title":"Discounts fail","confirmed":1,"runbook":"restart","report":{"summary":"new"}}`, &in)
	if in.Incident.Title != "Discounts fail" || in.Incident.Status != "confirmed" || in.Incident.Report.Summary != "new" {
		t.Errorf("update %+v", in.Incident)
	}
	e.get("/api/incidents/"+id, &in)
	if in.Incident.Title != "Discounts fail" {
		t.Error("the new title is kept")
	}
	if e.call("PUT", "/api/incidents/"+id, `{"confirmed":0}`, &in); in.Incident.Status != "done" {
		t.Errorf("unconfirmed: %s", in.Incident.Status)
	}
	for _, c := range []struct {
		method, path, body string
		code               int
	}{
		{"PUT", "/api/incidents/" + id, `{`, 400},
		{"PUT", "/api/incidents/999", `{}`, 404},
		{"GET", "/api/incidents/abc", ``, 404},
		{"POST", "/api/incidents", `{"logs":"  "}`, 400},
		{"DELETE", "/api/incidents/abc", ``, 404},
		{"DELETE", "/api/incidents/" + id, ``, 200},
		{"DELETE", "/api/incidents/" + id, ``, 404},
	} {
		if got := e.call(c.method, c.path, c.body, nil); got != c.code {
			t.Errorf("%s %s: %d, want %d", c.method, c.path, got, c.code)
		}
	}

	// Logs with no Java error, and a long title.
	e.call("POST", "/api/incidents", `{"title":"`+strings.Repeat("x", 200)+`","logs":"all quiet"}`, &made)
	waitFor(t, "the quiet incident", func() bool {
		e.get("/api/incidents/"+made["id"], &in)
		return in.Incident.Status == "done"
	})
	if len([]rune(in.Incident.Title)) != 120 || !strings.Contains(in.Incident.Report.Summary, "No Java errors") {
		t.Errorf("quiet incident: %d %q", len(in.Incident.Title), in.Incident.Report.Summary)
	}
	e.call("POST", "/api/incidents", `{"logs":"nothing to see"}`, &made)
	e.get("/api/incidents/"+made["id"], &in)
	if !strings.HasPrefix(in.Incident.Title, "Incident ") {
		t.Errorf("default title %q", in.Incident.Title)
	}

	// A model that fails leaves the incident failed, with the reason.
	e.model.mu.Lock()
	e.model.fail = true
	e.model.mu.Unlock()
	e.call("POST", "/api/incidents", `{"logs":`+quote(incidentLogs)+`}`, &made)
	var failed struct {
		Incident struct{ Status, Error string }
	}
	waitFor(t, "the failed analysis", func() bool {
		e.get("/api/incidents/"+made["id"], &failed)
		return failed.Incident.Status == "failed"
	})
	if !strings.Contains(failed.Incident.Error, "out of memory") {
		t.Errorf("error %q", failed.Incident.Error)
	}
}

// When the innermost cause is all library code, the project's own frames from the outer exception
// still locate the incident.
func TestIncidentInLibraryCode(t *testing.T) {
	e := start(t, func(c *config.Config) { c.ModelOff = true })
	logs := `org.springframework.dao.DataIntegrityViolationException: could not execute statement
	at com.shop.order.OrderService.find(OrderService.java:17)
	at com.shop.order.OrderController.get(OrderController.java:16)
Caused by: org.hibernate.exception.ConstraintViolationException: duplicate key
	at org.hibernate.engine.jdbc.spi.SqlExceptionHelper.convert(SqlExceptionHelper.java:62)
	at org.hibernate.internal.SessionImpl.flush(SessionImpl.java:1300)
`
	var made map[string]string
	e.call("POST", "/api/incidents", `{"logs":`+quote(logs)+`}`, &made)
	var in struct {
		Incident struct {
			Locations []struct {
				Path   string
				Line   int
				Origin bool
			}
			Errors []struct{ Frames []struct{ Class string } }
		}
	}
	e.get("/api/incidents/"+made["id"], &in)
	l := in.Incident.Locations
	if len(l) != 2 || l[0].Path != testrepo.ServicePath || l[0].Line != 17 || !l[0].Origin {
		t.Errorf("locations %+v", l)
	}
	if f := in.Incident.Errors[0].Frames; len(f) != 2 || !strings.HasPrefix(f[0].Class, "org.hibernate") {
		t.Errorf("the error still lists the cause's own frames: %+v", f)
	}
}

func TestWithoutModel(t *testing.T) {
	e := start(t, func(c *config.Config) { c.ModelOff = true })
	cs := e.reviewed(3, false)
	if cs[0].Model != "" {
		t.Errorf("no model: %+v", cs[0])
	}
	var st map[string]any
	if e.get("/api/status", &st); !strings.Contains(st["llmProblem"].(string), "Model review is off") || st["types"] != float64(8) {
		t.Errorf("status %v", st)
	}
	var made map[string]string
	e.call("POST", "/api/incidents", `{"title":"t","logs":`+quote(incidentLogs)+`}`, &made)
	var in struct {
		Incident struct {
			Status string
			Report struct{ Summary string }
		}
	}
	waitFor(t, "the incident", func() bool {
		e.get("/api/incidents/"+made["id"], &in)
		return in.Incident.Status == "done"
	})
	if !strings.Contains(in.Incident.Report.Summary, "model is not available") {
		t.Errorf("summary %q", in.Incident.Report.Summary)
	}
	if e.model.chats != 0 {
		t.Errorf("the model was asked %d times", e.model.chats)
	}
}

// With a history of one, code checks cover every commit and the model reads only the newest.
func TestHistoryLimit(t *testing.T) {
	e := start(t, func(c *config.Config) { c.History = 1 })
	waitFor(t, "the newest commit read by the model", func() bool {
		cs := e.commits()
		return len(cs) == 3 && cs[0].Model == "fake"
	})
	time.Sleep(300 * time.Millisecond) // give the model lane time to do more, which it must not
	cs := e.commits()
	for _, c := range cs[1:] {
		if c.Status != "done" || c.Model != "" {
			t.Errorf("older commit %+v", c)
		}
	}
	if e.app.models.length() != 0 {
		t.Errorf("%d commits still queued for the model", e.app.models.length())
	}
}

func TestModelServerAway(t *testing.T) {
	e := start(t, func(c *config.Config) { c.ModelURL = "http://127.0.0.1:1" })
	e.reviewed(3, false) // the code checks still run
	var st map[string]any
	if e.get("/api/status", &st); !strings.Contains(st["llmProblem"].(string), "no model server") {
		t.Errorf("status %v", st)
	}
	var bug struct {
		Review struct{ Status, Summary string }
	}
	e.get("/api/commits/"+e.repo.Bug, &bug)
	if bug.Review.Status != "done" || !strings.Contains(bug.Review.Summary, "the model has not read this commit yet") {
		t.Errorf("waiting for the model: %+v", bug.Review)
	}
}

func TestServing(t *testing.T) {
	e := start(t, func(c *config.Config) { c.History = 1 })
	var feats struct{ Menus map[string]bool }
	if e.get("/api/features", &feats); !feats.Menus["review"] || feats.Menus["team"] {
		t.Errorf("features %v", feats)
	}
	var missing map[string]string
	if e.get("/api/nope", &missing) != 404 || missing["error"] == "" {
		t.Error("unknown API path")
	}
	if code := e.get("/", nil); code != 200 && code != 404 {
		t.Errorf("interface: %d", code)
	}

	// Answers are compressed for clients that ask, and read back the same.
	req, _ := http.NewRequest("GET", e.url+"/api/features", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := (&http.Client{Transport: &http.Transport{DisableCompression: true}}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Encoding") != "gzip" {
		t.Fatal("not compressed")
	}
	zr, _ := gzip.NewReader(resp.Body)
	var unzipped map[string]any
	if err := json.NewDecoder(zr).Decode(&unzipped); err != nil || unzipped["menus"] == nil {
		t.Errorf("gzip body: %v", err)
	}

	// The event stream says hello, then reports a review being asked for.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ = http.NewRequestWithContext(ctx, "GET", e.url+"/api/events", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	stream, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if stream.Header.Get("Content-Encoding") == "gzip" {
		t.Error("the event stream must not be compressed")
	}
	lines := bufio.NewScanner(stream.Body)
	lines.Scan()
	if lines.Text() != "event: hello" {
		t.Fatalf("first event %q", lines.Text())
	}
	e.call("POST", "/api/commits/"+e.repo.Bug+"/review", "", nil)
	for lines.Scan() {
		if lines.Text() == "event: review" {
			return
		}
	}
	t.Error("no review event")
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestQueue(t *testing.T) {
	q := newQueue()
	q.push(job{sha: "a"}, false)
	q.push(job{sha: "b"}, false)
	q.push(job{sha: "c"}, true)
	q.push(job{sha: "b"}, true) // moves to the front
	q.push(job{sha: "a"}, false)
	if q.length() != 3 || !q.waiting("a") {
		t.Fatalf("length %d", q.length())
	}
	var order []string
	for {
		j, ok := q.pop()
		if !ok {
			break
		}
		if !q.running(j.sha) {
			t.Error("popped job is running")
		}
		q.push(job{sha: j.sha}, true) // the running job is not queued again
		order = append(order, j.sha)
		q.done(j.sha)
	}
	if strings.Join(order, "") != "bca" || q.waiting("a") {
		t.Errorf("order %v", order)
	}
}

func TestHelpers(t *testing.T) {
	patch := "@@ -3,4 +3,3 @@\n a\n-b\n+c\n-d\n\\ No newline at end of file\n@@ -20 +19 @@\n-x\n"
	if got := deletedAt(patch); len(got) != 3 || got[0] != 4 || got[1] != 5 || got[2] != 20 {
		t.Errorf("deletedAt %v", got)
	}
	for rule, want := range map[string]string{"hardcoded-secret": "security", "complex-method": "performance", "shared-date-format": "spring", "empty-catch": "bug"} {
		if category(rule, "rule") != want {
			t.Errorf("category %s", rule)
		}
	}
	if urgency("critical") != "Fix now" || urgency("medium") != "Fix soon" || urgency("info") != "When convenient" {
		t.Error("urgency")
	}
	if lastSeen("baseline", "a") != "a" || lastSeen("b", "a") != "b" {
		t.Error("lastSeen")
	}
	if base(`C:\code\shop`) != "shop" || base("shop") != "shop" || ms(time.Time{}) != 0 || short("abc") != "abc" {
		t.Error("base, ms, short")
	}
	if orName("", "b") != "b" || len(orEmptyM(nil)) != 0 || len(orEmptyR(nil)) != 0 || len(orEmpty(nil)) != 0 {
		t.Error("or helpers")
	}
	for p, want := range map[string][2]bool{
		"a/B.java": {true, false}, "a/pom.xml": {false, true}, "a/src/main/resources/application.yml": {false, true},
		"a/application.yml": {false, false}, "build.gradle.kts": {false, true},
	} {
		if j, b := wanted(p); j != want[0] || b != want[1] {
			t.Errorf("wanted %s", p)
		}
	}
}

// A page that falls behind misses events instead of holding up everyone else.
func TestHubDropsForSlowPages(t *testing.T) {
	h := newHub()
	slow := make(chan string) // nobody reads it
	h.subs[slow] = true
	done := make(chan bool)
	go func() { h.publish("x", 1); done <- true }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publish blocked on a slow page")
	}
	w := &plainWriter{header: http.Header{}}
	h.serve(w, httptest.NewRequest("GET", "/api/events", nil))
	if w.code != 500 {
		t.Errorf("a writer that cannot stream: %d", w.code)
	}
}

type plainWriter struct {
	header http.Header
	body   bytes.Buffer
	code   int
}

func (p *plainWriter) Header() http.Header         { return p.header }
func (p *plainWriter) Write(b []byte) (int, error) { return p.body.Write(b) }
func (p *plainWriter) WriteHeader(c int)           { p.code = c }
