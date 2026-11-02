package cli

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ecosyste-ms/search/internal/project"
	"github.com/ecosyste-ms/search/internal/search"
)

func TestMatchingPackagePreviewThroughHTTP(t *testing.T) {
	var packages, assignments []any
	var ids []int
	for id := 1; id <= 14; id++ {
		name, registry, ecosystem := fmt.Sprintf("component-%d", id), "npmjs.org", "npm"
		if id == 12 {
			name = "@babel/core"
		}
		if id == 13 {
			registry, ecosystem = "conda-forge.org", "conda"
		}
		if id == 14 {
			registry, ecosystem = "pypi.org", "pypi"
		}
		ids = append(ids, id)
		packages = append(packages, map[string]any{"package_id": id, "name": name, "purl": nil, "registry": map[string]any{"id": id, "name": registry, "ecosystem": ecosystem}, "ecosystem": ecosystem, "repository": nil})
		assignments = append(assignments, project.ProjectAssignment{PackageID: id, PreviousProjectID: fmt.Sprintf("package:%d", id), ProjectID: "project:babel", DisplayName: "babel/babel", Reason: "repository"})
	}
	packages = append(packages, map[string]any{"package_id": 15, "name": "unrelated", "purl": nil, "ecosystem": "pypi", "registry": map[string]any{"id": 14, "name": "pypi.org", "ecosystem": "pypi"}, "repository": nil})
	assignments = append(assignments, project.ProjectAssignment{PackageID: 15, PreviousProjectID: "package:15", ProjectID: "project:other", DisplayName: "unrelated", Reason: "package"})
	files := map[string][]byte{"packages.ndjson.gz": fixtureGzip(t, packages...), "assignments.ndjson.gz": fixtureGzip(t, assignments...), "projects.ndjson.gz": fixtureGzip(t, map[string]any{"id": "project:babel", "name": "babel/babel", "reason": "repository", "description": "JavaScript compiler", "package_ids": ids}, map[string]any{"id": "project:other", "name": "unrelated", "reason": "package", "description": "Unrelated package", "package_ids": []int{15}}), "excluded.json": []byte("[]")}
	h := contractHandler(t, files)
	q := url.Values{"q": {"@babel/core"}}.Encode()
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/api/v1/search?"+q, nil))
	var result search.Response
	if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if r.Code != 200 || len(result.Hits) != 1 {
		t.Fatalf("unexpected response: %s", r.Body.String())
	}
	if result.Hits[0].Identity.Label != "npm · PyPI · conda" {
		t.Fatalf("ecosystem order: %s", result.Hits[0].Identity.Label)
	}
	shown := result.Hits[0].Public["packages"].([]any)
	if len(shown) != 10 || shown[0].(map[string]any)["name"] != "@babel/core" || shown[1].(map[string]any)["registry"] != "pypi.org" || shown[2].(map[string]any)["registry"] != "conda-forge.org" {
		t.Fatalf("matching package and registries missing: %v", shown)
	}
	for _, path := range []string{"/project/project:babel?" + q, "/project/project:babel?fragment=identities&" + q} {
		r = httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		body := r.Body.String()
		if r.Code != 200 || !strings.Contains(body, "Showing 10 of 14 packages.") || !strings.Contains(body, "@babel/core") || !strings.Contains(body, "pypi.org") || !strings.Contains(body, "conda-forge.org") {
			t.Fatalf("HTML preview: %s", body)
		}
	}
	r = httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/?"+q, nil))
	if !strings.Contains(r.Body.String(), "q=%40babel%2Fcore&amp;fragment=identities") {
		t.Fatal("expansion lost the submitted query", r.Body.String())
	}
}

func TestPageMetadataThroughHTTP(t *testing.T) {
	h := contractHandler(t, archiveFixture(t))
	for _, tc := range []struct {
		path, title, canonical string
		noindex                bool
	}{
		{"/", "Search popular packages · ecosyste.ms", "https://search.ecosyste.ms/", false},
		{"/?q=curl", "curl · ecosyste.ms search", "https://search.ecosyste.ms/?q=curl", true},
		{"/?q=missing-project", "missing-project · ecosyste.ms search", "https://search.ecosyste.ms/?q=missing-project", true},
		{"/project/project:babel?q=ignored", "babel/babel · ecosyste.ms search", "https://search.ecosyste.ms/project/project:babel", true},
	} {
		r := httptest.NewRecorder()
		req := httptest.NewRequest("GET", tc.path, nil)
		req.Host = "untrusted.example"
		h.ServeHTTP(r, req)
		body := r.Body.String()
		for _, text := range []string{"<title>" + tc.title + "</title>", `rel="canonical" href="` + tc.canonical + `"`, `name="description"`, `property="og:title"`, `name="twitter:card"`} {
			if r.Code != 200 || !strings.Contains(body, text) {
				t.Fatalf("%s missing %s: %s", tc.path, text, body)
			}
		}
		if strings.Contains(body, `name="robots"`) != tc.noindex || strings.Contains(body, "untrusted.example") {
			t.Fatalf("incorrect canonical or robots metadata: %s", body)
		}
	}
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest("GET", "/?"+url.Values{"q": {`<script>alert("x")</script>`}}.Encode(), nil))
	if strings.Contains(r.Body.String(), `<script>alert(`) || !strings.Contains(r.Body.String(), "&lt;script&gt;") {
		t.Fatal("query was not escaped in metadata")
	}
}
