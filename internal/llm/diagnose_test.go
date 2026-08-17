package llm

// These tests are in-package and feed diagnose synthetic errors, because the
// point of the exercise is that the diagnosis must not depend on what a
// particular kernel, libc or sandbox writes into an error string. Driving it
// through a real socket is what let a CI runner disagree with a developer's
// machine about which branch was taken.

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
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

// TestChatSendsTheContextWindow guards a silent failure: Ollama loads a model
// with a small default window and quietly drops whatever does not fit, and what
// falls off the front is the system prompt — so the model stops following rules
// it can no longer see, and nothing anywhere reports a problem.
func TestChatSendsTheContextWindow(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{"role": "assistant", "content": "ok"},
		})
	}))
	defer srv.Close()

	temp := 0.0
	if _, err := New(srv.URL).Chat(context.Background(), ChatRequest{
		Model: "qwen3:8b", NumCtx: 16384, Temperature: &temp,
		Messages: []Message{{Role: "user", Content: "hello"}},
	}); err != nil {
		t.Fatalf("chat: %v", err)
	}

	options, ok := body["options"].(map[string]any)
	if !ok {
		t.Fatalf("no options were sent: %v", body)
	}
	if options["num_ctx"] != float64(16384) {
		t.Errorf("num_ctx = %v, want 16384", options["num_ctx"])
	}
	// Temperature 0 has to survive alongside it: the two share one options
	// object, and an earlier version of this built that object twice.
	if options["temperature"] != float64(0) {
		t.Errorf("temperature = %v, want 0", options["temperature"])
	}
}

// TestChatOmitsOptionsWhenThereAreNone keeps the request clean when neither is
// set, so the model's own Modelfile defaults still apply.
func TestChatOmitsOptionsWhenThereAreNone(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{"role": "assistant", "content": "ok"},
		})
	}))
	defer srv.Close()

	if _, err := New(srv.URL).Chat(context.Background(), ChatRequest{
		Model: "qwen3:8b", Messages: []Message{{Role: "user", Content: "hello"}},
	}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if _, present := body["options"]; present {
		t.Errorf("options should be absent when nothing is set: %v", body["options"])
	}
}
