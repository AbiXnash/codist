// Package web holds the small helpers the API uses to answer in JSON. The interface itself is the codist-ui
// module.
package web

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// JSON writes v as JSON.
func JSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// Error writes an error the interface shows as a message.
func Error(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	slog.Debug("api error", "code", code, "msg", msg)
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
