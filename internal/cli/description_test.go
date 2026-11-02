package cli

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ecosyste-ms/search/internal/search"
)

func TestPublishedDescriptionThroughHTTP(t *testing.T) {
	const description = ":chart_with_upwards_trend: HTTP :rocket: client :custom_emoji: \U0001F680"
	const want = "HTTP client :custom_emoji: \U0001F680"
	const name = "metrics-client"
	files := map[string][]byte{
		"packages.ndjson.gz":    fixtureGzip(t, map[string]any{"package_id": 1, "name": name, "purl": "pkg:npm/" + name, "ecosystem": "npm", "registry": map[string]any{"id": 1, "name": "npmjs.org", "ecosystem": "npm"}, "description": "## Installation", "repository": nil}),
		"assignments.ndjson.gz": fixtureGzip(t, map[string]any{"package_id": 1, "previous_project_id": "package:1", "project_id": "package:1", "display_name": name, "reason": "package"}),
		"projects.ndjson.gz":    fixtureGzip(t, map[string]any{"id": "package:1", "name": name, "reason": "package", "description": description, "package_ids": []int{1}}),
		"excluded.json":         []byte("[]"),
	}
	h := contractHandler(t, files)
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/api/v1/search?q="+name, nil))
	var response search.Response
	if err := json.Unmarshal(r.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if r.Code != 200 || len(response.Hits) != 1 || response.Hits[0].Description != want {
		t.Fatalf("unexpected search response: %s", r.Body.String())
	}
	for _, path := range []string{"/?q=" + name, "/project/package:1"} {
		r = httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if r.Code != 200 || !strings.Contains(r.Body.String(), "<p class=\"description\">"+want+"</p>") && !strings.Contains(r.Body.String(), "<p>"+want+"</p>") {
			t.Fatalf("description missing from %s: %s", path, r.Body.String())
		}
	}
	for query, count := range map[string]int{"installation": 1, "HTTP": 0} {
		r = httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", "/api/v1/search?q="+query, nil))
		var result search.Response
		if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil || r.Code != 200 || len(result.Hits) != count {
			t.Fatalf("indexed text changed for %s: %s", query, r.Body.String())
		}
	}
}

func TestSnapshotRequiresProjectDescription(t *testing.T) {
	for _, value := range []string{"missing", "null", "blank", "wrong-type"} {
		t.Run(value, func(t *testing.T) {
			files := archiveFixture(t)
			babel := map[string]any{"id": "project:babel", "name": "babel/babel", "reason": "repository", "package_ids": []int{1}}
			switch value {
			case "null":
				babel["description"] = nil
			case "blank":
				babel["description"] = " \n\t"
			case "wrong-type":
				babel["description"] = 42
			}
			files["projects.ndjson.gz"] = fixtureGzip(t, babel, map[string]any{"id": "project:curl", "name": "curl", "reason": "distribution_release", "description": "Transfer data with URLs", "package_ids": []int{2, 3}})
			output := filepath.Join(t.TempDir(), "release")
			err := Run(snapshotArgs(t, packFixture(t, files, nil), output), io.Discard)
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), "description") {
				t.Fatalf("invalid description accepted: %v", err)
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatalf("failed import left a release: %v", err)
			}
		})
	}
}
