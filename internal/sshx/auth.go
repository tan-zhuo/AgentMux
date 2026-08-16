package sshx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// defaultKeyNames are tried in order when AuthKey is selected without an
// explicit key path.
var defaultKeyNames = []string{"id_ed25519", "id_ecdsa", "id_rsa"}

func expandHome(p string) string {
	if p == "" {
		return p
	}
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p[1:], "/"), `\`))
		}
	}
	return p
}

// authMethods builds the ordered list of SSH auth methods for a target.
func authMethods(t Target) ([]ssh.AuthMethod, error) {
	switch t.AuthType {
	case "password":
		if t.Password == "" {
			return nil, errors.New("password auth selected but no password is stored for this server")
		}
		return []ssh.AuthMethod{
			ssh.Password(t.Password),
			// Many sshd configs answer password logins over keyboard-interactive.
			ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = t.Password
				}
				return answers, nil
			}),
		}, nil

	case "key":
		signer, err := loadKey(t)
		if err != nil {
			return nil, err
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil

	default: // "agent"
		conn, err := dialAgent()
		if err != nil {
			return nil, fmt.Errorf("ssh-agent unavailable: %w", err)
		}
		ag := agent.NewClient(conn)
		signers, err := ag.Signers()
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("ssh-agent has no usable keys: %w", err)
		}
		if len(signers) == 0 {
			_ = conn.Close()
			return nil, errors.New("ssh-agent is running but holds no keys (try ssh-add)")
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signers...)}, nil
	}
}

func loadKey(t Target) (ssh.Signer, error) {
	paths := []string{}
	if t.KeyPath != "" {
		paths = append(paths, expandHome(t.KeyPath))
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		for _, n := range defaultKeyNames {
			paths = append(paths, filepath.Join(home, ".ssh", n))
		}
	}

	var lastErr error
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			lastErr = err
			continue
		}
		if t.Passphrase != "" {
			signer, err := ssh.ParsePrivateKeyWithPassphrase(raw, []byte(t.Passphrase))
			if err != nil {
				lastErr = fmt.Errorf("%s: %w", p, err)
				continue
			}
			return signer, nil
		}
		signer, err := ssh.ParsePrivateKey(raw)
		if err != nil {
			var pe *ssh.PassphraseMissingError
			if errors.As(err, &pe) {
				return nil, fmt.Errorf("%s is passphrase-protected — add the passphrase in server settings", p)
			}
			lastErr = fmt.Errorf("%s: %w", p, err)
			continue
		}
		return signer, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no private key found")
	}
	return nil, lastErr
}
