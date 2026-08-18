//go:build windows

package natmux

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
)

// pipePath is the daemon's address: one named pipe per user, so two accounts on
// the same machine get two brokers and neither can see the other's sessions.
func pipePath() string {
	user := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, os.Getenv("USERNAME"))
	if user == "" {
		user = "default"
	}
	return `\\.\pipe\agentmux-natmux-` + user
}

func addrLabel() string { return pipePath() }

func listen() (net.Listener, error) {
	// The pipe carries keystrokes into shells. Restrict it to the account that
	// created it (plus SYSTEM), or any local process could type into an agent.
	sddl := "D:P(A;;GA;;;SY)"
	if sid, err := currentUserSID(); err == nil {
		sddl += "(A;;GA;;;" + sid + ")"
	}
	ln, err := winio.ListenPipe(pipePath(), &winio.PipeConfig{SecurityDescriptor: sddl})
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", pipePath(), err)
	}
	return ln, nil
}

func dial(timeout time.Duration) (net.Conn, error) {
	return winio.DialPipe(pipePath(), &timeout)
}

func currentUserSID() (string, error) {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		return "", err
	}
	defer token.Close()
	u, err := token.GetTokenUser()
	if err != nil {
		return "", err
	}
	return u.User.Sid.String(), nil
}

// daemonLogPath puts the daemon's log next to the application's data, where a
// person debugging "my session vanished" will actually find it.
func daemonLogPath() string {
	if base := os.Getenv("LOCALAPPDATA"); base != "" {
		dir := filepath.Join(base, "AgentMux")
		if err := os.MkdirAll(dir, 0o700); err == nil {
			return filepath.Join(dir, "natmux.log")
		}
	}
	return filepath.Join(os.TempDir(), "agentmux-natmux.log")
}

// TokenRelPath is where the TCP token lives, relative to %LOCALAPPDATA%. A
// remote client reads it through the same SSH account the daemon runs as, which
// is what makes presenting it prove anything.
const TokenRelPath = `AgentMux\natmux.token`

func tokenPath() string {
	if base := os.Getenv("LOCALAPPDATA"); base != "" {
		return filepath.Join(base, TokenRelPath)
	}
	return filepath.Join(os.TempDir(), "agentmux-natmux.token")
}

// listenTCP serves the loopback port an SSH port forward reaches: it is how a
// remote AgentMux drives this machine's sessions. Always on on Windows — this
// is the platform whose remote story runs through it — and guarded by the token
// because localhost TCP, unlike the pipe, is visible to every local account.
func listenTCP() (net.Listener, string, error) {
	token, err := loadOrCreateToken(tokenPath())
	if err != nil {
		return nil, "", err
	}
	user := os.Getenv("USERNAME")
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", TCPPort(user)))
	if err != nil {
		return nil, "", err
	}
	return ln, token, nil
}
