package webserve

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// A route reloads into the app; a missing file says it is missing.
func TestAssetFallbackOnlyAppliesToRoutes(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html>app")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
	s := New(NewRegistry(), NewHub(), assets, "t")
	h := s.Handler()

	cases := []struct {
		path string
		code int
		body string
	}{
		{"/", http.StatusOK, "<!doctype html>app"},
		{"/assets/app.js", http.StatusOK, "console.log(1)"},
		{"/some/deep/route", http.StatusOK, "<!doctype html>app"},
		// The one that mattered: a script that is not there must not be given
		// HTML with a straight face.
		{"/wails/custom.js", http.StatusNotFound, ""},
		{"/assets/missing.css", http.StatusNotFound, ""},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != c.code {
			t.Errorf("%s answered %d, want %d", c.path, rec.Code, c.code)
		}
		if c.body != "" && rec.Body.String() != c.body {
			t.Errorf("%s served %q", c.path, rec.Body.String())
		}
	}
}

// A service that panics fails its own call and leaves the connection alone.
type PanickyService struct{}

func (PanickyService) Explode() string { panic("a bug in one method") }
func (PanickyService) Fine() string    { return "still here" }

func TestPanicInOneCallDoesNotTakeTheConnection(t *testing.T) {
	s := New(NewRegistry(PanickyService{}), NewHub(), fstest.MapFS{"index.html": {Data: []byte("app")}}, "t")
	h := s.Handler()

	call := func(method string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/call",
			strings.NewReader(`{"name":"`+method+`","args":[]}`))
		req.Header.Set("Authorization", "Bearer t")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	boom := call("PanickyService.Explode")
	if boom.Code != http.StatusUnprocessableEntity {
		t.Errorf("a panicking call answered %d, want %d", boom.Code, http.StatusUnprocessableEntity)
	}
	if !strings.Contains(boom.Body.String(), "a bug in one method") {
		t.Errorf("the answer does not say what happened: %s", boom.Body.String())
	}

	// And the next call on the same server still works.
	if ok := call("PanickyService.Fine"); ok.Code != http.StatusOK {
		t.Errorf("the following call answered %d", ok.Code)
	}
}
