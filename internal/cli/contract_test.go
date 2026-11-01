package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ecosyste-ms/search/internal/project"
	"github.com/ecosyste-ms/search/internal/search"
	"github.com/ecosyste-ms/search/internal/web"
)

func contractHandler(t *testing.T, files map[string][]byte) http.Handler {
	t.Helper()
	output := filepath.Join(t.TempDir(), "release")
	if err := Run(snapshotArgs(t, packFixture(t, files, nil), output), io.Discard); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(output, "snapshot")
	e, err := search.Open(filepath.Join(dir, "index"), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Close() })
	return web.Handler(e)
}

func TestSearchUTF8ByteLimits(t *testing.T) {
	h := contractHandler(t, archiveFixture(t))
	for _, tc := range []struct {
		name, query, ecosystem string
		status                 int
	}{
		{"ascii boundary", strings.Repeat("a", 500), "", 200},
		{"ascii excess", strings.Repeat("a", 501), "", 400},
		{"multibyte boundary", strings.Repeat("é", 250), "", 200},
		{"multibyte excess", strings.Repeat("é", 251), "", 400},
		{"trim before counting", " \t" + strings.Repeat("é", 250) + "\n ", "", 200},
		{"blank after trimming", " \t\n ", "", 400},
		{"ecosystem boundary", "curl", strings.Repeat("é", 50), 200},
		{"ecosystem excess", "curl", strings.Repeat("é", 51), 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRecorder()
			values := url.Values{"q": {tc.query}, "ecosystem": {tc.ecosystem}}
			h.ServeHTTP(r, httptest.NewRequest("GET", "/api/v1/search?"+values.Encode(), nil))
			if r.Code != tc.status {
				t.Fatalf("status %d, want %d: %s", r.Code, tc.status, r.Body.String())
			}
		})
	}
}

func TestSearchSuggestionPresence(t *testing.T) {
	h := contractHandler(t, archiveFixture(t))
	for _, q := range []string{"curl", "not-in-corpus"} {
		for _, setting := range []string{"", "&suggestions=true", "&suggestions=false"} {
			t.Run(q+setting, func(t *testing.T) {
				r := httptest.NewRecorder()
				h.ServeHTTP(r, httptest.NewRequest("GET", "/api/v1/search?q="+q+setting, nil))
				var body map[string]json.RawMessage
				if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if r.Code != 200 {
					t.Fatalf("status %d: %s", r.Code, r.Body.String())
				}
				_, suggestions := body["suggestions"]
				_, partial := body["suggestions_partial"]
				enabled := setting != "&suggestions=false"
				if suggestions != enabled || partial != enabled {
					t.Fatalf("unexpected suggestion fields: %s", r.Body.String())
				}
				if !enabled {
					return
				}
				assertCandidateSuggestions(t, body, q == "not-in-corpus")
			})
		}
	}
}

func assertCandidateSuggestions(t *testing.T, body map[string]json.RawMessage, empty bool) {
	t.Helper()
	if string(body["suggestions_partial"]) != "true" {
		t.Fatal("candidate suggestions must be marked partial")
	}
	var categories map[string][]string
	if err := json.Unmarshal(body["suggestions"], &categories); err != nil {
		t.Fatal(err)
	}
	for _, field := range search.FacetFields {
		values, present := categories[field]
		if !present || values == nil || (empty && len(values) != 0) {
			t.Errorf("unexpected %s suggestions: %v", field, values)
		}
	}
}

func TestSearchLimitCountsProjects(t *testing.T) {
	const memberships = 101
	var packages, assignments []any
	var ids []int
	for id := 1; id <= memberships; id++ {
		ids = append(ids, id)
		channel := fmt.Sprintf("channel-%d", id)
		packages = append(packages, map[string]any{"package_id": id, "name": "curl", "purl": "pkg:nix/curl?channel=" + channel, "ecosystem": "nixpkgs", "registry": map[string]any{"id": id, "name": "nixpkgs-" + channel, "ecosystem": "nixpkgs"}, "repository": nil})
		assignments = append(assignments, project.ProjectAssignment{PackageID: id, PreviousProjectID: fmt.Sprintf("package:%d", id), ProjectID: "project:curl", DisplayName: "curl", Reason: "distribution_release"})
	}
	groups := map[string]any{"id": "project:curl", "name": "curl", "reason": "distribution_release", "package_ids": ids}
	files := map[string][]byte{"packages.ndjson.gz": fixtureGzip(t, packages...), "assignments.ndjson.gz": fixtureGzip(t, assignments...), "projects.ndjson.gz": fixtureGzip(t, groups), "excluded.json": []byte("[]")}
	h := contractHandler(t, files)
	for _, limit := range []string{"10", "100"} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", "/api/v1/search?q=curl&limit="+limit, nil))
		var body search.Response
		if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if r.Code != 200 || len(body.Hits) != 1 {
			t.Fatalf("unexpected project count: %s", r.Body.String())
		}
		hit := body.Hits[0]
		if len(hit.Public["packages"].([]any)) != memberships || len(hit.Public["bulk_lookup_requests"].([]any)) != 2 {
			t.Fatal("project limit truncated public memberships or lookup requests")
		}
		if hit.Identity.URL != "" {
			t.Fatal("multi-package project arbitrarily selected one public URL")
		}
		if hit.Score != hit.Lexical {
			t.Fatal("documented score equality changed")
		}
	}
}
