package cli

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ecosyste-ms/search/internal/search"
)

func TestDescriptionsRemoveEmojiCodes(t *testing.T) {
	const description = ":chart_with_upwards_trend: HTTP :rocket: client :custom_emoji: \U0001F680"
	const want = "HTTP client :custom_emoji: \U0001F680"
	const name = "metrics-client"
	files := map[string][]byte{
		"packages.ndjson.gz":    fixtureGzip(t, map[string]any{"package_id": 1, "name": name, "purl": "pkg:npm/" + name, "ecosystem": "npm", "registry": map[string]any{"id": 1, "name": "npmjs.org", "ecosystem": "npm"}, "description": description, "repository": nil}),
		"assignments.ndjson.gz": fixtureGzip(t, map[string]any{"package_id": 1, "previous_project_id": "package:1", "project_id": "package:1", "display_name": name, "reason": "package"}),
		"projects.ndjson.gz":    fixtureGzip(t, map[string]any{"id": "package:1", "name": name, "reason": "package", "package_ids": []int{1}}),
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
}
