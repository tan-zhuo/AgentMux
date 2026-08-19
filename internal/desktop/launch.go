package desktop

import (
	"fmt"
	"os/exec"
	"strings"
)

// Client is a desktop viewer on this computer, and the command that starts it.
type Client struct {
	// Name is what to call it when telling somebody which one opened.
	Name string
	// Argv is the command, already pointed at the local end of the forward.
	Argv []string
	// File, when set, is written to a temporary path and appended to Argv. It is
	// how the macOS RDP clients are driven: they have no address argument, but
	// every one of them opens a .rdp file.
	File string
}

// Launch starts this computer's own client for a desktop reachable at addr.
//
// The viewer is not bundled and never installed: the machine has one, or it
// does not and is told which to get. AgentMux's business is the private path to
// the desktop, not a rendering of it — that is the whole reason this layer is
// small enough to work on three operating systems at once.
func Launch(p Protocol, addr string) (string, error) {
	candidates := clients(p, addr)
	if len(candidates) == 0 {
		return "", fmt.Errorf("no %s client is known for this operating system", p)
	}
	var tried []string
	for _, c := range candidates {
		bin := c.Argv[0]
		path, err := exec.LookPath(bin)
		if err != nil {
			tried = append(tried, bin)
			continue
		}
		argv := append([]string(nil), c.Argv[1:]...)
		if c.File != "" {
			written, err := writeTempFile(c.File)
			if err != nil {
				return "", err
			}
			argv = append(argv, written)
		}
		cmd := exec.Command(path, argv...)
		hideWindow(cmd)
		if err := cmd.Start(); err != nil {
			return "", fmt.Errorf("%s: %w", c.Name, err)
		}
		// The viewer belongs to itself from here: it outlives this call, and
		// AgentMux is not its parent process for any purpose but reaping.
		go func() { _ = cmd.Wait() }()
		return c.Name, nil
	}
	return "", fmt.Errorf("no desktop client found for %s — tried %s", p, strings.Join(tried, ", "))
}
