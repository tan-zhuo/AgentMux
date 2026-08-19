package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// rdpFile is the smallest .rdp that points a client at an address and lets it
// ask for the credentials itself. AgentMux does not put a password in here: the
// file is on disk, however briefly, and the desktop's own password is not one
// of the secrets it was given to look after.
func rdpFile(addr string) string {
	return strings.Join([]string{
		"full address:s:" + addr,
		"prompt for credentials:i:1",
		"administrative session:i:0",
		"screen mode id:i:2",
	}, "\r\n") + "\r\n"
}

// writeTempFile puts a client's configuration somewhere it can be opened from,
// readable only by its owner, and named so a person who finds it later knows
// what left it there.
func writeTempFile(content string) (string, error) {
	f, err := os.CreateTemp("", "agentmux-*.rdp")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return "", err
	}
	if _, err := f.WriteString(content); err != nil {
		return "", fmt.Errorf("writing %s: %w", filepath.Base(f.Name()), err)
	}
	return f.Name(), nil
}
