// Package web carries the built interface inside the binary, so one file is all anyone needs to run
// Codist. `make build` builds web/ and copies its output to dist/ before compiling.
package web

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var files embed.FS

// UI serves the interface. Paths that are not files fall back to index.html, so a reload on any
// screen works. Without a built interface it says how to build one.
func UI() http.Handler {
	dist, err := fs.Sub(files, "dist")
	if err == nil {
		if _, err = fs.Stat(dist, "index.html"); err == nil {
			static := http.FileServerFS(dist)
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				p := strings.TrimPrefix(r.URL.Path, "/")
				if p != "" {
					if _, err := fs.Stat(dist, p); err == nil {
						if strings.HasPrefix(p, "assets/") {
							w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
						}
						static.ServeHTTP(w, r)
						return
					}
				}
				w.Header().Set("Cache-Control", "no-cache")
				http.ServeFileFS(w, r, dist, "index.html")
			})
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "This build has no interface. Run make build, which builds web/ into the binary.", http.StatusNotFound)
	})
}

// JSON writes v as JSON.
func JSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// Error writes an error the interface shows as a message.
func Error(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
