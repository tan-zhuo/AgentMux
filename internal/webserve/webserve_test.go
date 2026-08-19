package webserve

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// fakeService covers every return shape the app's services use.
type fakeService struct{}

type point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

func (fakeService) Add(a, b int) int                  { return a + b }
func (fakeService) Move(p point) (point, error)       { return point{p.X + 1, p.Y + 1}, nil }
func (fakeService) Fail() error                       { return errors.New("boom") }
func (fakeService) Fire()                             {}
func (fakeService) Greet(name string) (string, error) { return "hi " + name, nil }

func reg() *Registry { return NewRegistry(fakeService{}) }

func TestRegistryDispatch(t *testing.T) {
	cases := []struct {
		name string
		args string
		want string
	}{
		// The frontend sends the Wails bound name, package path included.
		{"agentmux/internal/app.fakeService.Add", `[2,3]`, `5`},
		{"fakeService.Move", `[{"x":1,"y":2}]`, `{"x":2,"y":3}`},
		{"fakeService.Fire", `[]`, `null`},
		{"fakeService.Greet", `["tan"]`, `"hi tan"`},
		// null argument means zero value, as the Wails binder reads it.
		{"fakeService.Greet", `[null]`, `"hi "`},
	}
	for _, c := range cases {
		var args []json.RawMessage
		if err := json.Unmarshal([]byte(c.args), &args); err != nil {
			t.Fatal(err)
		}
		got, err := reg().Call(c.name, args)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if string(got) != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestRegistryErrors(t *testing.T) {
	if _, err := reg().Call("fakeService.Fail", nil); err == nil || err.Error() != "boom" {
		t.Errorf("service error not surfaced: %v", err)
	}
	if _, err := reg().Call("fakeService.Missing", nil); err == nil {
		t.Error("unknown method accepted")
	}
	if _, err := reg().Call("fakeService.Add", []json.RawMessage{json.RawMessage(`1`)}); err == nil {
		t.Error("wrong arity accepted")
	}
}

func newTestServer() *Server {
	assets := fstest.MapFS{
		"index.html": {Data: []byte("<html>app</html>")},
		"app.js":     {Data: []byte("js")},
	}
	return New(reg(), NewHub(), assets, "secret-token")
}

func TestAPIRequiresToken(t *testing.T) {
	h := newTestServer().Handler()

	body := `{"name":"fakeService.Add","args":[1,2]}`
	r := httptest.NewRequest("POST", "/api/call", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("tokenless call: got %d want 401", w.Code)
	}

	r = httptest.NewRequest("POST", "/api/call", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer secret-token")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != "3" {
		t.Errorf("authorised call: got %d %q", w.Code, w.Body.String())
	}

	// EventSource cannot set headers, so the stream accepts the query form.
	// A wrong token must still be turned away.
	r = httptest.NewRequest("GET", "/api/events?token=wrong", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("bad stream token: got %d want 401", w.Code)
	}
}

func TestAuthEndpoint(t *testing.T) {
	h := newTestServer().Handler()
	for body, want := range map[string]int{
		`{"token":"secret-token"}`: http.StatusNoContent,
		`{"token":"nope"}`:         http.StatusUnauthorized,
	} {
		r := httptest.NewRequest("POST", "/api/auth", strings.NewReader(body))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != want {
			t.Errorf("auth %s: got %d want %d", body, w.Code, want)
		}
	}
}

// The SPA fallback is what makes a reload deep inside the app boot instead of
// 404ing, while real files are served as themselves and the API stays JSON.
func TestAssetsAndSPAFallback(t *testing.T) {
	h := newTestServer().Handler()
	for path, want := range map[string]string{
		"/":            "<html>app</html>",
		"/app.js":      "js",
		"/servers/abc": "<html>app</html>",
	} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK || w.Body.String() != want {
			t.Errorf("%s: got %d %q want %q", path, w.Code, w.Body.String(), want)
		}
	}
}

func TestServiceErrorsAreDistinguishable(t *testing.T) {
	h := newTestServer().Handler()
	r := httptest.NewRequest("POST", "/api/call", strings.NewReader(`{"name":"fakeService.Fail","args":[]}`))
	r.Header.Set("Authorization", "Bearer secret-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnprocessableEntity {
		t.Errorf("service error status: got %d want 422", w.Code)
	}
	var e struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil || e.Error != "boom" {
		t.Errorf("error body: %q (%v)", w.Body.String(), err)
	}
}
