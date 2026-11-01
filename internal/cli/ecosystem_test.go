package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ecosyste-ms/search/internal/project"
	"github.com/ecosyste-ms/search/internal/search"
	"github.com/ecosyste-ms/search/internal/web"
)

func ecosystemFixture(t *testing.T) map[string][]byte {
	t.Helper()
	var packages, assignments, groups []any
	for i := 1; i <= 122; i++ {
		pid, name, ecosystem, registry := fmt.Sprintf("package:%d", i), "http", "npm", "npmjs.org"
		if i > 120 {
			pid, name = "project:shared", "quiet-client"
		}
		if i == 121 {
			ecosystem, registry = "pypi", "pypi.org"
		}
		packages = append(packages, map[string]any{"package_id": i, "name": name, "purl": "pkg:" + ecosystem + "/" + name, "ecosystem": ecosystem, "registry": map[string]any{"id": i, "name": registry, "ecosystem": ecosystem}, "description": "HTTP client library", "repository": nil})
		assignments = append(assignments, project.ProjectAssignment{PackageID: i, PreviousProjectID: pid, ProjectID: pid, DisplayName: name, Reason: "package"})
		if i <= 120 {
			groups = append(groups, map[string]any{"id": pid, "name": name, "reason": "package", "package_ids": []int{i}})
		}
	}
	groups = append(groups, map[string]any{"id": "project:shared", "name": "quiet-client", "reason": "package", "package_ids": []int{121, 122}})
	return map[string][]byte{"packages.ndjson.gz": fixtureGzip(t, packages...), "assignments.ndjson.gz": fixtureGzip(t, assignments...), "projects.ndjson.gz": fixtureGzip(t, groups...), "excluded.json": []byte("[]")}
}

func TestEcosystemFilterThroughAPI(t *testing.T) {
	output := filepath.Join(t.TempDir(), "release")
	if err := Run(snapshotArgs(t, packFixture(t, ecosystemFixture(t), nil), output), io.Discard); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(output, "snapshot")
	e, err := search.Open(filepath.Join(dir, "index"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	h := web.Handler(e)
	get := func(query, parameters string) search.Response {
		t.Helper()
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", "/api/v1/search?limit=100&q="+url.QueryEscape(query)+parameters, nil))
		var result search.Response
		if r.Code != 200 {
			t.Fatalf("search failed: %d %s", r.Code, r.Body.String())
		}
		if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	all := get("http", "")
	if len(all.Hits) != 100 {
		t.Fatal("fixture must fill the unfiltered candidate limit")
	}
	for _, hit := range all.Hits {
		if hit.ID == "project:shared" {
			t.Fatal("fixture must put the PyPI project outside the unfiltered candidates")
		}
	}
	filtered := get("http", "&ecosystem=%20PyPI%20")
	if len(filtered.Hits) != 1 || filtered.Hits[0].ID != "project:shared" {
		t.Fatalf("filter must retrieve beyond unfiltered top 100: %+v", filtered.Hits)
	}
	if !reflect.DeepEqual(filtered.Suggestions["ecosystem"], []string{"npm", "pypi"}) || !filtered.SuggestionsPartial {
		t.Fatal("multi-ecosystem membership lost from suggestions", filtered.Suggestions)
	}
	if len(get("http", "&ecosystem=missing").Hits) != 0 {
		t.Fatal("unknown ecosystem must return no results")
	}
	if !reflect.DeepEqual(all, get("http", "&ecosystem=")) {
		t.Fatal("empty filter changed results")
	}
	for _, ecosystem := range []string{"npm", "pypi"} {
		unfiltered := get("quiet-client", "")
		selected := get("quiet-client", "&ecosystem="+ecosystem)
		if len(selected.Hits) != 1 || !reflect.DeepEqual(unfiltered.Hits, selected.Hits) {
			t.Fatalf("filter changed scores or identities: %+v versus %+v", unfiltered.Hits, selected.Hits)
		}
	}
	if get("http", "&ecosystem=pypi&suggestions=false").Suggestions != nil {
		t.Fatal("suggestions=false ignored")
	}
	public := string(fixtureJSON(t, filtered.Hits[0].Public))
	for _, purl := range []string{"pkg:npm/quiet-client", "pkg:pypi/quiet-client"} {
		if !strings.Contains(public, purl) {
			t.Fatal("filter removed public identity", purl, public)
		}
	}
	for _, parameters := range []string{"ecosystem=npm&ecosystem=pypi", "ecosystem=" + strings.Repeat("x", 101), "registry=npmjs.org"} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", "/api/v1/search?q=http&"+parameters, nil))
		if r.Code != 400 {
			t.Errorf("invalid parameter accepted: %s", parameters)
		}
	}
}
