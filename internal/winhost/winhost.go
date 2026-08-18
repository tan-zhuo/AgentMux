// Package winhost reaches the natmux session daemon on a remote Windows
// machine, over the same SSH connection everything else uses.
//
// The problem it solves is transport, not protocol. A remote Windows host has
// no tmux, so its sessions live in the same daemon that serves the native
// local host — but an SSH exec channel on Windows runs through whatever the
// operator set as the default shell, cmd.exe or PowerShell, and PowerShell
// re-encodes the byte streams of anything it parents. So the protocol never
// crosses a shell at all: the daemon listens on a loopback TCP port and this
// package dials it through an SSH direct-tcpip forward, which sshd serves
// itself, byte for byte. One-shot commands — probing, reading the auth token,
// starting the daemon — go as `powershell.exe -EncodedCommand <base64>`, a
// spelling both default shells pass through untouched.
//
// The daemon binary is deployed on first use: uploaded over SFTP into the
// user's %LOCALAPPDATA%\AgentMux, then started through WMI process creation,
// which is what detaches it from the SSH session's job — processes left inside
// that job die with the connection, and a session broker that dies with the
// connection would be worse than none.
package winhost

import (
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"agentmux/internal/natmux"
	"agentmux/internal/sshx"

	"github.com/pkg/sftp"
)

// remoteExeRel is where the daemon lives under %LOCALAPPDATA%, in both spellings.
const (
	remoteDirPS = `$env:LOCALAPPDATA\AgentMux`
	remoteExePS = remoteDirPS + `\agentmux-host.exe`
)

// Transport carries the natmux protocol to one remote Windows host. It
// satisfies natmux.Transport.
type Transport struct {
	ServerID string
	Pool     *sshx.Pool
	// Username is the account SSH logs into, which is the account the daemon
	// runs as and therefore what its loopback port is derived from.
	Username func() (string, error)
	// LocalExe locates the Windows AgentMux binary to deploy.
	LocalExe func() (string, error)

	mu         sync.Mutex
	port       int
	token      string
	tokenAt    time.Time
	ensuring   sync.Mutex
	deployedAt time.Time
}

// tcpPort resolves — once — the loopback port the remote daemon serves.
func (t *Transport) tcpPort() (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.port != 0 {
		return t.port, nil
	}
	user, err := t.Username()
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(user) == "" {
		return 0, fmt.Errorf("the host has no username to derive the session daemon's port from")
	}
	t.port = natmux.TCPPort(user)
	return t.port, nil
}

// Dial opens one protocol connection: an SSH channel forwarded to the daemon's
// loopback port. The release returns the SSH lease once the connection is done.
func (t *Transport) Dial() (io.ReadWriteCloser, func(), error) {
	port, err := t.tcpPort()
	if err != nil {
		return nil, nil, err
	}
	lease, err := t.Pool.Acquire(t.ServerID)
	if err != nil {
		return nil, nil, err
	}
	conn, err := lease.Client.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		lease.Release()
		return nil, nil, err
	}
	return conn, lease.Release, nil
}

// exec runs one PowerShell script on the host, in the encoded spelling that
// survives either default shell.
func (t *Transport) exec(script string) (sshx.ExecResult, error) {
	return t.Pool.Exec(t.ServerID, sshx.PowerShellCommand(script))
}

// Token reads the daemon's auth token from the host, as the SSH account —
// which is what makes presenting it prove anything. Cached briefly: one extra
// exec per session operation would double every round trip.
func (t *Transport) Token() (string, error) {
	t.mu.Lock()
	if t.token != "" && time.Since(t.tokenAt) < 10*time.Minute {
		tok := t.token
		t.mu.Unlock()
		return tok, nil
	}
	t.mu.Unlock()

	res, err := t.exec(`Get-Content -Raw "` + remoteDirPS + `\natmux.token"`)
	if err != nil {
		return "", fmt.Errorf("read the session daemon's token: %w", err)
	}
	tok := strings.TrimSpace(res.Stdout)
	if tok == "" {
		return "", fmt.Errorf("the session daemon has no token yet on this host: %s", strings.TrimSpace(res.Stderr))
	}
	t.mu.Lock()
	t.token, t.tokenAt = tok, time.Now()
	t.mu.Unlock()
	return tok, nil
}

// Ensure makes the daemon reachable: deploy the binary if the host has none,
// start it detached from this SSH session, and wait for its port to answer.
func (t *Transport) Ensure() error {
	t.ensuring.Lock()
	defer t.ensuring.Unlock()

	// Someone else may have finished the job while we waited on the lock.
	if conn, release, err := t.Dial(); err == nil {
		_ = conn.Close()
		release()
		return nil
	}

	res, err := t.exec(`if (Test-Path "` + remoteExePS + `") { 'yes' } else { 'no' }`)
	if err != nil {
		return fmt.Errorf("probe the host for the session daemon: %w", err)
	}
	if !strings.Contains(res.Stdout, "yes") {
		if err := t.deploy(); err != nil {
			return err
		}
	}

	// WMI creates the process outside this SSH session's job object. A plain
	// exec would work until the connection dropped, and then take every native
	// session down with it — precisely what the daemon exists to prevent.
	start := `$r = Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{ CommandLine = ('"{0}" --natmuxd' -f "` + remoteExePS + `") }; $r.ReturnValue`
	res, err = t.exec(start)
	if err != nil {
		return fmt.Errorf("start the session daemon: %w", err)
	}
	if code := strings.TrimSpace(res.Stdout); code != "0" {
		return fmt.Errorf("starting the session daemon failed (Win32_Process.Create returned %s)", code)
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(300 * time.Millisecond)
		if conn, release, err := t.Dial(); err == nil {
			_ = conn.Close()
			release()
			// A fresh daemon may have minted a fresh token; drop the cache.
			t.mu.Lock()
			t.token = ""
			t.mu.Unlock()
			return nil
		}
	}
	return fmt.Errorf("the session daemon did not come up on this host; check %%LOCALAPPDATA%%\\AgentMux\\natmux.log there")
}

// deploy uploads the Windows AgentMux binary over SFTP.
func (t *Transport) deploy() error {
	src, err := t.LocalExe()
	if err != nil {
		return err
	}
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open the Windows build to deploy: %w", err)
	}
	defer f.Close()

	// The absolute remote directory, asked of the host because %LOCALAPPDATA%
	// is not something SFTP expands.
	res, err := t.exec(`$env:LOCALAPPDATA`)
	if err != nil {
		return fmt.Errorf("resolve the host's application data directory: %w", err)
	}
	base := strings.TrimSpace(res.Stdout)
	if base == "" {
		return fmt.Errorf("the host reported no LOCALAPPDATA directory")
	}
	dir := path.Join(strings.ReplaceAll(base, `\`, "/"), "AgentMux")

	lease, err := t.Pool.Acquire(t.ServerID)
	if err != nil {
		return err
	}
	defer lease.Release()
	cl, err := sftp.NewClient(lease.Client)
	if err != nil {
		return fmt.Errorf("open SFTP to deploy the session daemon: %w", err)
	}
	defer cl.Close()

	if err := cl.MkdirAll(dir); err != nil {
		return fmt.Errorf("create %s on the host: %w", dir, err)
	}
	target := path.Join(dir, "agentmux-host.exe")
	tmp := target + ".uploading"
	w, err := cl.Create(tmp)
	if err != nil {
		return fmt.Errorf("create %s on the host: %w", tmp, err)
	}
	if _, err := io.Copy(w, f); err != nil {
		_ = w.Close()
		_ = cl.Remove(tmp)
		return fmt.Errorf("upload the session daemon: %w", err)
	}
	if err := w.Close(); err != nil {
		_ = cl.Remove(tmp)
		return fmt.Errorf("finish uploading the session daemon: %w", err)
	}
	// Replace, not rename-over: Windows refuses to rename onto an existing file.
	_ = cl.Remove(target)
	if err := cl.Rename(tmp, target); err != nil {
		_ = cl.Remove(tmp)
		return fmt.Errorf("install the session daemon: %w", err)
	}
	t.mu.Lock()
	t.deployedAt = time.Now()
	t.mu.Unlock()
	return nil
}
