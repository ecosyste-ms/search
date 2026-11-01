package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"github.com/ecosyste-ms/search/internal/search"
	"github.com/ecosyste-ms/search/internal/storage"
	"github.com/ecosyste-ms/search/internal/web"
)

func TestExistingSnapshotRegression(t *testing.T) {
	snapshot, baseline := os.Getenv("SEARCH_SNAPSHOT"), os.Getenv("SEARCH_BASELINE_URL")
	if snapshot == "" || baseline == "" {
		t.Skip("set SEARCH_SNAPSHOT and SEARCH_BASELINE_URL to compare the running service")
	}
	e, err := search.Open(filepath.Join(snapshot, "index"), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	h := web.Handler(e)
	var probes []struct{ Query string }
	if err := storage.ReadJSON("testdata/queries.json", &probes); err != nil {
		t.Fatal(err)
	}
	compare := func(path string) {
		t.Helper()
		old, err := http.Get(baseline + path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(old.Body)
		_ = old.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		got := httptest.NewRecorder()
		h.ServeHTTP(got, httptest.NewRequest("GET", path, nil))
		if got.Code != old.StatusCode || got.Header().Get("Content-Type") != old.Header.Get("Content-Type") {
			t.Fatalf("status or content type changed: %s", path)
		}
		if old.Header.Get("Content-Type") == "application/json" {
			var a, b any
			if err := json.Unmarshal(body, &a); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(got.Body.Bytes(), &b); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(a, b) {
				t.Fatalf("JSON response changed: %s", path)
			}
		} else {
			timing := regexp.MustCompile(`[0-9]+ ms`)
			if !bytes.Equal(timing.ReplaceAll(body, []byte("TIME")), timing.ReplaceAll(got.Body.Bytes(), []byte("TIME"))) {
				t.Fatalf("response changed: %s", path)
			}
		}
	}
	for _, p := range probes {
		compare("/api/v1/search?limit=100&q=" + url.QueryEscape(p.Query))
	}
	for _, p := range []string{"/api/v1/health", "/api/v1/search?q=numpy", "/api/v1/search?q=react&suggestions=false", "/?q=curl", "/?q=%40babel%2Fcore", "/project/repo:1068937", "/project/repo:1068937?fragment=identities", "/search.js", "/style.css", "/api/v1/search", "/api/v1/search?q=x&from=10", "/autocomplete?q=numpy"} {
		compare(p)
	}
	t.Logf("Identical responses for %d top-100 queries and API/UI checks", len(probes))
}
