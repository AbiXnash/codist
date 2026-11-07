package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

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
