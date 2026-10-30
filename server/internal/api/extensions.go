package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"codist/internal/incident"
	"codist/internal/knowledge"
	"codist/internal/review"
)

func (s *Server) extensionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/incidents", s.listIncidents)
	mux.HandleFunc("POST /api/incidents", s.createIncident)
	mux.HandleFunc("GET /api/incidents/{id}", s.getIncident)
	mux.HandleFunc("PUT /api/incidents/{id}", s.updateIncident)
	mux.HandleFunc("DELETE /api/incidents/{id}", func(w http.ResponseWriter, r *http.Request) {
		s.Incidents.Store().Delete(r.PathValue("id"))
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /api/incidents/{id}/fix", s.incidentFix)
	mux.HandleFunc("POST /api/incidents/{id}/test", s.incidentTest)
	mux.HandleFunc("POST /api/incidents/{id}/rule", s.incidentRule)
	mux.HandleFunc("POST /api/incidents/{id}/runbook", s.incidentRunbook)
	mux.HandleFunc("POST /api/incidents/{id}/runbook/save", s.saveRunbook)
	mux.HandleFunc("POST /api/rules/learned", s.saveLearnedRule)
	mux.HandleFunc("POST /api/tests", s.issueTest)
	mux.HandleFunc("GET /api/release", s.release)
	mux.HandleFunc("POST /api/release/signoff", s.signOff)
	mux.HandleFunc("DELETE /api/release/signoff/{to}/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := s.SignOffs.Remove(r.PathValue("to"), r.PathValue("id")); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.Hub.Publish("signoff", map[string]string{"to": r.PathValue("to")})
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /api/dependencies", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, review.Dependencies(s.Repo)) })
}

func (s *Server) incidentOr404(w http.ResponseWriter, r *http.Request) (*incident.Incident, bool) {
	in, ok := s.Incidents.Store().Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no such incident")
	}
	return in, ok
}

func (s *Server) listIncidents(w http.ResponseWriter, r *http.Request) {
	out := []map[string]any{}
	for _, in := range s.Incidents.Store().All() {
		count := 0
		for _, e := range in.Errors {
			count += e.Count
		}
		top := ""
		if in.Confirmed >= 0 && in.Confirmed < len(in.Causes) {
			top = in.Causes[in.Confirmed].Title
		} else if len(in.Causes) > 0 {
			top = in.Causes[0].Title
		}
		out = append(out, map[string]any{"id": in.ID, "title": in.Title, "status": in.Status, "createdAt": in.CreatedAt,
			"errors": len(in.Errors), "occurrences": count, "confirmed": in.Confirmed >= 0, "topCause": top})
	}
	writeJSON(w, out)
}

func (s *Server) createIncident(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
		Logs  string `json:"logs"`
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 16<<20))
	if err != nil || json.Unmarshal(body, &req) != nil {
		writeError(w, http.StatusBadRequest, "send {title, logs}")
		return
	}
	in, err := s.Incidents.Start(req.Title, req.Logs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, map[string]string{"id": in.ID})
}

func (s *Server) getIncident(w http.ResponseWriter, r *http.Request) {
	in, ok := s.incidentOr404(w, r)
	if !ok {
		return
	}
	writeJSON(w, map[string]any{"incident": in, "markdown": incident.ReportMarkdown(in)})
}

func (s *Server) updateIncident(w http.ResponseWriter, r *http.Request) {
	in, ok := s.incidentOr404(w, r)
	if !ok {
		return
	}
	var req struct {
		Title     *string          `json:"title"`
		Report    *incident.Report `json:"report"`
		Confirmed *int             `json:"confirmed"`
		Runbook   *string          `json:"runbook"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Title != nil && strings.TrimSpace(*req.Title) != "" {
		in.Title = strings.TrimSpace(*req.Title)
	}
	if req.Report != nil {
		in.Report = *req.Report
	}
	if req.Confirmed != nil {
		in.Confirmed = *req.Confirmed
		if in.Confirmed >= 0 && in.Confirmed < len(in.Causes) {
			in.Status = "confirmed"
		} else {
			in.Confirmed = -1
			if in.Status == "confirmed" {
				in.Status = "done"
			}
		}
	}
	if req.Runbook != nil {
		in.Runbook = *req.Runbook
	}
	s.Incidents.Store().Save(in)
	s.Hub.Publish("incident", map[string]string{"id": in.ID, "status": in.Status})
	writeJSON(w, map[string]any{"incident": in, "markdown": incident.ReportMarkdown(in)})
}

func causeOf(w http.ResponseWriter, r *http.Request, in *incident.Incident) (incident.Cause, bool) {
	var req struct {
		Cause int `json:"cause"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Cause < 0 || req.Cause >= len(in.Causes) {
		writeError(w, http.StatusBadRequest, "no such cause")
		return incident.Cause{}, false
	}
	return in.Causes[req.Cause], true
}

func (s *Server) incidentFix(w http.ResponseWriter, r *http.Request) {
	in, ok := s.incidentOr404(w, r)
	if !ok {
		return
	}
	c, ok := causeOf(w, r, in)
	if !ok {
		return
	}
	if c.Path == "" {
		writeError(w, http.StatusBadRequest, "this cause is not tied to a file")
		return
	}
	// When Codist already tracks an open issue with an exact fix at this spot, use it.
	for _, is := range s.Tracker.Issues() {
		if is.State != "open" || is.Path != c.Path || !review.Instant(review.Finding{Ref: is.Ref}) {
			continue
		}
		text := strings.ToLower(c.Title + " " + c.Explanation)
		if (c.Line > 0 && abs(is.Line-c.Line) <= 3) || (is.Ref == "uninjected-dependency" && strings.Contains(text, "inject")) {
			if p, err := s.Fixer.Propose(is.LastSeen, is.Path, is.ReviewLine, is.Title); err == nil {
				p.Message = strings.Replace(p.Message, "\n\n", fmt.Sprintf("\n\nRoot cause of incident \"%s\".\n", in.Title), 1)
				writeJSON(w, p)
				return
			}
		}
	}
	instruction := fmt.Sprintf("Fix the production incident \"%s\".\nRoot cause: %s. %s\nWhat to change: %s", in.Title, c.Title, c.Explanation, c.Fix)
	if c.Line > 0 {
		instruction += fmt.Sprintf("\nThe error is thrown at line %d.", c.Line)
	}
	p, err := s.Fixer.ProposeEdit(c.Path, instruction)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	p.Message = fmt.Sprintf("fix(%s): %s\n\nRoot cause of incident \"%s\": %s\n\nFound by Codist RCA.", strings.TrimSuffix(filepath.Base(c.Path), ".java"),
		strings.ToLower(c.Title[:1])+c.Title[1:], in.Title, c.Explanation)
	writeJSON(w, p)
}

func (s *Server) incidentTest(w http.ResponseWriter, r *http.Request) {
	in, ok := s.incidentOr404(w, r)
	if !ok {
		return
	}
	c, ok := causeOf(w, r, in)
	if !ok {
		return
	}
	defect := fmt.Sprintf("%s. %s Observed in production as: ", c.Title, c.Explanation)
	if len(in.Errors) > 0 {
		e := in.Errors[0]
		defect += e.Exception + ": " + e.Message
	}
	// Wiring defects get the standard, exact test.
	if m := injectedField(s.Tracker.Issues(), c); m != "" {
		if p, err := s.Fixer.InjectionRegressionTest(c.Path, m, c.Title); err == nil {
			writeJSON(w, p)
			return
		}
	}
	exc := ""
	if len(in.Errors) > 0 {
		exc = in.Errors[0].Exception
	}
	p, err := s.Fixer.ProposeTest(c.Path, defect, s.Reviews.Build(s.Repo.Head()).Summary(), exc)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, p)
}

var injectionTitle = regexp.MustCompile(`is never injected: \w+\.(\w+) stays null`)

// injectedField returns the field of an open uninjected-dependency issue matching the cause.
func injectedField(issues []review.Issue, c incident.Cause) string {
	text := strings.ToLower(c.Title + " " + c.Explanation)
	for _, is := range issues {
		if is.State == "open" && is.Ref == "uninjected-dependency" && is.Path == c.Path && strings.Contains(text, "inject") {
			if m := injectionTitle.FindStringSubmatch(is.Title); m != nil {
				return m[1]
			}
		}
	}
	return ""
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// issueTest writes a regression test for a tracked issue.
func (s *Server) issueTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path   string `json:"path"`
		Title  string `json:"title"`
		Detail string `json:"detail"`
		Line   int    `json:"line"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
		writeError(w, http.StatusBadRequest, "path is required")
		return
	}
	if m := injectionTitle.FindStringSubmatch(req.Title); m != nil {
		if p, err := s.Fixer.InjectionRegressionTest(req.Path, m[1], req.Title); err == nil {
			writeJSON(w, p)
			return
		}
	}
	defect := fmt.Sprintf("%s (line %d). %s", req.Title, req.Line, req.Detail)
	p, err := s.Fixer.ProposeTest(req.Path, defect, s.Reviews.Build(s.Repo.Head()).Summary())
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, p)
}

func (s *Server) incidentRule(w http.ResponseWriter, r *http.Request) {
	in, ok := s.incidentOr404(w, r)
	if !ok {
		return
	}
	var req struct {
		Cause int `json:"cause"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	text, err := s.Incidents.RuleFor(in, req.Cause)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, map[string]string{"yaml": text})
}

func (s *Server) saveLearnedRule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		YAML string `json:"yaml"`
		Team bool   `json:"team"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	rules, err := incident.ParseRuleYAML(req.YAML)
	if err != nil {
		writeError(w, http.StatusBadRequest, "the rule is not valid YAML: "+err.Error())
		return
	}
	dir := filepath.Join(s.Cfg.DataDir, "rules")
	if req.Team {
		dir = filepath.Join(s.Repo.Root, ".codist", "rules")
	}
	path := filepath.Join(dir, "learned.yaml")
	var existing []knowledge.Rule
	if b, err := os.ReadFile(path); err == nil {
		_ = yaml.Unmarshal(b, &existing)
	}
	for _, nr := range rules {
		replaced := false
		for i := range existing {
			if existing[i].ID == nr.ID {
				existing[i], replaced = nr, true
			}
		}
		if !replaced {
			existing = append(existing, nr)
		}
	}
	out, _ := yaml.Marshal(existing)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	header := "# Rules learned from production incidents by Codist. Edit freely.\n"
	if err := os.WriteFile(path, append([]byte(header), out...), 0o644); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	where := path
	if req.Team {
		where = ".codist/rules/learned.yaml (commit it to share with the team)"
	}
	writeJSON(w, map[string]any{"saved": len(rules), "file": where, "total": len(s.Reviews.Knowledge().Rules)})
}

func (s *Server) incidentRunbook(w http.ResponseWriter, r *http.Request) {
	in, ok := s.incidentOr404(w, r)
	if !ok {
		return
	}
	md, err := s.Incidents.Runbook(in)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	in.Runbook = md
	s.Incidents.Store().Save(in)
	writeJSON(w, map[string]string{"markdown": md})
}

func (s *Server) saveRunbook(w http.ResponseWriter, r *http.Request) {
	in, ok := s.incidentOr404(w, r)
	if !ok {
		return
	}
	var req struct {
		Markdown string `json:"markdown"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	md := strings.TrimSpace(req.Markdown)
	if md == "" {
		md = in.Runbook
	}
	if md == "" {
		writeError(w, http.StatusBadRequest, "write the runbook first")
		return
	}
	name := slugify(in.Title)
	path := "docs/runbooks/" + name + ".md"
	p, err := s.Fixer.ProposeNewFile(path, md+"\n", "docs(runbook): "+in.Title+"\n\nDrafted by Codist from the incident analysis.", "Runbook: "+in.Title)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, p)
}

func (s *Server) release(w http.ResponseWriter, r *http.Request) {
	to, label := r.URL.Query().Get("to"), r.URL.Query().Get("to")
	if to == "" {
		to, label = s.Repo.Head(), "HEAD"
	}
	c, err := s.Repo.Commit(to)
	if err != nil {
		writeError(w, http.StatusNotFound, "unknown commit or tag: "+to)
		return
	}
	if label == "HEAD" || strings.HasPrefix(c.SHA, label) {
		label = c.ShortSHA + " " + c.Message
	}
	// The report only changes with the release commit, the reviews, ignores, rules or working tree.
	key := c.SHA + "|" + label + "|" + s.Tracker.Version()
	s.relMu.Lock()
	rep, ok := s.relCache[key]
	s.relMu.Unlock()
	if !ok {
		rep = review.Release(s.Repo, s.Tracker, s.Store, s.Reviews.Ignores, s.Reviews.Knowledge(), c.SHA, label)
		s.relMu.Lock()
		if len(s.relCache) > 16 {
			s.relCache = nil
		}
		if s.relCache == nil {
			s.relCache = map[string]review.ReleaseReport{}
		}
		s.relCache[key] = rep
		s.relMu.Unlock()
	}
	so := s.SignOffs.For(rep.To, rep.Checklist)
	refs := []map[string]string{}
	for i, c := range s.Repo.Log(30) {
		label := c.ShortSHA + " " + c.Message
		if i == 0 {
			label = "HEAD · " + label
		}
		refs = append(refs, map[string]string{"ref": c.SHA, "label": label})
	}
	for _, t := range s.Repo.Tags() {
		refs = append(refs, map[string]string{"ref": t, "label": "tag " + t})
	}
	writeJSON(w, map[string]any{"report": rep, "signoff": so, "markdown": review.ReleaseMarkdown(rep, so), "refs": refs})
}

// signOff ticks, unticks or adds an item on the release target's checklist.
func (s *Server) signOff(w http.ResponseWriter, r *http.Request) {
	var req struct {
		To     string `json:"to"`
		Text   string `json:"text"`
		Done   bool   `json:"done"`
		Custom bool   `json:"custom"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	c, err := s.Repo.Commit(req.To)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	by := s.Repo.UserName()
	if err := s.SignOffs.Set(c.SHA, req.Text, req.Done, req.Custom, by); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.Hub.Publish("signoff", map[string]string{"to": c.SHA})
	w.WriteHeader(http.StatusNoContent)
}

func slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 60 {
		out = strings.Trim(out[:60], "-")
	}
	if out == "" {
		out = "incident"
	}
	return out
}
