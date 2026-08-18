package app

import (
	"errors"
	"strings"

	"agentmux/internal/localx"
	"agentmux/internal/sftpx"
)

// FileService exposes a host's file system to the frontend.
type FileService struct{ core *Core }

// NewFileService binds a file service to the core.
func NewFileService(c *Core) *FileService { return &FileService{core: c} }

// ServiceName identifies the service in Wails logs.
func (f *FileService) ServiceName() string { return "FileService" }

// files is the transport for one host: SFTP over its connection, or ordinary file
// operations when the host is this computer. Both return the same shapes, so the
// browser and the editor above cannot tell which one answered.
type files interface {
	Home(serverID string) (string, error)
	List(serverID, dir string) (sftpx.Listing, error)
	Mkdir(serverID, dir string) error
	Rename(serverID, from, to string) error
	Remove(serverID, target string, recursive bool) error
	ReadFile(serverID, target string) (sftpx.FileContent, error)
	WriteFile(serverID, target, content string, expectedModTime int64, crlf bool) (sftpx.FileContent, error)
}

func (f *FileService) on(serverID string) files {
	if f.core.IsLocal(serverID) {
		return f.core.LocalFiles
	}
	if f.core.IsLocalWin(serverID) {
		return f.core.NativeFiles
	}
	return f.core.Files
}

// List reads a directory. An empty path opens the user's home.
func (f *FileService) List(serverID, dir string) (sftpx.Listing, error) {
	if serverID == "" {
		return sftpx.Listing{}, errors.New("serverId is required")
	}
	return f.on(serverID).List(serverID, dir)
}

// Home returns the login user's home directory.
func (f *FileService) Home(serverID string) (string, error) {
	return f.on(serverID).Home(serverID)
}

// ListWorkspace opens the directory a workspace points at, which is the reason
// most people open the file browser in the first place.
func (f *FileService) ListWorkspace(workspaceID string) (sftpx.Listing, error) {
	ws, err := f.core.Store.GetWorkspace(workspaceID)
	if err != nil {
		return sftpx.Listing{}, err
	}
	return f.on(ws.ServerID).List(ws.ServerID, ws.RemotePath)
}

// Mkdir creates a directory.
func (f *FileService) Mkdir(serverID, dir string) error {
	if strings.TrimSpace(dir) == "" {
		return errors.New("directory name is required")
	}
	return f.on(serverID).Mkdir(serverID, dir)
}

// Rename moves or renames a path.
func (f *FileService) Rename(serverID, from, to string) error {
	return f.on(serverID).Rename(serverID, from, to)
}

// Remove deletes a path. Directories need recursive set explicitly, so a
// mis-click cannot take a tree with it.
func (f *FileService) Remove(serverID, target string, recursive bool) error {
	if strings.TrimSpace(target) == "" || target == "/" || isDriveRoot(target) {
		return errors.New("refusing to delete that path")
	}
	return f.on(serverID).Remove(serverID, target, recursive)
}

// isDriveRoot recognises "C:" and "C:/" — the Windows spellings of "everything".
func isDriveRoot(p string) bool {
	p = strings.TrimSuffix(strings.TrimSpace(p), "/")
	return len(p) == 2 && p[1] == ':'
}

// Read loads a text file for the editor.
func (f *FileService) Read(serverID, remote string) (sftpx.FileContent, error) {
	return f.on(serverID).ReadFile(serverID, remote)
}

// Write saves an edited file. expectedModTime guards against clobbering a
// change someone — or an agent — made since it was opened.
func (f *FileService) Write(
	serverID, remote, content string, expectedModTime int64, crlf bool,
) (sftpx.FileContent, error) {
	return f.on(serverID).WriteFile(serverID, remote, content, expectedModTime, crlf)
}

// Download copies a file from a host to a local path. It returns as soon as the
// transfer starts; progress arrives as transfer:update events.
func (f *FileService) Download(serverID, remote, local string) (sftpx.Transfer, error) {
	if f.core.IsLocalAny(serverID) {
		return sftpx.Transfer{}, localx.ErrNotTransferable
	}
	return f.core.Files.Download(serverID, remote, local)
}

// Upload copies a local file into a directory on a host.
func (f *FileService) Upload(serverID, local, remoteDir string) (sftpx.Transfer, error) {
	if f.core.IsLocalAny(serverID) {
		return sftpx.Transfer{}, localx.ErrNotTransferable
	}
	return f.core.Files.Upload(serverID, local, remoteDir)
}

// Cancel stops a running transfer.
func (f *FileService) Cancel(id string) { f.core.Files.Cancel(id) }

// Transfers lists this session's transfers, newest first.
func (f *FileService) Transfers() []sftpx.Transfer { return f.core.Files.Transfers() }

// ClearFinished drops completed transfers from the list.
func (f *FileService) ClearFinished() { f.core.Files.ClearFinished() }
