// Switching the desktop window between its own core and a remote serve is a
// desktop concern: the headless build has no window to re-point.
//go:build !headless

package app

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Where the desktop window points at startup. Empty or "local" is the app's
// own core; "remote" loads connect.addr — a machine running `agentmux --serve`.
const (
	settingConnectMode = "connect.mode"
	settingConnectAddr = "connect.addr"
)

// ConnectService switches the desktop window between the local core and a
// remote serve, and remembers the choice across launches.
//
// The switch has two doorways because the two pages live in different worlds.
// The locally-served page reaches this service over the Wails bridge like any
// other. A window pointed at a remote serve cannot: its Wails runtime posts to
// the page's own origin, which is the remote server. For that page the service
// listens on this machine's loopback, and the control URL — with a per-run
// key — is handed over in the hash when the window is pointed there.
type ConnectService struct {
	core *Core

	mu         sync.Mutex
	key        string
	controlURL string
	openLocal  func()
	openRemote func(addr string)
}

// NewConnectService binds a connect service to the core.
func NewConnectService(c *Core) *ConnectService {
	return &ConnectService{core: c}
}

// ServiceName identifies the service in Wails logs.
func (s *ConnectService) ServiceName() string { return "ConnectService" }

// SetOpeners lends the service the entrypoint's window builders. Window
// construction stays with the entrypoint, which owns chrome, theme and icon.
func (s *ConnectService) SetOpeners(local func(), remote func(addr string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.openLocal = local
	s.openRemote = remote
}

// StartupRemote reports whether this launch should open on a remote serve,
// and where.
func (s *ConnectService) StartupRemote() (string, bool) {
	if s.core.Store.GetSetting(settingConnectMode, "local") != "remote" {
		return "", false
	}
	addr := s.core.Store.GetSetting(settingConnectAddr, "")
	return addr, addr != ""
}

// RemoteAddr returns the last remote address used, for prefilling the form.
func (s *ConnectService) RemoteAddr() string {
	return s.core.Store.GetSetting(settingConnectAddr, "")
}

// ConnectRemote persists the choice and re-points the window at addr. This is
// the doorway for the locally-served page.
func (s *ConnectService) ConnectRemote(addr string) error {
	normalized, err := normalizeServeAddr(addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	open, control := s.openRemote, s.controlURL
	s.mu.Unlock()
	if open == nil {
		return errors.New("window switching is not available")
	}
	// Without the control endpoint the remote page would have no way back
	// short of digging the setting out of the database, so refuse up front.
	if control == "" {
		return errors.New("the loopback control endpoint failed to start; restart AgentMux and try again")
	}
	// Refused rather than discovered later: a window pointed at a dead address
	// shows a blank page with no UI to switch back from.
	if !ProbeServe(normalized) {
		return fmt.Errorf("could not reach %s — check that agentmux --serve is running there", normalized)
	}
	if err := s.persist("remote", normalized); err != nil {
		return err
	}
	open(normalized)
	return nil
}

// ProbeServe reports whether addr answers like an AgentMux serve. The manifest
// is tiny, unauthenticated, and only AgentMux serves it at this path — the
// same probe the Android shell's launch page uses.
func ProbeServe(addr string) bool {
	client := &http.Client{Timeout: 4 * time.Second}
	res, err := client.Get(addr + "/manifest.webmanifest")
	if err != nil {
		return false
	}
	defer res.Body.Close()
	return res.StatusCode == http.StatusOK
}

func (s *ConnectService) persist(mode, addr string) error {
	if err := s.core.Store.SetSetting(settingConnectMode, mode); err != nil {
		return err
	}
	if addr != "" {
		return s.core.Store.SetSetting(settingConnectAddr, addr)
	}
	return nil
}

// ControlURL is the loopback doorway with its key, ready to hand to a remote
// page in the hash. Empty until StartControl has succeeded.
func (s *ConnectService) ControlURL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.controlURL
}

// StartControl opens the loopback listener a remotely-served page calls to
// switch this window. The key is fresh per run: the URL that carries it is
// only ever handed to pages this process itself navigated to.
func (s *ConnectService) StartControl() error {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	key := hex.EncodeToString(raw)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/connect", s.handleControl)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()

	s.mu.Lock()
	s.key = key
	s.controlURL = fmt.Sprintf("http://%s/connect?key=%s", ln.Addr().String(), key)
	s.mu.Unlock()
	return nil
}

// handleControl is called cross-origin by the page in the remote-mode window,
// so it answers CORS preflights — including Chromium's private-network one:
// without that header a page on a LAN origin is not allowed to reach loopback.
func (s *ConnectService) handleControl(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	h.Set("Access-Control-Allow-Private-Network", "true")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	s.mu.Lock()
	key, openLocal, openRemote := s.key, s.openLocal, s.openRemote
	s.mu.Unlock()
	if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("key")), []byte(key)) != 1 {
		http.Error(w, "bad key", http.StatusForbidden)
		return
	}

	switch r.URL.Query().Get("mode") {
	case "local":
		if openLocal == nil {
			http.Error(w, "window switching is not available", http.StatusServiceUnavailable)
			return
		}
		if err := s.persist("local", ""); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		openLocal()
	case "remote":
		normalized, err := normalizeServeAddr(r.URL.Query().Get("addr"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if openRemote == nil {
			http.Error(w, "window switching is not available", http.StatusServiceUnavailable)
			return
		}
		if !ProbeServe(normalized) {
			http.Error(w, fmt.Sprintf("could not reach %s — check that agentmux --serve is running there", normalized), http.StatusBadGateway)
			return
		}
		if err := s.persist("remote", normalized); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		openRemote(normalized)
	default:
		http.Error(w, "mode must be local or remote", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// normalizeServeAddr mirrors the connect forms: scheme defaulted to http,
// trailing slashes off, and the result required to actually name a host.
func normalizeServeAddr(addr string) (string, error) {
	addr = strings.TrimRight(strings.TrimSpace(addr), "/")
	if addr == "" {
		return "", errors.New("a server address is required")
	}
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		addr = "http://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("%q is not a usable server address", addr)
	}
	return addr, nil
}
