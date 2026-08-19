//go:build darwin

package desktop

import "os/exec"

// clients on macOS: Screen Sharing is part of the system and answers vnc://
// URLs, so VNC needs nothing installed. RDP has no URL scheme and no command
// line worth relying on — every client here opens a .rdp file, so one is
// written and handed to `open`, which routes it to whichever client the person
// installed.
func clients(p Protocol, addr string) []Client {
	switch p {
	case VNC:
		return []Client{{Name: "Screen Sharing", Argv: []string{"open", "vnc://" + addr}}}
	case RDP:
		return []Client{{
			Name: "Windows App",
			Argv: []string{"open"},
			File: rdpFile(addr),
		}}
	}
	return nil
}

func hideWindow(*exec.Cmd) {}
