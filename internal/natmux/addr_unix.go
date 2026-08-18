//go:build !windows

package natmux

import (
	"crypto/sha256"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// maxUnixPath is how much room a sockaddr_un has for a path. macOS and the
// BSDs give 104 bytes including the terminator, Linux 108; the smaller one is
// used everywhere, so an address that binds on one machine is not a surprise
// on the next.
const maxUnixPath = 103

// socketPath is where the daemon listens. Overridable for tests, which run
// several daemons side by side; otherwise one per user in the runtime dir.
func socketPath() string { return bindable(configuredSocketPath()) }

func configuredSocketPath() string {
	if p := os.Getenv("AGENTMUX_NATMUX_SOCKET"); p != "" {
		return p
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "agentmux-natmux.sock")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("agentmux-natmux-%d.sock", os.Getuid()))
}

// bindable keeps a socket address inside what the kernel will accept.
//
// Temporary directories are what makes this necessary: macOS puts them under
// /var/folders/<two random components>/T/, so a socket named inside one — a
// test's own directory, a sandboxed runtime dir — passes 104 bytes without
// looking long at all. bind() then fails with "invalid argument", which reads
// like a malformed address rather than a long one, and the daemon simply never
// comes up.
//
// The replacement is derived from the address it replaces, so every process
// that starts from the same configuration still meets: a daemon spawned by a
// client reads the same environment, shortens it the same way, and listens
// exactly where that client dials.
func bindable(path string) string {
	if len(path) <= maxUnixPath {
		return path
	}
	sum := sha256.Sum256([]byte(path))
	name := fmt.Sprintf("agentmux-%x.sock", sum[:8])
	for _, dir := range []string{os.TempDir(), "/tmp"} {
		if short := filepath.Join(dir, name); len(short) <= maxUnixPath {
			return short
		}
	}
	// Nowhere short enough to put it. The original is handed back so the
	// failure names the address somebody configured, rather than one invented
	// here in a directory that may not even exist.
	return path
}

func addrLabel() string { return socketPath() }

func listen() (net.Listener, error) {
	path := socketPath()
	ln, err := net.Listen("unix", path)
	if err != nil {
		// A stale socket from a dead daemon refuses to be listened on. If nothing
		// answers a dial, it is stale, and the file can go.
		if c, derr := net.DialTimeout("unix", path, time.Second); derr == nil {
			_ = c.Close()
			return nil, fmt.Errorf("another session daemon is already running on %s", path)
		}
		_ = os.Remove(path)
		ln, err = net.Listen("unix", path)
	}
	if err != nil {
		return nil, err
	}
	// The socket carries keystrokes into shells; nobody else on the machine has
	// any business connecting to it.
	_ = os.Chmod(path, 0o600)
	return ln, nil
}

func dial(timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", socketPath(), timeout)
}

// daemonLogPath is where the detached daemon writes its few log lines, and
// tokenPath is where its TCP token lives.
//
// Both hang off the address that was configured rather than the one the socket
// ended up at: only a socket has a length limit, so only a socket moves, and
// somebody looking for the log — or an SSH exec on the other side reading the
// token — knows the place they configured and nothing about the shortening.
func daemonLogPath() string { return configuredSocketPath() + ".log" }

func tokenPath() string { return configuredSocketPath() + ".token" }

// listenTCP is how the tests exercise the token-authenticated transport that
// remote Windows hosts use in production. Off unless asked for by address,
// because on POSIX nothing ever needs to reach this daemon over TCP.
func listenTCP() (net.Listener, string, error) {
	addr := os.Getenv("AGENTMUX_NATMUX_TCP")
	if addr == "" {
		return nil, "", nil
	}
	token, err := loadOrCreateToken(tokenPath())
	if err != nil {
		return nil, "", err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, "", err
	}
	return ln, token, nil
}
