package webserve

import (
	"net/http"
	"net/http/httptest"
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
