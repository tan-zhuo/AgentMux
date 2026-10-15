package app

import (
	"errors"
	"fmt"
	"strings"

	"agentmux/internal/localx"
	"agentmux/internal/sftpx"
	"agentmux/internal/sshx"
	"agentmux/internal/store"
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

// SearchHit is one path a search found.
type SearchHit struct {
	Path string `json:"path"`
	Dir  bool   `json:"dir"`
}

// SearchResult is everything a search brought back. Truncated says the caps
// were hit: there were more matches than anyone scrolls through, and a more
// specific query is the answer, not a longer list.
type SearchResult struct {
	Hits      []SearchHit `json:"hits"`
	Truncated bool        `json:"truncated"`
}

// How deep and how many: deep enough to find a folder three projects down,
// bounded enough that a home directory with a million files answers fast.
const (
	searchDepth    = 6
	searchCapDirs  = 100
	searchCapFiles = 100
)

// searchMarker separates the two halves of the POSIX search output.
const searchMarker = "__AGENTMUX_SEARCH_FILES__"

// Search finds files and folders under dir whose names contain query,
// case-insensitively. It runs where the files are — find over SSH or a local
// shell, Get-ChildItem on the Windows hosts — because walking a remote tree
// over SFTP is a round trip per directory and a spinner per round trip.
// Directories come first: locating a folder is what the search box is for.
func (f *FileService) Search(serverID, dir, query string) (SearchResult, error) {
	if serverID == "" || strings.TrimSpace(dir) == "" {
		return SearchResult{}, errors.New("serverId and dir are required")
	}
	q := sanitizeQuery(query)
	if q == "" {
		return SearchResult{}, errors.New("nothing searchable in the query")
	}

	kind := f.core.Store.ServerKindOf(serverID)
	if kind == store.KindSSHWin || kind == store.KindLocalWin {
		return f.searchWindows(serverID, dir, q)
	}
	return f.searchPosix(serverID, dir, q)
}

// sanitizeQuery keeps the characters file names are actually made of and
// drops everything a shell or a glob could read as its own: quoting is the
// first defence, this is the second, and neither trusts the other.
func sanitizeQuery(q string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(q) {
		switch {
		case r < 0x20 || r == 0x7f:
		case strings.ContainsRune("`'\"\\$*?[]{}()<>|&;!#~", r):
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func (f *FileService) searchPosix(serverID, dir, q string) (SearchResult, error) {
	qdir := sshx.ShellQuote(dir)
	qpat := sshx.ShellQuote("*" + q + "*")
	half := func(typ string, cap int) string {
		// A bounded walk, twice over: head caps the matches, timeout caps the
		// walk itself — a home directory dragging a million-file cache can
		// otherwise keep a phone's spinner going for a minute. What was found
		// before the cut still comes back. Hosts without a timeout binary
		// (rare) just take as long as they take.
		find := fmt.Sprintf(
			`find %s -maxdepth %d \( -name .git -o -name node_modules \) -prune -o -type %s -iname %s -print 2>/dev/null`,
			qdir, searchDepth, typ, qpat)
		return fmt.Sprintf(
			`{ if command -v timeout >/dev/null 2>&1; then timeout 15 %s; else %s; fi; } | head -n %d`,
			find, find, cap+1)
	}
	cmd := fmt.Sprintf(`%s; echo %s; %s`,
		half("d", searchCapDirs), searchMarker, half("f", searchCapFiles))

	res, err := f.core.Run.Exec(serverID, cmd)
	if err != nil {
		return SearchResult{}, err
	}

	var out SearchResult
	dirs, files, inFiles := 0, 0, false
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == searchMarker {
			inFiles = true
			continue
		}
		if inFiles {
			if files++; files > searchCapFiles {
				out.Truncated = true
				continue
			}
		} else {
			if dirs++; dirs > searchCapDirs {
				out.Truncated = true
				continue
			}
		}
		out.Hits = append(out.Hits, SearchHit{Path: line, Dir: !inFiles})
	}
	return out, nil
}

func (f *FileService) searchWindows(serverID, dir, q string) (SearchResult, error) {
	psq := func(s string) string { return strings.ReplaceAll(s, "'", "''") }
	cap := searchCapDirs + searchCapFiles
	cmd := fmt.Sprintf(
		`$ErrorActionPreference='SilentlyContinue'; Get-ChildItem -LiteralPath '%s' -Recurse -Depth %d | Where-Object { $_.Name -like '*%s*' } | Select-Object -First %d | ForEach-Object { $(if ($_.PSIsContainer) {'d'} else {'f'}) + '|' + $_.FullName }`,
		psq(dir), searchDepth-1, psq(q), cap+1)

	res, err := f.core.Run.Exec(serverID, cmd)
	if err != nil {
		return SearchResult{}, err
	}

	var dirsFound, filesFound []SearchHit
	n := 0
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		kind, path, ok := strings.Cut(line, "|")
		if !ok || path == "" {
			continue
		}
		if n++; n > cap {
			continue
		}
		hit := SearchHit{Path: strings.ReplaceAll(path, `\`, "/"), Dir: kind == "d"}
		if hit.Dir {
			dirsFound = append(dirsFound, hit)
		} else {
			filesFound = append(filesFound, hit)
		}
	}
	return SearchResult{Hits: append(dirsFound, filesFound...), Truncated: n > cap}, nil
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
