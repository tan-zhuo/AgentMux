//go:build windows

package sshx

import (
	"net"
	"os"
	"time"

	"github.com/Microsoft/go-winio"
)

const openSSHAgentPipe = `\\.\pipe\openssh-ssh-agent`

// dialAgent connects to the Windows OpenSSH agent named pipe, falling back to
// SSH_AUTH_SOCK for setups that proxy an AF_UNIX socket (WSL, Git Bash).
func dialAgent() (net.Conn, error) {
	timeout := 3 * time.Second
	conn, err := winio.DialPipe(openSSHAgentPipe, &timeout)
	if err == nil {
		return conn, nil
	}
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if c, uerr := net.DialTimeout("unix", sock, timeout); uerr == nil {
			return c, nil
		}
	}
	return nil, err
}
