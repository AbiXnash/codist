package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func TestServe(t *testing.T) {
	h := serve(fstest.MapFS{
		"dist/index.html":      {Data: []byte("<html>app</html>")},
		"dist/assets/app-1.js": {Data: []byte("js")},
		"dist/favicon.svg":     {Data: []byte("<svg/>")},
	})
	cases := []struct{ path, body, cache string }{
		{"/", "<html>app</html>", "no-cache"},
		{"/graph/some.Class", "<html>app</html>", "no-cache"}, // a screen, not a file
		{"/assets/app-1.js", "js", "public, max-age=31536000, immutable"},
		{"/favicon.svg", "<svg/>", ""},
	}
	for _, c := range cases {
		w := get(h, c.path)
		if w.Code != 200 || w.Body.String() != c.body || w.Header().Get("Cache-Control") != c.cache {
			t.Errorf("%s: %d %q cache %q", c.path, w.Code, w.Body.String(), w.Header().Get("Cache-Control"))
		}
	}
}

func TestServeWithoutBuild(t *testing.T) {
	for _, fsys := range []fstest.MapFS{{"dist/.gitkeep": {}}, {"other/x": {}}} {
		w := get(serve(fsys), "/")
		if w.Code != 404 || !strings.Contains(w.Body.String(), "make build") {
			t.Errorf("%d %q", w.Code, w.Body.String())
		}
	}
	if w := get(UI(), "/"); w.Code != 200 && w.Code != 404 { // depends on whether web/ was built
		t.Errorf("embedded UI: %d", w.Code)
	}
}

func TestJSONAndError(t *testing.T) {
	w := httptest.NewRecorder()
	JSON(w, map[string]int{"a": 1})
	if w.Header().Get("Content-Type") != "application/json" || w.Body.String() != "{\"a\":1}\n" {
		t.Errorf("JSON: %q", w.Body.String())
	}
	w = httptest.NewRecorder()
	Error(w, http.StatusTeapot, "no tea")
	if w.Code != http.StatusTeapot || w.Body.String() != "{\"error\":\"no tea\"}\n" {
		t.Errorf("Error: %d %q", w.Code, w.Body.String())
	}
}
