//go:build !windows

package sshx

import (
	"errors"
	"net"
	"os"
	"time"
)

// dialAgent connects to the agent socket advertised by SSH_AUTH_SOCK.
func dialAgent() (net.Conn, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, errors.New("SSH_AUTH_SOCK is not set")
	}
	return net.DialTimeout("unix", sock, 3*time.Second)
}
