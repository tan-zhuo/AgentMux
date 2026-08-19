//go:build windows

package desktop

import (
	"os/exec"
	"syscall"
)

// clients on Windows: Remote Desktop Connection ships with the operating
// system, so RDP always has an answer here. VNC does not — Windows has no
// built-in viewer, so the ones people actually install are tried by name and
// the failure says what to get.
func clients(p Protocol, addr string) []Client {
	switch p {
	case RDP:
		return []Client{{Name: "Remote Desktop Connection", Argv: []string{"mstsc.exe", "/v:" + addr}}}
	case VNC:
		return []Client{
			{Name: "TightVNC", Argv: []string{"tvnviewer.exe", addr}},
			{Name: "RealVNC", Argv: []string{"vncviewer.exe", addr}},
			{Name: "UltraVNC", Argv: []string{"vncviewer64.exe", addr}},
		}
	}
	return nil
}

// hideWindow keeps the console a launcher would otherwise flash on screen.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
