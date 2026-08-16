// Package sftpx is the remote file system: browsing, uploads and downloads over
// the same pooled SSH connections everything else uses.
//
// SFTP clients are cached per server rather than opened per call. Opening the
// subsystem costs a round trip, and a file browser makes a lot of small calls —
// one per directory you click into.
package sftpx

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/sftp"

	"agentmux/internal/sshx"
)

// idleTTL closes an SFTP channel that nothing has used for this long. The
// underlying SSH connection stays in the pool.
const idleTTL = 5 * time.Minute

// Entry is one item in a remote directory.
type Entry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"isDir"`
	IsLink  bool   `json:"isLink"`
	Size    int64  `json:"size"`
	Mode    string `json:"mode"`
	ModTime int64  `json:"modTime"`
	// Target is set for symlinks that could be resolved.
	Target string `json:"target"`
	// TargetIsDir reports whether a resolved symlink points at a directory, so
	// the UI can let you click through it.
	TargetIsDir bool `json:"targetIsDir"`
}

// Listing is a directory and its contents.
type Listing struct {
	ServerID string  `json:"serverId"`
	Path     string  `json:"path"`
	Parent   string  `json:"parent"`
	Entries  []Entry `json:"entries"`
}

// Transfer is the state of one upload or download.
type Transfer struct {
	ID        string `json:"id"`
	ServerID  string `json:"serverId"`
	Kind      string `json:"kind"` // upload | download
	Local     string `json:"local"`
	Remote    string `json:"remote"`
	Size      int64  `json:"size"`
	Done      int64  `json:"done"`
	Status    string `json:"status"` // running | done | error | cancelled
	Error     string `json:"error"`
	StartedAt int64  `json:"startedAt"`
}

type session struct {
	client   *sftp.Client
	lease    *sshx.Lease
	lastUsed time.Time
}

// Client owns the per-server SFTP sessions and the transfer registry.
type Client struct {
	pool *sshx.Pool
	emit func(name string, data any)

	mu       sync.Mutex
	sessions map[string]*session

	tmu       sync.Mutex
	transfers map[string]*Transfer
	cancels   map[string]chan struct{}

	stopOnce sync.Once
	stopCh   chan struct{}
}

// New builds an SFTP client on top of the SSH pool.
func New(pool *sshx.Pool, emit func(name string, data any)) *Client {
	c := &Client{
		pool:      pool,
		emit:      emit,
		sessions:  map[string]*session{},
		transfers: map[string]*Transfer{},
		cancels:   map[string]chan struct{}{},
		stopCh:    make(chan struct{}),
	}
	go c.reap()
	return c
}

// Close tears down every cached SFTP session.
func (c *Client) Close() {
	c.stopOnce.Do(func() {
		close(c.stopCh)
		c.mu.Lock()
		defer c.mu.Unlock()
		for id, s := range c.sessions {
			_ = s.client.Close()
			s.lease.Release()
			delete(c.sessions, id)
		}
	})
}

func (c *Client) reap() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-t.C:
			c.mu.Lock()
			for id, s := range c.sessions {
				if time.Since(s.lastUsed) > idleTTL {
					_ = s.client.Close()
					s.lease.Release()
					delete(c.sessions, id)
				}
			}
			c.mu.Unlock()
		}
	}
}

// conn returns a cached SFTP client, opening one if needed.
func (c *Client) conn(serverID string) (*sftp.Client, error) {
	c.mu.Lock()
	if s, ok := c.sessions[serverID]; ok {
		// Cheap liveness probe: a dead channel fails here rather than halfway
		// through a directory listing.
		if _, err := s.client.Getwd(); err == nil {
			s.lastUsed = time.Now()
			cl := s.client
			c.mu.Unlock()
			return cl, nil
		}
		_ = s.client.Close()
		s.lease.Release()
		delete(c.sessions, serverID)
	}
	c.mu.Unlock()

	lease, err := c.pool.Acquire(serverID)
	if err != nil {
		return nil, err
	}
	cl, err := sftp.NewClient(lease.Client, sftp.UseConcurrentReads(true), sftp.UseConcurrentWrites(true))
	if err != nil {
		lease.Release()
		// "subsystem request failed" is what sshd says when the SFTP subsystem
		// is not enabled, which reads as a bug in this app unless we say so.
		if strings.Contains(err.Error(), "subsystem request failed") {
			return nil, fmt.Errorf(
				"this server does not offer SFTP: sshd has no 'Subsystem sftp' line, " +
					"so file browsing and transfers are unavailable here. " +
					"Terminals and agents still work")
		}
		return nil, fmt.Errorf("open sftp: %w", err)
	}

	c.mu.Lock()
	c.sessions[serverID] = &session{client: cl, lease: lease, lastUsed: time.Now()}
	c.mu.Unlock()
	return cl, nil
}

// Home returns the login user's home directory.
func (c *Client) Home(serverID string) (string, error) {
	cl, err := c.conn(serverID)
	if err != nil {
		return "", err
	}
	wd, err := cl.Getwd()
	if err != nil || wd == "" {
		return "/", nil //nolint:nilerr // an unusable cwd is not fatal, root always works
	}
	return wd, nil
}

// List reads a directory. An empty path means the user's home.
func (c *Client) List(serverID, dir string) (Listing, error) {
	cl, err := c.conn(serverID)
	if err != nil {
		return Listing{}, err
	}
	if strings.TrimSpace(dir) == "" || dir == "~" {
		if dir, err = c.Home(serverID); err != nil {
			return Listing{}, err
		}
	}
	dir = path.Clean(dir)

	infos, err := cl.ReadDir(dir)
	if err != nil {
		return Listing{}, fmt.Errorf("%s: %w", dir, err)
	}

	entries := make([]Entry, 0, len(infos))
	for _, fi := range infos {
		full := path.Join(dir, fi.Name())
		e := Entry{
			Name:    fi.Name(),
			Path:    full,
			IsDir:   fi.IsDir(),
			Size:    fi.Size(),
			Mode:    fi.Mode().String(),
			ModTime: fi.ModTime().Unix(),
			IsLink:  fi.Mode()&os.ModeSymlink != 0,
		}
		if e.IsLink {
			if target, lerr := cl.ReadLink(full); lerr == nil {
				e.Target = target
				// Stat follows the link; a broken link simply stays a file.
				if st, serr := cl.Stat(full); serr == nil {
					e.TargetIsDir = st.IsDir()
				}
			}
		}
		entries = append(entries, e)
	}

	// Directories first, then case-insensitive by name — the order people
	// expect from a file manager.
	sort.SliceStable(entries, func(i, j int) bool {
		di := entries[i].IsDir || entries[i].TargetIsDir
		dj := entries[j].IsDir || entries[j].TargetIsDir
		if di != dj {
			return di
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	parent := path.Dir(dir)
	if parent == dir {
		parent = ""
	}
	return Listing{ServerID: serverID, Path: dir, Parent: parent, Entries: entries}, nil
}

// Mkdir creates a directory.
func (c *Client) Mkdir(serverID, dir string) error {
	cl, err := c.conn(serverID)
	if err != nil {
		return err
	}
	return cl.MkdirAll(dir)
}

// Rename moves a file or directory.
func (c *Client) Rename(serverID, from, to string) error {
	cl, err := c.conn(serverID)
	if err != nil {
		return err
	}
	return cl.Rename(from, to)
}

// Remove deletes a file, or a directory and everything under it.
func (c *Client) Remove(serverID, target string, recursive bool) error {
	cl, err := c.conn(serverID)
	if err != nil {
		return err
	}
	if !recursive {
		return cl.Remove(target)
	}
	return cl.RemoveAll(target)
}

// --- transfers --------------------------------------------------------------

func (c *Client) newTransfer(serverID, kind, local, remote string, size int64) (*Transfer, chan struct{}) {
	t := &Transfer{
		ID:        uuid.NewString(),
		ServerID:  serverID,
		Kind:      kind,
		Local:     local,
		Remote:    remote,
		Size:      size,
		Status:    "running",
		StartedAt: time.Now().Unix(),
	}
	cancel := make(chan struct{})
	c.tmu.Lock()
	c.transfers[t.ID] = t
	c.cancels[t.ID] = cancel
	c.tmu.Unlock()
	c.publish(t)
	return t, cancel
}

func (c *Client) publish(t *Transfer) {
	if c.emit == nil {
		return
	}
	c.tmu.Lock()
	snapshot := *t
	c.tmu.Unlock()
	c.emit("transfer:update", snapshot)
}

func (c *Client) finish(t *Transfer, err error) {
	c.tmu.Lock()
	switch {
	case err == nil:
		t.Status = "done"
		t.Done = t.Size
	case errors.Is(err, errCancelled):
		t.Status = "cancelled"
	default:
		t.Status = "error"
		t.Error = err.Error()
	}
	delete(c.cancels, t.ID)
	c.tmu.Unlock()
	c.publish(t)
}

var errCancelled = errors.New("cancelled")

// Cancel stops a running transfer.
func (c *Client) Cancel(id string) {
	c.tmu.Lock()
	ch, ok := c.cancels[id]
	if ok {
		delete(c.cancels, id)
		close(ch)
	}
	c.tmu.Unlock()
}

// Transfers returns every transfer this session has seen.
func (c *Client) Transfers() []Transfer {
	c.tmu.Lock()
	defer c.tmu.Unlock()
	out := make([]Transfer, 0, len(c.transfers))
	for _, t := range c.transfers {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	return out
}

// ClearFinished drops completed transfers from the list.
func (c *Client) ClearFinished() {
	c.tmu.Lock()
	defer c.tmu.Unlock()
	for id, t := range c.transfers {
		if t.Status != "running" {
			delete(c.transfers, id)
		}
	}
}

// Download copies a remote file to a local path, reporting progress as it goes.
func (c *Client) Download(serverID, remote, local string) (Transfer, error) {
	cl, err := c.conn(serverID)
	if err != nil {
		return Transfer{}, err
	}
	st, err := cl.Stat(remote)
	if err != nil {
		return Transfer{}, fmt.Errorf("%s: %w", remote, err)
	}
	if st.IsDir() {
		return Transfer{}, fmt.Errorf("%s is a directory; downloading whole directories is not supported yet", remote)
	}

	t, cancel := c.newTransfer(serverID, "download", local, remote, st.Size())
	go func() {
		src, err := cl.Open(remote)
		if err != nil {
			c.finish(t, err)
			return
		}
		defer src.Close()

		dst, err := os.Create(local)
		if err != nil {
			c.finish(t, err)
			return
		}
		err = c.pump(t, dst, src, cancel)
		cerr := dst.Close()
		if err == nil {
			err = cerr
		}
		if err != nil {
			// A partial file is worse than none: it looks like a successful
			// download until you open it.
			_ = os.Remove(local)
		}
		c.finish(t, err)
	}()
	return *t, nil
}

// Upload copies a local file to a remote path.
func (c *Client) Upload(serverID, local, remoteDir string) (Transfer, error) {
	cl, err := c.conn(serverID)
	if err != nil {
		return Transfer{}, err
	}
	fi, err := os.Stat(local)
	if err != nil {
		return Transfer{}, err
	}
	if fi.IsDir() {
		return Transfer{}, fmt.Errorf("%s is a directory; uploading whole directories is not supported yet", local)
	}
	remote := path.Join(remoteDir, filepathBase(local))

	t, cancel := c.newTransfer(serverID, "upload", local, remote, fi.Size())
	go func() {
		src, err := os.Open(local)
		if err != nil {
			c.finish(t, err)
			return
		}
		defer src.Close()

		dst, err := cl.Create(remote)
		if err != nil {
			c.finish(t, err)
			return
		}
		err = c.pump(t, dst, src, cancel)
		cerr := dst.Close()
		if err == nil {
			err = cerr
		}
		if err != nil {
			_ = cl.Remove(remote)
		}
		c.finish(t, err)
	}()
	return *t, nil
}

// pump copies with cancellation and throttled progress reporting.
func (c *Client) pump(t *Transfer, dst io.Writer, src io.Reader, cancel <-chan struct{}) error {
	buf := make([]byte, 256*1024)
	lastReport := time.Now()
	for {
		select {
		case <-cancel:
			return errCancelled
		case <-c.stopCh:
			return errCancelled
		default:
		}

		n, rerr := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return werr
			}
			c.tmu.Lock()
			t.Done += int64(n)
			c.tmu.Unlock()
			// A progress event per 256 KB chunk would flood the UI on a fast
			// link; ten a second is plenty to animate a bar.
			if time.Since(lastReport) > 100*time.Millisecond {
				lastReport = time.Now()
				c.publish(t)
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

// filepathBase returns the last element of a local path, tolerating both
// separators because the UI may hand us either.
func filepathBase(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	return path.Base(p)
}
