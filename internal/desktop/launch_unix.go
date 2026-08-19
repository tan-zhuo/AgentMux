//go:build !windows && !darwin

package desktop

import "os/exec"

// clients on Linux and the BSDs: nothing is guaranteed to be installed, so the
// viewers people actually have are tried in turn. Remmina first because it
// speaks both protocols and is what the desktops ship; the single-protocol
// viewers after it, for the machines that have one of those instead.
func clients(p Protocol, addr string) []Client {
	switch p {
	case RDP:
		return []Client{
			{Name: "Remmina", Argv: []string{"remmina", "-c", "rdp://" + addr}},
			{Name: "FreeRDP", Argv: []string{"xfreerdp", "/v:" + addr}},
			{Name: "FreeRDP", Argv: []string{"xfreerdp3", "/v:" + addr}},
			{Name: "Vinagre", Argv: []string{"vinagre", "rdp://" + addr}},
		}
	case VNC:
		return []Client{
			{Name: "Remmina", Argv: []string{"remmina", "-c", "vnc://" + addr}},
			{Name: "TigerVNC", Argv: []string{"vncviewer", addr}},
			{Name: "Vinagre", Argv: []string{"vinagre", "vnc://" + addr}},
			{Name: "GNOME Connections", Argv: []string{"gnome-connections", "vnc://" + addr}},
		}
	}
	return nil
}

func hideWindow(*exec.Cmd) {}
