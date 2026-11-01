package web

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/ecosyste-ms/search/internal/search"
)

func TestOpenAPISpec(t *testing.T) {
	h := Handler(&search.Engine{})
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/openapi.yml", nil))
	if r.Code != 200 || r.Header().Get("Content-Type") != "application/yaml" || !bytes.Equal(r.Body.Bytes(), openAPISpec) {
		t.Fatalf("spec response: %d %s", r.Code, r.Body.String())
	}
	for _, path := range []string{"/search", "/health"} {
		if !bytes.Contains(r.Body.Bytes(), []byte("\n  "+path+":")) {
			t.Errorf("undocumented route: %s", path)
		}
	}
	for _, path := range []string{"/entities/{id}", "/projects/{id}", "/summaries/{id}"} {
		if bytes.Contains(r.Body.Bytes(), []byte("\n  "+path+":")) {
			t.Errorf("removed endpoint still documented: %s", path)
		}
	}
	r = httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("POST", "/openapi.yml", nil))
	if r.Code != 405 {
		t.Fatalf("spec accepts POST: %d", r.Code)
	}
}

func TestSearchPageHeader(t *testing.T) {
	r := httptest.NewRecorder()
	Handler(&search.Engine{}).ServeHTTP(r, httptest.NewRequest("GET", "/", nil))
	if r.Code != 200 {
		t.Fatalf("search page: %d", r.Code)
	}
	for _, fragment := range []string{`class="header-links"`, `href="/openapi.yml"`, `href="https://github.com/ecosyste-ms/search">GitHub</a>`, `placeholder="Search popular packages by name or description"`} {
		if !bytes.Contains(r.Body.Bytes(), []byte(fragment)) {
			t.Errorf("missing HTML: %s", fragment)
		}
	}
}
