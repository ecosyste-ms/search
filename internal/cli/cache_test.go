package cli

import (
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ecosyste-ms/search/internal/search"
	"github.com/ecosyste-ms/search/internal/web"
)

func TestSearchCacheHeaders(t *testing.T) {
	output := filepath.Join(t.TempDir(), "release")
	if err := Run(snapshotArgs(t, packFixture(t, archiveFixture(t), nil), output), io.Discard); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(output, "snapshot")
	e, err := search.Open(filepath.Join(dir, "index"), dir)
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = e.Close()
		}
	})
	h := web.Handler(e)
	check := func(path string, status int, cache string) {
		t.Helper()
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if r.Code != status || r.Header().Get("Cache-Control") != cache {
			t.Errorf("%s: status=%d cache=%q", path, r.Code, r.Header().Get("Cache-Control"))
		}
		if strings.HasPrefix(path, "/api/v1/") && r.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("%s: missing public CORS header for status %d", path, r.Code)
		}
	}
	for _, path := range []string{
		"/", "/?q=curl", "/?q=not-in-corpus",
		"/api/v1/search?q=curl", "/api/v1/search?q=curl&limit=100",
		"/api/v1/search?q=curl&ecosystem=npm&suggestions=false",
		"/api/v1/search?q=not-in-corpus",
		"/project/project:curl", "/project/project:curl?fragment=identities",
	} {
		check(path, 200, "public, max-age=3600, s-maxage=3600")
	}
	check("/api/v1/health", 200, "no-store")
	check("/project/missing", 404, "no-store")
	for _, path := range []string{
		"/api/v1/search", "/api/v1/search?q=curl&limit=50",
		"/api/v1/search?q=curl&ecosystem=npm&ecosystem=gem",
		"/api/v1/search?q=curl&suggestions=invalid",
		"/api/v1/search?q=curl&unknown=value",
		"/?q=" + strings.Repeat("x", 501),
	} {
		check(path, 400, "no-store")
	}
	closed = true
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	check("/api/v1/search?q=curl", 500, "no-store")
	check("/?q=curl", 500, "no-store")
}
