//go:build !windows

package natmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The address a temporary directory produces on macOS is long enough to pass
// what a unix socket may be called, which is how a daemon that works
// everywhere else fails to start there. Forced here so it is checked on every
// platform rather than discovered on one.
func TestADaemonStartsAtAnAddressTooLongToBind(t *testing.T) {
	deep := filepath.Join(t.TempDir(), strings.Repeat("d", 60), strings.Repeat("e", 60))
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	asked := filepath.Join(deep, "natmux.sock")
	if len(asked) <= maxUnixPath {
		t.Fatalf("the test did not build a long enough address (%d bytes)", len(asked))
	}
	t.Setenv("AGENTMUX_NATMUX_SOCKET", asked)
	go DaemonMain()

	waitForDaemon(t)

	// The point is not only that it started, but that the client and the daemon
	// agreed on where without either being told: both shorten the same address
	// the same way.
	if got := socketPath(); got == asked {
		t.Fatal("the address was used as it was, which cannot have bound")
	} else if len(got) > maxUnixPath {
		t.Fatalf("the shortened address is still too long: %s", got)
	}
	if socketPath() != bindable(asked) {
		t.Fatal("two calls produced two addresses; nothing would ever meet")
	}
}

func TestAnAddressShortEnoughToBindIsLeftAlone(t *testing.T) {
	// A literal rather than a temporary directory: the point is an address that
	// fits, and on some machines a temporary directory is exactly what does not.
	const asked = "/tmp/agentmux-natmux-short.sock"
	if got := bindable(asked); got != asked {
		t.Fatalf("a usable address was rewritten: %s", got)
	}
}
