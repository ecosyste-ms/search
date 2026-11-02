package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicAPICORS(t *testing.T) {
	h := contractHandler(t, archiveFixture(t))
	for _, origin := range []string{"", "https://example.org", "https://another.example"} {
		for _, tc := range []struct {
			method, path string
			status       int
		}{
			{"GET", "/api/v1/search?q=curl", 200},
			{"GET", "/api/v1/search", 400},
			{"HEAD", "/api/v1/health", 200},
			{"GET", "/openapi.yml", 200},
			{"POST", "/api/v1/search", 405},
			{"OPTIONS", "/api/v1/search", 204},
			{"OPTIONS", "/openapi.yml", 204},
		} {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Origin", origin)
			if tc.method == http.MethodOptions {
				req.Header.Set("Access-Control-Request-Method", "GET")
				req.Header.Set("Access-Control-Request-Headers", "content-type")
			}
			r := httptest.NewRecorder()
			h.ServeHTTP(r, req)
			if r.Code != tc.status || r.Header().Get("Access-Control-Allow-Origin") != "*" {
				t.Errorf("%s %s origin=%q: %d %v", tc.method, tc.path, origin, r.Code, r.Header())
			}
			if r.Header().Get("Access-Control-Allow-Credentials") != "" || strings.Contains(r.Header().Get("Vary"), "Origin") {
				t.Errorf("public CORS must be credential-free and independent of origin: %v", r.Header())
			}
			if tc.method == http.MethodOptions && (r.Header().Get("Access-Control-Allow-Methods") != "GET, HEAD, OPTIONS" || r.Header().Get("Access-Control-Allow-Headers") != "Accept, Content-Type" || r.Body.Len() != 0) {
				t.Errorf("invalid preflight: %v %q", r.Header(), r.Body.String())
			}
		}
	}
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/", nil))
	if r.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("CORS applied to HTML")
	}
}
