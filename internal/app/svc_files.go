package app

import (
	"errors"
	"strings"

	"agentmux/internal/sftpx"
)

// FileService exposes the remote file system to the frontend.
type FileService struct{ core *Core }

// NewFileService binds a file service to the core.
func NewFileService(c *Core) *FileService { return &FileService{core: c} }

// ServiceName identifies the service in Wails logs.
func (f *FileService) ServiceName() string { return "FileService" }

// List reads a remote directory. An empty path opens the user's home.
func (f *FileService) List(serverID, dir string) (sftpx.Listing, error) {
	if serverID == "" {
		return sftpx.Listing{}, errors.New("serverId is required")
	}
	return f.core.Files.List(serverID, dir)
}

// Home returns the login user's home directory.
func (f *FileService) Home(serverID string) (string, error) {
	return f.core.Files.Home(serverID)
}

// ListWorkspace opens the directory a workspace points at, which is the reason
// most people open the file browser in the first place.
func (f *FileService) ListWorkspace(workspaceID string) (sftpx.Listing, error) {
	ws, err := f.core.Store.GetWorkspace(workspaceID)
	if err != nil {
		return sftpx.Listing{}, err
	}
	return f.core.Files.List(ws.ServerID, ws.RemotePath)
}

// Mkdir creates a directory.
func (f *FileService) Mkdir(serverID, dir string) error {
	if strings.TrimSpace(dir) == "" {
		return errors.New("directory name is required")
	}
	return f.core.Files.Mkdir(serverID, dir)
}

// Rename moves or renames a remote path.
func (f *FileService) Rename(serverID, from, to string) error {
	return f.core.Files.Rename(serverID, from, to)
}

// Remove deletes a remote path. Directories need recursive set explicitly, so
// a mis-click cannot take a tree with it.
func (f *FileService) Remove(serverID, target string, recursive bool) error {
	if strings.TrimSpace(target) == "" || target == "/" {
		return errors.New("refusing to delete that path")
	}
	return f.core.Files.Remove(serverID, target, recursive)
}

// Download copies a remote file to a local path. It returns as soon as the
// transfer starts; progress arrives as transfer:update events.
func (f *FileService) Download(serverID, remote, local string) (sftpx.Transfer, error) {
	return f.core.Files.Download(serverID, remote, local)
}

// Upload copies a local file into a remote directory.
func (f *FileService) Upload(serverID, local, remoteDir string) (sftpx.Transfer, error) {
	return f.core.Files.Upload(serverID, local, remoteDir)
}

// Cancel stops a running transfer.
func (f *FileService) Cancel(id string) { f.core.Files.Cancel(id) }

// Transfers lists this session's transfers, newest first.
func (f *FileService) Transfers() []sftpx.Transfer { return f.core.Files.Transfers() }

// ClearFinished drops completed transfers from the list.
func (f *FileService) ClearFinished() { f.core.Files.ClearFinished() }
