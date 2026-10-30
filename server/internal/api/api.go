// Package api serves the REST API, the event stream and the built web UI.
package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"codist/internal/config"
	"codist/internal/events"
	"codist/internal/gitx"
	"codist/internal/graph"
	"codist/internal/incident"
	"codist/internal/review"
	"codist/internal/watch"
	"codist/prompts"
)

type Server struct {
	Cfg       config.Config
	Repo      *gitx.Repo
	Graph     *graph.Service
	Analyzer  *review.Analyzer
	Reviews   *review.Service
	Store     *review.Store
	LLM       *review.Ollama
	Hub       *events.Hub
	Watcher   *watch.Watcher
	Fixer     *review.Fixer
	Tracker   *review.Tracker
	Incidents *incident.Engine
	SignOffs  *review.SignOffs

	relMu    sync.Mutex
	relCache map[string]review.ReleaseReport // release commit + tracker version -> report
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/status", s.status)
	mux.HandleFunc("GET /api/graph", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.Graph.Graph()) })
	mux.HandleFunc("GET /api/commits", s.commits)
	mux.HandleFunc("GET /api/commits/{sha}", s.commit)
	mux.HandleFunc("POST /api/commits/{sha}/review", s.review)
	mux.HandleFunc("GET /api/file", s.file)
	mux.Handle("GET /api/events", s.Hub)
	mux.HandleFunc("POST /api/webhook/github", s.webhook)
	mux.HandleFunc("GET /api/ignores", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.Reviews.Ignores.All()) })
	mux.HandleFunc("POST /api/ignores", s.addIgnore)
	mux.HandleFunc("DELETE /api/ignores/{id}", s.removeIgnore)
	mux.HandleFunc("POST /api/ask", s.ask)
	mux.HandleFunc("POST /api/edit", s.edit)
	mux.HandleFunc("GET /api/rules", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.Reviews.Knowledge().Rules) })
	mux.HandleFunc("GET /api/issues", s.issues)
	s.extensionRoutes(mux)
	mux.HandleFunc("POST /api/fix", s.proposeFix)
	mux.HandleFunc("POST /api/fix/{id}/apply", s.applyFix)
	mux.HandleFunc("GET /api/prompts", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, prompts.All()) })
	mux.HandleFunc("GET /api/working", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, review.WorkingReview(s.Repo, s.Fixer.Rules, s.Reviews.Ignores))
	})
	mux.HandleFunc("DELETE /api/fix/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.Fixer.Discard(r.PathValue("id"))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.Handle("/", s.web())
	return mux
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	g := s.Graph.Graph()
	writeJSON(w, map[string]any{
		"repo": s.Repo.Name(), "path": s.Repo.Root, "branch": s.Repo.Branch, "tracking": s.Repo.Tracking(),
		"head": s.Repo.Head(), "model": s.LLM.Model, "llmProblem": s.LLM.Problem(),
		"graphSource": g.Source, "graphBuiltAt": g.BuiltAt,
		"canFix": s.Fixer.Enabled(), "rules": len(s.Reviews.Knowledge().Rules), "user": s.Repo.UserName(),
		"models": map[string]string{"review": s.LLM.Model, "summary": firstNonEmpty(s.Cfg.SummaryModel, s.LLM.Model),
			"fix": firstNonEmpty(s.Cfg.FixModel, s.LLM.Model), "ask": firstNonEmpty(s.Cfg.AskModel, s.LLM.Model)},
	})
}

func (s *Server) commits(w http.ResponseWriter, r *http.Request) {
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit <= 0 {
		limit = 60
	}
	out := []map[string]any{}
	issues := s.Tracker.Issues()
	fixed := review.FixedBy(issues)
	for _, c := range s.Repo.Log(min(limit, 300)) {
		item := map[string]any{"commit": c, "fixed": fixed[c.SHA]}
		if rv, ok := s.Store.Get(c.SHA); ok {
			rv = s.Tracker.Annotate(review.AssignGroups(s.Reviews.Ignores.Decorate(rv)), issues)
			item["status"], item["risk"], item["counts"] = rv.Status, rv.Risk, rv.Counts()
			item["messageProblem"] = rv.MessageProblem != ""
		}
		out = append(out, item)
	}
	writeJSON(w, out)
}

func (s *Server) commit(w http.ResponseWriter, r *http.Request) {
	a, err := s.Analyzer.Analyze(r.PathValue("sha"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	var rv any
	if found, ok := s.Store.Get(a.Commit.SHA); ok {
		issues := s.Tracker.Issues()
		annotated := s.Tracker.Annotate(review.AssignGroups(s.Reviews.Ignores.Decorate(found)), issues)
		rv = annotated
		ready := s.Fixer.Ready(found.SHA)
		for _, fr := range annotated.Files {
			for _, f := range fr.Findings {
				if review.Instant(f) {
					ready[fr.Path+"|"+itoa(f.Line)+"|"+f.Title] = "instant"
				}
			}
		}
		writeJSON(w, map[string]any{"analysis": a, "review": rv, "fixes": ready, "resolved": review.FixedInCommit(issues, a.Commit.SHA)})
		return
	}
	// Even an unreviewed commit can have fixed earlier issues.
	writeJSON(w, map[string]any{"analysis": a, "review": rv, "fixes": map[string]string{},
		"resolved": review.FixedInCommit(s.Tracker.Issues(), a.Commit.SHA)})
}

func (s *Server) review(w http.ResponseWriter, r *http.Request) {
	c, err := s.Repo.Commit(r.PathValue("sha"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusAccepted)
	writeJSON(w, map[string]any{"sha": c.SHA, "queued": s.Reviews.Enqueue(c.SHA)})
}

// file returns content at a commit, or from the working tree / tracked head when sha is empty.
func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	path, sha := r.URL.Query().Get("path"), r.URL.Query().Get("sha")
	if path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	var content string
	var ok bool
	switch {
	case sha != "":
		if _, err := s.Repo.Commit(sha); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		content, ok = s.Repo.ReadFile(sha, path)
	case s.Repo.Remote:
		content, ok = s.Repo.ReadFile(s.Repo.Head(), path)
	default:
		var err error
		if content, ok, err = s.Repo.ReadWorking(path); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if !ok {
		writeError(w, http.StatusNotFound, "no such file: "+path)
		return
	}
	writeJSON(w, map[string]any{"path": path, "sha": sha, "content": content})
}

// webhook handles GitHub push events. Expose it with cloudflared or ngrok.
func (s *Server) webhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 25<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "unreadable body")
		return
	}
	if s.Cfg.WebhookSecret != "" {
		mac := hmac.New(sha256.New, []byte(s.Cfg.WebhookSecret))
		mac.Write(body)
		want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(want), []byte(r.Header.Get("X-Hub-Signature-256"))) {
			writeError(w, http.StatusUnauthorized, "bad signature")
			return
		}
	}
	s.Watcher.Poke()
	writeJSON(w, map[string]string{"ok": "true"})
}

func (s *Server) proposeFix(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SHA      string `json:"sha"`
		Path     string `json:"path"`
		Line     int    `json:"line"`
		Title    string `json:"title"`
		Guidance string `json:"guidance"` // optional: how the developer wants it fixed
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	sha := review.WorkingSHA
	if req.SHA != review.WorkingSHA {
		c, err := s.Repo.Commit(req.SHA)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		sha = c.SHA
	}
	p, err := s.Fixer.ProposeGuided(sha, req.Path, req.Line, req.Title, req.Guidance)
	var off review.ErrOffTopic
	if errors.As(err, &off) {
		writeJSON(w, map[string]any{"blocked": true, "error": off.Reason})
		return
	}
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, p)
}

func (s *Server) applyFix(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Message string `json:"message"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	sha, err := s.Fixer.Apply(r.PathValue("id"), req.Message)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, map[string]string{"commit": sha})
}

func itoa(n int) string { return strconv.Itoa(n) }

// issues lists tracked issues across all commits; ?state=open|resolved|ignored|all (default open).
func (s *Server) issues(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	if state == "" {
		state = "open"
	}
	all := s.Tracker.Issues()
	counts := map[string]int{}
	bySeverity := map[string]int{}
	list := []review.Issue{}
	for _, is := range all {
		if is.Group == "" || is.GroupRoot || is.State != "open" {
			counts[is.State]++
			if is.State == "open" {
				bySeverity[is.Severity]++
			}
		}
		if state == "all" || is.State == state {
			list = append(list, is)
		}
	}
	writeJSON(w, map[string]any{"issues": list, "counts": counts, "openBySeverity": bySeverity})
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (s *Server) addIgnore(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Scope  string `json:"scope"` // commit, code, rule
		SHA    string `json:"sha"`
		Path   string `json:"path"`
		Line   int    `json:"line"`
		Title  string `json:"title"`
		Reason string `json:"reason"`
		Team   bool   `json:"team"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	var c gitx.Commit
	var rv review.Review
	if req.SHA == review.WorkingSHA {
		// Uncommitted changes: ignore the line or the rule (a commit-scoped ignore has no commit yet).
		if req.Scope == "commit" {
			req.Scope = "code"
		}
		rv = review.Review{Files: review.WorkingReview(s.Repo, s.Fixer.Rules, s.Reviews.Ignores).Files}
	} else {
		var err error
		if c, err = s.Repo.Commit(req.SHA); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		var ok bool
		if rv, ok = s.Store.Get(c.SHA); !ok {
			writeError(w, http.StatusNotFound, "commit has not been reviewed")
			return
		}
	}
	var f review.Finding
	found := false
	for _, fr := range rv.Files {
		for _, x := range fr.Findings {
			if fr.Path == req.Path && x.Line == req.Line && x.Title == req.Title {
				f, found = x, true
			}
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "finding not found")
		return
	}
	ig := review.Ignore{Scope: req.Scope, Key: f.Key(), Category: f.Category, Title: f.Title, Reason: req.Reason, Team: req.Team}
	switch req.Scope {
	case "commit":
		ig.SHA, ig.Path, ig.Line = c.SHA, req.Path, req.Line
	case "code":
		ig.Path, ig.Snippet = req.Path, f.Snippet
		if f.Snippet == "" {
			if src, ok := s.Repo.ReadFile(c.SHA, req.Path); ok {
				lines := strings.Split(src, "\n")
				if req.Line >= 1 && req.Line <= len(lines) {
					ig.Snippet = strings.TrimSpace(lines[req.Line-1])
				}
			}
		}
	case "rule":
		if f.Ref == "" {
			writeError(w, http.StatusBadRequest, "this finding has no rule id; ignore it in this commit or on this line instead")
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "scope must be commit, code or rule")
		return
	}
	saved, err := s.Reviews.Ignores.Add(ig)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.Hub.Publish("ignores", map[string]any{"changed": true})
	writeJSON(w, saved)
}

func (s *Server) removeIgnore(w http.ResponseWriter, r *http.Request) {
	if err := s.Reviews.Ignores.Remove(r.PathValue("id")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.Hub.Publish("ignores", map[string]any{"changed": true})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path     string `json:"path"`
		Question string `json:"question"`
		Node     string `json:"node"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Question) == "" {
		writeError(w, http.StatusBadRequest, "question is required")
		return
	}
	// Shared context for the Ask model: what the graph knows about the selected type.
	var ctx strings.Builder
	if b := s.Reviews.Build(s.Repo.Head()).Summary(); b != "" {
		ctx.WriteString("## Build\n" + b + "\n")
	}
	for _, n := range s.Graph.Graph().Nodes {
		if n.ID == req.Node || (req.Node == "" && n.File == req.Path) {
			ctx.WriteString("\n## Type " + n.ID + " (" + n.Stereotype + ")\n")
			for _, nb := range s.Graph.Neighbours(n.ID) {
				ctx.WriteString("- linked to " + nb.ID + " (" + nb.Stereotype + ")\n")
			}
		}
	}
	answer, err := s.Fixer.Ask(req.Path, req.Question, ctx.String())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, map[string]string{"answer": answer})
}

func (s *Server) edit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path        string `json:"path"`
		Instruction string `json:"instruction"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" || strings.TrimSpace(req.Instruction) == "" {
		writeError(w, http.StatusBadRequest, "path and instruction are required")
		return
	}
	p, err := s.Fixer.ProposeEdit(req.Path, req.Instruction)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, p)
}

// web serves the built Svelte app, falling back to index.html for client-side routes.
func (s *Server) web() http.Handler {
	dir := s.Cfg.WebDir
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
			http.Error(w, "UI not built. Run `npm run build` in web/, or use the Vite dev server on :5173.", http.StatusNotFound)
			return
		}
		p := filepath.Join(dir, filepath.Clean("/"+r.URL.Path))
		if info, err := os.Stat(p); err != nil || info.IsDir() && !strings.HasSuffix(r.URL.Path, "/") {
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
