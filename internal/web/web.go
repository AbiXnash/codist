// Package web serves Codist's pages. Pages are rendered on the server with html/template, and HTMX
// swaps parts of them, so there is no build step and no JavaScript framework. The graph is the one
// place that draws in the browser, with Cytoscape.
package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// Shell is what every page shows around its content.
type Shell struct {
	Repo, Branch, Head, Model string
	ModelOK                   bool
	ModelNote                 string
}

// Server renders pages. Each page is parsed with the shared layout into its own template set,
// so pages can each define "content" without clashing.
type Server struct {
	mux   *http.ServeMux
	pages map[string]*template.Template
	shell func() Shell
}

var funcs = template.FuncMap{
	"ago": func(t time.Time) string {
		d := time.Since(t)
		switch {
		case d < time.Minute:
			return "just now"
		case d < time.Hour:
			return fmt.Sprintf("%dm ago", int(d.Minutes()))
		case d < 48*time.Hour:
			return fmt.Sprintf("%dh ago", int(d.Hours()))
		default:
			return fmt.Sprintf("%dd ago", int(d.Hours()/24))
		}
	},
	"base": func(p string) string { return p[strings.LastIndex(p, "/")+1:] },
	"json": func(v any) template.JS {
		b, _ := json.Marshal(v)
		return template.JS(b)
	},
	"add":   func(a, b int) int { return a + b },
	"thous": thousands,
	"pct": func(a, b int) int {
		if b == 0 {
			return 0
		}
		return a * 100 / b
	},
}

func thousands(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// New prepares the server. shell supplies the header values for every page.
func New(shell func() Shell) (*Server, error) {
	s := &Server{mux: http.NewServeMux(), pages: map[string]*template.Template{}, shell: shell}
	names, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	for _, n := range names {
		page := strings.TrimSuffix(strings.TrimPrefix(n, "templates/"), ".html")
		if page == "layout" {
			continue
		}
		t, err := template.New("layout.html").Funcs(funcs).ParseFS(templateFS, "templates/layout.html", n)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		s.pages[page] = t
	}
	static, _ := fs.Sub(staticFS, "static")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	return s, nil
}

// Handle registers a route.
func (s *Server) Handle(pattern string, h http.HandlerFunc) { s.mux.HandleFunc(pattern, h) }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// Page is the data a page template receives.
type Page struct {
	Shell
	Nav   string // which section is active
	Title string
	Data  any
}

// Render writes a whole page, or only its content when HTMX asks for a partial swap.
func (s *Server) Render(w http.ResponseWriter, r *http.Request, page, nav, title string, data any) {
	t, ok := s.pages[page]
	if !ok {
		http.Error(w, "no page "+page, http.StatusInternalServerError)
		return
	}
	name := "layout.html"
	if r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-Boosted") != "true" {
		name = "content"
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, Page{Shell: s.shell(), Nav: nav, Title: title, Data: data}); err != nil {
		slog.Error("render", "page", page, "err", err)
		http.Error(w, "could not show this page: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

// Fragment renders one named template from a page's set, for HTMX swaps.
func (s *Server) Fragment(w http.ResponseWriter, page, name string, data any) {
	t, ok := s.pages[page]
	if !ok {
		http.Error(w, "no page "+page, http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		slog.Error("fragment", "page", page, "name", name, "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}

// JSON writes v as JSON.
func JSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
