//go:build !windows

package natmux

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// socketPath is where the daemon listens. Overridable for tests, which run
// several daemons side by side; otherwise one per user in the runtime dir.
func socketPath() string {
	if p := os.Getenv("AGENTMUX_NATMUX_SOCKET"); p != "" {
		return p
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "agentmux-natmux.sock")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("agentmux-natmux-%d.sock", os.Getuid()))
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

// daemonLogPath is where the detached daemon writes its few log lines.
func daemonLogPath() string {
	return socketPath() + ".log"
}

// listenTCP is how the tests exercise the token-authenticated transport that
// remote Windows hosts use in production. Off unless asked for by address,
// because on POSIX nothing ever needs to reach this daemon over TCP.
func listenTCP() (net.Listener, string, error) {
	addr := os.Getenv("AGENTMUX_NATMUX_TCP")
	if addr == "" {
		return nil, "", nil
	}
	token, err := loadOrCreateToken(socketPath() + ".token")
	if err != nil {
		return nil, "", err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, "", err
	}
	return ln, token, nil
}
