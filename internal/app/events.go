package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// hub sends server events to every open page, so new commits and finished reviews show at once.
type hub struct {
	mu   sync.Mutex
	subs map[chan string]bool
}

func newHub() *hub { return &hub{subs: map[chan string]bool{}} }

// publish sends one event to every page. A page too slow to keep up misses it rather than stalling others.
func (h *hub) publish(event string, data any) {
	b, _ := json.Marshal(data)
	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", event, b)
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.subs {
		select {
		case c <- msg:
		default:
		}
	}
}

func (h *hub) serve(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	c := make(chan string, 32)
	h.mu.Lock()
	h.subs[c] = true
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.subs, c)
		h.mu.Unlock()
	}()
	fmt.Fprint(w, "event: hello\ndata: {}\n\n")
	flusher.Flush()
	keep := time.NewTicker(25 * time.Second)
	defer keep.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-c:
			fmt.Fprint(w, msg)
			flusher.Flush()
		case <-keep.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		}
	}
}
