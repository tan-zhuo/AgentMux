// Switching the desktop window between its own core and a remote serve is a
// desktop concern: the headless build has no window to re-point.
//go:build !headless

package app

import (
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"agentmux/internal/webserve"
)

// Where the desktop window points at startup. Empty or "local" is the app's
// own core; "remote" loads connect.addr — a machine running `agentmux --serve`.
// connect.pin is the SHA-256 fingerprint of a self-signed certificate the user
// chose to trust for that address; connect.proxyPort keeps the loopback
// proxy's port stable so the page's origin — and the token it stored — survive
// a restart.
const (
	settingConnectMode  = "connect.mode"
	settingConnectAddr  = "connect.addr"
	settingConnectPin   = "connect.pin"
	settingConnectProxy = "connect.proxyPort"
)

// RemoteProbe is what asking "can I connect to this address" found out.
type RemoteProbe struct {
	// OK: the server answered and, for https, its certificate checked out —
	// against the system roots, or against an already-pinned fingerprint.
	OK bool `json:"ok"`
	// NeedsPin: the server answered TLS but with a certificate no authority
	// vouches for. Fingerprint is its SHA-256, for the user to compare with
	// the one the server printed at startup before choosing to trust it.
	NeedsPin    bool   `json:"needsPin"`
	Fingerprint string `json:"fingerprint"`
	Error       string `json:"error"`
}

// ConnectService switches the desktop window between the local core and a
// remote serve, and remembers the choice across launches.
//
// The switch has two doorways because the two pages live in different worlds.
// The locally-served page reaches this service over the Wails bridge like any
// other. A window pointed at a remote serve cannot: its Wails runtime posts to
// the page's own origin, which is the remote server. For that page the service
// listens on this machine's loopback, and the control URL — with a per-run
// key — is handed over in the hash when the window is pointed there.
//
// A remote behind a self-signed certificate gets a third piece: the webview
// cannot be taught to trust one, so the window is pointed at a loopback
// reverse proxy instead, and the proxy's Go client verifies the server by the
// pinned fingerprint — exact bytes, no names, no authorities.
type ConnectService struct {
	core *Core

	mu         sync.Mutex
	key        string
	controlURL string
	openLocal  func()
	openRemote func(pageURL string)

	// The pinned-TLS proxy. target and pin are read per request, so pointing
	// the proxy somewhere else does not mean restarting it.
	proxyLn   net.Listener
	proxyURL  string
	target    *url.URL
	targetPin string
}

// NewConnectService binds a connect service to the core.
func NewConnectService(c *Core) *ConnectService {
	return &ConnectService{core: c}
}

// ServiceName identifies the service in Wails logs.
func (s *ConnectService) ServiceName() string { return "ConnectService" }

// SetOpeners lends the service the entrypoint's window builders. Window
// construction stays with the entrypoint, which owns chrome, theme and icon.
func (s *ConnectService) SetOpeners(local func(), remote func(pageURL string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.openLocal = local
	s.openRemote = remote
}

// StartupRemote reports whether this launch should open on a remote serve,
// and the page URL to load — the address itself, or the loopback proxy when
// the remote is pinned. A remote that does not answer right now, or whose
// certificate no longer matches its pin, is declined: the local UI can always
// switch again, and the setting is kept for the next launch to try.
func (s *ConnectService) StartupRemote() (string, bool) {
	if s.core.Store.GetSetting(settingConnectMode, "local") != "remote" {
		return "", false
	}
	addr := s.core.Store.GetSetting(settingConnectAddr, "")
	if addr == "" {
		return "", false
	}
	pin := s.core.Store.GetSetting(settingConnectPin, "")
	if !probeServe(addr, pin) {
		log.Printf("remembered remote %s is not answering (or its certificate changed); opening the local UI", addr)
		return "", false
	}
	pageURL, err := s.prepareRemote(addr, pin)
	if err != nil {
		log.Printf("could not prepare the connection to %s: %v; opening the local UI", addr, err)
		return "", false
	}
	return pageURL, true
}

// RemoteAddr returns the last remote address used, for prefilling the form.
func (s *ConnectService) RemoteAddr() string {
	return s.core.Store.GetSetting(settingConnectAddr, "")
}

// ProbeRemote asks addr whether it is a reachable serve, and on what terms:
// plain reachability for http, certificate verification for https — with the
// self-signed case answered as a fingerprint for the user to judge.
func (s *ConnectService) ProbeRemote(addr string) RemoteProbe {
	normalized, err := normalizeServeAddr(addr)
	if err != nil {
		return RemoteProbe{Error: err.Error()}
	}
	return probeRemote(normalized, s.core.Store.GetSetting(settingConnectPin, ""))
}

func probeRemote(addr, storedPin string) RemoteProbe {
	if !strings.HasPrefix(addr, "https://") {
		if probeServe(addr, "") {
			return RemoteProbe{OK: true}
		}
		return RemoteProbe{Error: fmt.Sprintf("could not reach %s — check that agentmux --serve is running there", addr)}
	}
	// System-trusted, or already pinned: either way it just works.
	if probeServe(addr, "") || (storedPin != "" && probeServe(addr, storedPin)) {
		return RemoteProbe{OK: true}
	}
	// Not trusted — fetch the certificate so the user can decide to be the
	// authority. Handshake only; nothing is sent.
	fp, err := leafFingerprint(addr)
	if err != nil {
		return RemoteProbe{Error: fmt.Sprintf("could not reach %s: %v", addr, err)}
	}
	return RemoteProbe{NeedsPin: true, Fingerprint: fp}
}

// ConnectRemote persists the choice and re-points the window at addr. pin is
// the fingerprint the user agreed to trust for a self-signed https serve, and
// empty everywhere else. This is the doorway for the locally-served page.
func (s *ConnectService) ConnectRemote(addr, pin string) error {
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
	if !probeServe(normalized, pin) {
		return fmt.Errorf("could not reach %s — check that agentmux --serve is running there", normalized)
	}
	if err := s.persist("remote", normalized, pin); err != nil {
		return err
	}
	pageURL, err := s.prepareRemote(normalized, pin)
	if err != nil {
		return err
	}
	open(pageURL)
	return nil
}

func (s *ConnectService) persist(mode, addr, pin string) error {
	if err := s.core.Store.SetSetting(settingConnectMode, mode); err != nil {
		return err
	}
	if addr != "" {
		if err := s.core.Store.SetSetting(settingConnectAddr, addr); err != nil {
			return err
		}
		// The pin belongs to the address: moving to a new server, pinned or
		// not, must forget the old server's certificate.
		return s.core.Store.SetSetting(settingConnectPin, pin)
	}
	return nil
}

// prepareRemote returns the URL the window should load for addr: the address
// itself, or — for a pinned self-signed serve — the loopback proxy that holds
// the pin, started (or re-pointed) here.
func (s *ConnectService) prepareRemote(addr, pin string) (string, error) {
	if pin == "" || !strings.HasPrefix(addr, "https://") {
		return addr, nil
	}
	target, err := url.Parse(addr)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.target, s.targetPin = target, pin
	if s.proxyLn != nil {
		return s.proxyURL, nil
	}

	// The port is kept stable across restarts because it is an origin: the
	// browser-side storage — the serve token the user typed once — lives
	// under it. Whoever held the remembered port meanwhile costs a re-entry
	// of the token, nothing worse.
	port, _ := strconv.Atoi(s.core.Store.GetSetting(settingConnectProxy, ""))
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil && port != 0 {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		return "", err
	}
	_ = s.core.Store.SetSetting(settingConnectProxy, strconv.Itoa(ln.Addr().(*net.TCPAddr).Port))

	proxy := &httputil.ReverseProxy{
		Director: func(r *http.Request) {
			s.mu.Lock()
			t := s.target
			s.mu.Unlock()
			r.URL.Scheme = t.Scheme
			r.URL.Host = t.Host
			r.Host = t.Host
		},
		// Flush every write straight through: the event stream under this is
		// a terminal, and a buffered terminal paints in lurches.
		FlushInterval: -1,
		Transport: &http.Transport{
			// Verification is replaced, not removed: exact certificate bytes
			// against the pinned fingerprint — stricter than a chain, blind
			// to names and dates, immune to any CA.
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
				VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
					s.mu.Lock()
					pin := s.targetPin
					s.mu.Unlock()
					if len(rawCerts) > 0 && webserve.Fingerprint(rawCerts[0]) == pin {
						return nil
					}
					return errors.New("server certificate does not match the pinned fingerprint")
				},
			},
		},
	}
	srv := &http.Server{Handler: proxy}
	go func() { _ = srv.Serve(ln) }()
	s.proxyLn = ln
	s.proxyURL = "http://" + ln.Addr().String()
	return s.proxyURL, nil
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
		if err := s.persist("local", "", ""); err != nil {
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
		pin := r.URL.Query().Get("pin")
		if pin == "" {
			// The page could not have shown a fingerprint dialog for a server
			// it has not probed; do that here and hand the question back.
			probe := probeRemote(normalized, "")
			if probe.NeedsPin || probe.Error != "" {
				h.Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusPreconditionRequired)
				_ = json.NewEncoder(w).Encode(probe)
				return
			}
		} else if !probeServe(normalized, pin) {
			http.Error(w, fmt.Sprintf("could not reach %s with the accepted certificate", normalized), http.StatusBadGateway)
			return
		}
		if err := s.persist("remote", normalized, pin); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		pageURL, err := s.prepareRemote(normalized, pin)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		openRemote(pageURL)
	default:
		http.Error(w, "mode must be local or remote", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// probeServe reports whether addr answers like an AgentMux serve. The
// manifest is tiny, unauthenticated, and only AgentMux serves it at this path
// — the same probe the Android shell's launch page uses. pin, when set, is
// the only certificate the https client will accept.
func probeServe(addr, pin string) bool {
	client := &http.Client{Timeout: 4 * time.Second}
	if pin != "" {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
				VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
					if len(rawCerts) > 0 && webserve.Fingerprint(rawCerts[0]) == pin {
						return nil
					}
					return errors.New("server certificate does not match the pinned fingerprint")
				},
			},
		}
	}
	res, err := client.Get(addr + "/manifest.webmanifest")
	if err != nil {
		return false
	}
	defer res.Body.Close()
	return res.StatusCode == http.StatusOK
}

// leafFingerprint handshakes with an https address and returns the SHA-256 of
// the certificate it presented. Nothing is sent past the handshake.
func leafFingerprint(addr string) (string, error) {
	u, err := url.Parse(addr)
	if err != nil {
		return "", err
	}
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), "443")
	}
	dialer := &net.Dialer{Timeout: 4 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", host, &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		return "", err
	}
	defer conn.Close()
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return "", errors.New("the server presented no certificate")
	}
	return webserve.Fingerprint(certs[0].Raw), nil
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
