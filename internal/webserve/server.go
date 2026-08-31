package webserve

import (
	"compress/gzip"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Server is the headless HTTP face of AgentMux: the built frontend, the RPC
// endpoint, and the event stream, all behind one bearer token.
type Server struct {
	registry *Registry
	hub      *Hub
	token    string
	assets   fs.FS
	extra    map[string]http.Handler

	// Stream tickets: short-lived, single-use stand-ins for the token, minted
	// so EventSource — which cannot send headers — does not have to put the
	// long-lived token in a URL that proxies log.
	tmu     sync.Mutex
	tickets map[string]time.Time
}

// New assembles a server. assets is the built frontend (the directory that
// holds index.html); token is the shared secret every API request must carry.
func New(reg *Registry, hub *Hub, assets fs.FS, token string) *Server {
	return &Server{registry: reg, hub: hub, token: token, assets: assets, tickets: map[string]time.Time{}}
}

// Handle mounts one more route behind the same token as the rest of the API.
// It exists for the endpoints that are not RPC — a desktop session is a socket
// carrying somebody's screen, which does not fit through a call and a reply.
func (s *Server) Handle(pattern string, h http.Handler) {
	if s.extra == nil {
		s.extra = map[string]http.Handler{}
	}
	s.extra[pattern] = h
}

// Handler returns the routed handler. The UI itself is served without auth —
// it is the same static bundle anyone can download from a release — while
// everything under /api, which is what actually touches servers, requires the
// token.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth", s.handleAuth)
	mux.Handle("POST /api/call", s.requireToken(gzipped(http.HandlerFunc(s.handleCall))))
	mux.Handle("POST /api/stream-ticket", s.requireToken(http.HandlerFunc(s.handleStreamTicket)))
	// The event stream is never compressed: gzip buffers, and a buffered
	// stream is a terminal that paints in lurches.
	mux.Handle("GET /api/events", s.requireToken(s.hub))
	for pattern, h := range s.extra {
		mux.Handle(pattern, s.requireToken(h))
	}
	mux.Handle("/", gzipped(http.HandlerFunc(s.handleAssets)))
	return mux
}

// authorized checks the bearer token from the header or, for EventSource —
// which cannot set headers — a stream ticket or the token in the query string.
// The query token stays accepted for older pages; new ones carry a ticket.
func (s *Server) authorized(r *http.Request) bool {
	if t := r.URL.Query().Get("ticket"); t != "" && s.redeemTicket(t) {
		return true
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if got == "" || got == r.Header.Get("Authorization") {
		got = r.URL.Query().Get("token")
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) == 1
}

// handleStreamTicket mints a single-use, sixty-second stand-in for the token.
// The caller proved it holds the token (this sits behind requireToken); what
// it gets back is safe to put in a URL, because by the time a log is read the
// ticket is spent or stale.
func (s *Server) handleStreamTicket(w http.ResponseWriter, r *http.Request) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		writeError(w, http.StatusInternalServerError, "could not mint a ticket")
		return
	}
	t := hex.EncodeToString(raw)

	s.tmu.Lock()
	now := time.Now()
	for k, exp := range s.tickets {
		if now.After(exp) {
			delete(s.tickets, k)
		}
	}
	s.tickets[t] = now.Add(60 * time.Second)
	s.tmu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"ticket": t})
}

// redeemTicket spends a ticket: valid once, and only while fresh.
func (s *Server) redeemTicket(t string) bool {
	s.tmu.Lock()
	defer s.tmu.Unlock()
	exp, ok := s.tickets[t]
	if !ok {
		return false
	}
	delete(s.tickets, t)
	return time.Now().Before(exp)
}

func (s *Server) requireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			writeError(w, http.StatusUnauthorized, "invalid or missing token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleAuth lets the token gate validate a token before storing it, without
// exposing anything else.
func (s *Server) handleAuth(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request")
		return
	}
	if subtle.ConstantTimeCompare([]byte(body.Token), []byte(s.token)) != 1 {
		writeError(w, http.StatusUnauthorized, "invalid token")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// callRequest is one RPC over the wire: the Wails-style bound name and
// positional arguments.
type callRequest struct {
	Name string            `json:"name"`
	Args []json.RawMessage `json:"args"`
}

func (s *Server) handleCall(w http.ResponseWriter, r *http.Request) {
	var req callRequest
	// File writes travel through this endpoint as base64, so the cap is sized
	// for content, not just control messages.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "malformed request")
		return
	}
	result, err := s.registry.Call(req.Name, req.Args)
	if err != nil {
		// Service errors are expected traffic (a bad path, a dead host), not
		// server faults; 422 keeps them apart from transport failures.
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(result)
}

// gzipPool keeps writers warm; a compressor per request is measurable work.
var gzipPool = sync.Pool{New: func() any {
	w, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
	return w
}}

type gzipWriter struct {
	http.ResponseWriter
	gz          *gzip.Writer
	wroteHeader bool
}

func (g *gzipWriter) WriteHeader(code int) {
	if !g.wroteHeader {
		g.wroteHeader = true
		g.Header().Del("Content-Length")
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzipWriter) Write(b []byte) (int, error) {
	if !g.wroteHeader {
		g.WriteHeader(http.StatusOK)
	}
	return g.gz.Write(b)
}

// alreadyDense is what gzip cannot help: formats that carry their own
// compression. Everything else the app serves — JS, CSS, HTML, JSON, wasm —
// shrinks three- to four-fold, which on a phone on a weak link is the
// difference between a load and a timeout.
func alreadyDense(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".woff", ".woff2", ".zip", ".gz", ".mp4":
		return true
	}
	return false
}

// gzipped compresses responses for clients that ask for it. Streams must not
// pass through here — a compressor buffers, and a buffered event stream is a
// terminal that paints in lurches.
func gzipped(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") ||
			r.Header.Get("Range") != "" || alreadyDense(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		gz := gzipPool.Get().(*gzip.Writer)
		defer gzipPool.Put(gz)
		gz.Reset(w)
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		gw := &gzipWriter{ResponseWriter: w, gz: gz}
		next.ServeHTTP(gw, r)
		_ = gz.Close()
	})
}

// handleAssets serves the built frontend with an SPA fallback: any path that
// is not a real file gets index.html, so a reload deep in the app still boots.
func (s *Server) handleAssets(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(filepath.ToSlash(r.URL.Path), "/")
	if p == "" {
		p = "index.html"
	}
	if f, err := s.assets.Open(p); err == nil {
		_ = f.Close()
		// Go's mime table does not know the PWA manifest, and a wrong type
		// here quietly breaks home-screen install. The CORS header is for the
		// Android shell's connect page, which probes this file from its own
		// origin to learn whether the core is up — without it the browser
		// hides the response and a running core looks dead.
		if strings.HasSuffix(p, ".webmanifest") {
			w.Header().Set("Content-Type", "application/manifest+json")
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		// Vite stamps a content hash into every filename under assets/, so
		// those may be kept forever; everything else (index.html, sw.js, the
		// manifest) must be re-asked-for or an upgrade never reaches the page.
		if strings.HasPrefix(p, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		http.FileServerFS(s.assets).ServeHTTP(w, r)
		return
	}
	// The fallback is for routes, not for files. A missing asset answered with
	// index.html is a script tag that receives HTML, which the browser reports
	// as a syntax error inside a file that is fine — the Wails runtime asking
	// a served browser for /wails/custom.js is exactly this, and it is a
	// failed request on every page load. A path that names a file gets the
	// answer it deserves.
	if path.Ext(p) != "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	r.URL.Path = "/"
	http.FileServerFS(s.assets).ServeHTTP(w, r)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// LoadOrCreateToken returns the serve token: the AGENTMUX_TOKEN environment
// variable if set, otherwise a token persisted next to the database so it
// survives restarts — a tablet that stored it once keeps working.
func LoadOrCreateToken(dataDir string) (string, error) {
	if t := strings.TrimSpace(os.Getenv("AGENTMUX_TOKEN")); t != "" {
		return t, nil
	}
	path := filepath.Join(dataDir, "serve-token")
	if b, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(b)); t != "" {
			return t, nil
		}
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	t := hex.EncodeToString(raw)
	if err := os.WriteFile(path, []byte(t+"\n"), 0o600); err != nil {
		return "", err
	}
	return t, nil
}
