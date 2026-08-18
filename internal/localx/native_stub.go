//go:build !windows

package localx

import (
	"errors"
	"os/exec"

	"agentmux/internal/sshx"
)

// On anything that is not Windows there is no "native Windows side" of this
// machine: KindLocal already is the machine itself. These stubs exist so the
// wiring above compiles everywhere and answers with a sentence when asked.

var errNotWindows = errors.New(
	"a native Windows host only exists on Windows; on this platform, this computer is already managed directly")

func nativeAvailable() error { return errNotWindows }

func nativeShellCommand(cmd string) (string, []string) {
	// Never reached: Exec checks Available first. A harmless value keeps the
	// compiler and any future caller honest.
	return "/bin/sh", []string{"-c", cmd}
}

func hideConsole(*exec.Cmd) {}

func nativeHome() (string, error) { return "", errNotWindows }

func nativeHostPath(string) (string, error) { return "", errNotWindows }

func nativePathOf(p string) string { return p }

// NativeTerminals is the stub of the Windows terminal opener.
type NativeTerminals struct{ run *NativeRunner }

// NewNativeTerminals builds the stub opener.
func NewNativeTerminals(run *NativeRunner) *NativeTerminals { return &NativeTerminals{run: run} }

// OpenTerminal refuses: there is no native Windows side here.
func (t *NativeTerminals) OpenTerminal(sshx.ShellOptions) (sshx.Opened, error) {
	return sshx.Opened{}, errNotWindows
}
