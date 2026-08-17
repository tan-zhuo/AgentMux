package llm

// These tests are in-package and feed diagnose synthetic errors, because the
// point of the exercise is that the diagnosis must not depend on what a
// particular kernel, libc or sandbox writes into an error string. Driving it
// through a real socket is what let a CI runner disagree with a developer's
// machine about which branch was taken.

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestDiagnoseNamesTheRightCause(t *testing.T) {
	c := New("http://127.0.0.1:11434")

	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			"refused, as the net package reports it",
			&net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED},
			"ollama serve",
		},
		{
			"refused, wrapped by the http client",
			&url.Error{Op: "Get", URL: "http://127.0.0.1:11434/api/version",
				Err: &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}},
			"ollama serve",
		},
		{
			"the host does not resolve",
			&net.DNSError{Err: "no such host", Name: "ollama.invalid", IsNotFound: true},
			"does not resolve",
		},
		{
			"the context ran out",
			context.DeadlineExceeded,
			"did not answer in time",
		},
		{
			"the socket deadline ran out",
			os.ErrDeadlineExceeded,
			"did not answer in time",
		},
		{
			"a net.Error that reports itself as a timeout",
			&net.OpError{Op: "read", Err: os.ErrDeadlineExceeded},
			"did not answer in time",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := c.diagnose(tc.err)
			if !strings.Contains(got, tc.want) {
				t.Errorf("diagnose(%v) = %q, want it to mention %q", tc.err, got, tc.want)
			}
		})
	}
}

// TestDiagnoseFallbackIsStillActionable is the regression test for the failure
// this replaced: on a machine whose error text nothing recognised, the hint was
// "Could not reach Ollama at …", which tells a reader nothing they did not
// already know from the screen.
func TestDiagnoseFallbackIsStillActionable(t *testing.T) {
	c := New("http://127.0.0.1:11434")
	got := c.diagnose(errors.New("some sandbox-specific failure nobody anticipated"))

	if !strings.Contains(got, "ollama serve") {
		t.Errorf("the fallback should still say what to try, got %q", got)
	}
	// And it should carry the original text, since that is the only clue left
	// when the cause is one nothing here recognises.
	if !strings.Contains(got, "sandbox-specific") {
		t.Errorf("the fallback should keep the underlying error, got %q", got)
	}
}
