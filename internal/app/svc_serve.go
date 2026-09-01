// Serve mode inside the desktop app: the same HTTP face the headless build
// wears, layered onto the running desktop's own core — so the machine under
// the desk is a server the moment its owner flips the switch, with no second
// process and no second state. Desktop-only: the headless build IS a server.
//go:build !headless

package app

import (
	"crypto/tls"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"agentmux/internal/store"
	"agentmux/internal/webserve"
)

// Serve-mode settings. Enabled survives restarts: a machine that was a server
// goes back to being one when the app reopens.
const (
	settingServeEnabled = "serve.enabled"
	settingServeAddr    = "serve.addr"
	settingServeTLS     = "serve.tls"
)

// ServeConfig is the user's choice: whether to serve, where, and how.
type ServeConfig struct {
	Enabled bool   `json:"enabled"`
	Addr    string `json:"addr"`
	TLS     bool   `json:"tls"`
}

// ServeStatus is everything the settings dialog shows: whether the server is
// up, where a phone should point, and the two values a connecting device asks
// its user to verify — the token and the certificate fingerprint.
type ServeStatus struct {
	Running     bool     `json:"running"`
	Addr        string   `json:"addr"`
	TLS         bool     `json:"tls"`
	URLs        []string `json:"urls"`
	Token       string   `json:"token"`
	Fingerprint string   `json:"fingerprint"`
	Error       string   `json:"error"`
}

// ServeService turns the desktop app into an `agentmux --serve` on demand.
type ServeService struct {
	core *Core

	// What the HTTP face is made of, lent by the entrypoint: the same service
	// instances the window binds (one state, two transports), the shared
	// event hub, and the embedded frontend.
	services []any
	hub      *webserve.Hub
	assets   fs.FS

	mu          sync.Mutex
	srv         *http.Server
	ln          net.Listener
	fingerprint string
	lastErr     string
}

// NewServeService binds a serve service to the core.
func NewServeService(c *Core, hub *webserve.Hub) *ServeService {
	return &ServeService{core: c, hub: hub}
}

// ServiceName identifies the service in Wails logs.
func (s *ServeService) ServiceName() string { return "ServeService" }

// SetBacking hands over the pieces the HTTP face is built from. Called once
// by the entrypoint before anything can start.
func (s *ServeService) SetBacking(services []any, assets fs.FS) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.services = services
	s.assets = assets
}

// Config returns the persisted choice, defaults filled in.
func (s *ServeService) Config() ServeConfig {
	return ServeConfig{
		Enabled: s.core.Store.GetSetting(settingServeEnabled, "") == "1",
		Addr:    s.core.Store.GetSetting(settingServeAddr, ":8642"),
		TLS:     s.core.Store.GetSetting(settingServeTLS, "1") == "1",
	}
}

// Start persists the configuration and brings the server up (restarting it if
// it was already running on different terms).
func (s *ServeService) Start(cfg ServeConfig) ServeStatus {
	addr := strings.TrimSpace(cfg.Addr)
	if addr == "" {
		addr = ":8642"
	}
	_ = s.core.Store.SetSetting(settingServeEnabled, "1")
	_ = s.core.Store.SetSetting(settingServeAddr, addr)
	tlsVal := ""
	if cfg.TLS {
		tlsVal = "1"
	}
	_ = s.core.Store.SetSetting(settingServeTLS, tlsVal)

	s.stop()
	if err := s.start(addr, cfg.TLS); err != nil {
		s.mu.Lock()
		s.lastErr = err.Error()
		s.mu.Unlock()
	}
	return s.Status()
}

// Stop persists the choice to not serve, and stops.
func (s *ServeService) Stop() ServeStatus {
	_ = s.core.Store.SetSetting(settingServeEnabled, "")
	s.stop()
	return s.Status()
}

// AutoStart brings the server up at launch when it was left enabled. Failure
// is recorded, not fatal: the window must open either way, and the settings
// dialog is where the error explains itself.
func (s *ServeService) AutoStart() {
	cfg := s.Config()
	if !cfg.Enabled {
		return
	}
	if err := s.start(cfg.Addr, cfg.TLS); err != nil {
		s.mu.Lock()
		s.lastErr = err.Error()
		s.mu.Unlock()
	}
}

// Status reports the current state, addresses a device can try, and the
// values that device's user will be asked to verify.
func (s *ServeService) Status() ServeStatus {
	cfg := s.Config()
	s.mu.Lock()
	running := s.srv != nil
	fingerprint := s.fingerprint
	lastErr := s.lastErr
	var port string
	if s.ln != nil {
		if tcp, ok := s.ln.Addr().(*net.TCPAddr); ok {
			port = ":" + strconv.Itoa(tcp.Port)
		}
	}
	s.mu.Unlock()

	st := ServeStatus{Running: running, Addr: cfg.Addr, TLS: cfg.TLS, Error: lastErr}
	if !running {
		return st
	}
	st.Fingerprint = fingerprint
	if dataDir, err := store.AppDir(); err == nil {
		if token, err := webserve.LoadOrCreateToken(dataDir); err == nil {
			st.Token = token
		}
	}
	scheme := "http"
	if cfg.TLS {
		scheme = "https"
	}
	// Every address this machine holds right now, ready to tap on a phone.
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok || ipn.IP.IsLoopback() || ipn.IP.To4() == nil {
				continue
			}
			st.URLs = append(st.URLs, scheme+"://"+ipn.IP.String()+port)
		}
	}
	return st
}

func (s *ServeService) start(addr string, useTLS bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.srv != nil {
		return nil
	}
	if s.services == nil || s.assets == nil {
		return errors.New("serve mode is not wired up")
	}

	dataDir, err := store.AppDir()
	if err != nil {
		return err
	}
	token, err := webserve.LoadOrCreateToken(dataDir)
	if err != nil {
		return err
	}

	web := webserve.New(webserve.NewRegistry(s.services...), s.hub, s.assets, token)
	for _, svc := range s.services {
		if d, ok := svc.(*DesktopService); ok {
			web.Handle("GET "+WSPath, http.HandlerFunc(d.ServeWS))
		}
	}

	// Listen synchronously so a taken port comes back as the answer to the
	// click, not a silent background failure.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: web.Handler(), ReadHeaderTimeout: 10 * time.Second}
	s.fingerprint = ""
	if useTLS {
		cert, fingerprint, err := webserve.LoadOrCreateCert(dataDir, "", "")
		if err != nil {
			ln.Close()
			return err
		}
		s.fingerprint = fingerprint
		srv.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}
		// ServeTLS rather than a hand-wrapped listener: it is the path that
		// also switches HTTP/2 on, same as the headless build.
		go func() { _ = srv.ServeTLS(ln, "", "") }()
	} else {
		go func() { _ = srv.Serve(ln) }()
	}
	s.srv = srv
	s.ln = ln
	s.lastErr = ""
	return nil
}

func (s *ServeService) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.srv == nil {
		return
	}
	_ = s.srv.Close()
	s.srv = nil
	s.ln = nil
	s.lastErr = ""
}

