package webserve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Hub fans backend events out to every connected browser over Server-Sent
// Events. SSE rather than a websocket because the traffic is strictly one-way
// — calls already travel as HTTP requests — and EventSource reconnects by
// itself, which is exactly what a tablet that just came back from sleep needs.
type Hub struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

// NewHub builds an empty hub.
func NewHub() *Hub { return &Hub{subs: map[chan []byte]struct{}{}} }

// Emit broadcasts one event. It matches the emitter signature Core wants, so
// serve mode plugs it in where the desktop plugs in the Wails event bus. A
// subscriber that cannot keep up loses events rather than blocking the rest;
// every stream here is advisory (status, terminal echo) and self-heals on the
// next poll or repaint.
func (h *Hub) Emit(name string, data any) {
	payload, err := json.Marshal(struct {
		Name string `json:"name"`
		Data any    `json:"data"`
	}{name, data})
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- payload:
		default:
		}
	}
}

// ServeHTTP streams events to one browser until it disconnects.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	// Terminal output arrives in bursts of small chunks; a big buffer keeps a
	// briefly stalled connection from dropping mid-burst.
	ch := make(chan []byte, 1024)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fl.Flush()

	// Heartbeats keep idle proxies from closing the stream and let the client
	// notice a dead connection.
	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			fl.Flush()
		case msg := <-ch:
			if _, err := fmt.Fprintf(w, "data: %s\n\n", msg); err != nil {
				return
			}
			fl.Flush()
		}
	}
}
