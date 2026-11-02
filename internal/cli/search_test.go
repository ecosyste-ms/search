package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/ecosyste-ms/search/internal/project"
	"github.com/ecosyste-ms/search/internal/search"
	"github.com/ecosyste-ms/search/internal/web"
)

func TestSearchAPIAndHTML(t *testing.T) {
	packages, assignments, groups := []any{}, []any{}, []any{}
	names := []string{"@babel/core", "babel-core", "org.example:core", "org.example-core", "openssl", "openssl", "requests"}
	for i := 0; i < 600; i++ {
		names = append(names, fmt.Sprintf("utility-%d", i))
	}
	for i, name := range names {
		id := i + 1
		pid := fmt.Sprintf("package:%d", id)
		packages = append(packages, map[string]any{"package_id": id, "name": name, "purl": "pkg:npm/" + url.PathEscape(name), "ecosystem": "npm", "registry": map[string]any{"id": 1, "name": "npmjs.org", "ecosystem": "npm"}, "description": "Parsing libraries for HTTP clients", "repository": nil})
		assignments = append(assignments, project.ProjectAssignment{PackageID: id, PreviousProjectID: pid, ProjectID: pid, DisplayName: name, Reason: "package"})
		groups = append(groups, map[string]any{"id": pid, "name": name, "reason": "package", "package_ids": []int{id}})
	}
	files := map[string][]byte{"packages.ndjson.gz": fixtureGzip(t, packages...), "assignments.ndjson.gz": fixtureGzip(t, assignments...), "projects.ndjson.gz": fixtureGzip(t, groups...), "excluded.json": []byte("[]")}
	output := filepath.Join(t.TempDir(), "release")
	if err := Run(snapshotArgs(t, packFixture(t, files, nil), output), io.Discard); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(output, "snapshot")
	before := indexSnapshot(t, dir)
	e, err := search.Open(filepath.Join(dir, "index"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	h := web.Handler(e)
	get := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		return r
	}
	for q, want := range map[string]string{"@babel/core": "package:1", "@BABEL/CORE": "package:1", "org.example:core": "package:3", "org.example-core": "package:4", "requests": "package:7"} {
		r := get("/api/v1/search?q=" + url.QueryEscape(q))
		var body search.Response
		if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if r.Code != 200 || len(body.Hits) == 0 || body.Hits[0].ID != want {
			t.Fatalf("%s: %s", q, r.Body.String())
		}
		identity := body.Hits[0].Identity
		if identity == nil || identity.URL == "" {
			t.Fatalf("invalid public identity link for %s: %+v", q, identity)
		}
		if body.Hits[0].Public["bulk_lookup_requests"] == nil || body.Hits[0].Public["packages"] == nil {
			t.Fatalf("public package details missing: %s", r.Body.String())
		}
		for _, private := range []string{"entities_url", "/Users/", "/private/"} {
			if strings.Contains(r.Body.String(), private) {
				t.Fatalf("unexpected lookup URL or local path: %s", private)
			}
		}
	}
	var small, large search.Response
	if err := json.Unmarshal(get("/api/v1/search?q=parsing+libraries").Body.Bytes(), &small); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(get("/api/v1/search?q=parsing+libraries&limit=100").Body.Bytes(), &large); err != nil {
		t.Fatal(err)
	}
	if len(small.Hits) != 10 || len(large.Hits) != 100 || !reflect.DeepEqual(small.Hits, large.Hits[:10]) {
		t.Fatal("result limits change ranking")
	}
	for _, hit := range large.Hits {
		if hit.Description != "Parsing libraries for HTTP clients" {
			t.Fatalf("JSON description missing for %s: %q", hit.ID, hit.Description)
		}
	}
	if !large.SuggestionsPartial || len(large.Suggestions["ecosystem"]) == 0 {
		t.Fatal("missing candidate suggestions")
	}
	if strings.Contains(get("/api/v1/search?q=parsing+libraries&suggestions=false").Body.String(), "suggestions") {
		t.Fatal("suggestions=false ignored")
	}
	page := get("/?q=parsing+libraries")
	if page.Code != 200 || strings.Count(page.Body.String(), "<li>") != 100 || !strings.Contains(page.Body.String(), "Parsing libraries for HTTP clients") {
		t.Fatal("HTML must contain 100 results and descriptions")
	}
	for _, path := range []string{"/api/v1/search", "/api/v1/search?q=x&from=10", "/api/v1/search?q=x&limit=50", "/api/v1/search?q=x&suggestions=maybe"} {
		if get(path).Code != 400 {
			t.Fatal("bad query accepted", path)
		}
	}
	if get("/autocomplete?q=req").Code != 404 {
		t.Fatal("autocomplete is exposed")
	}
	for _, path := range []string{"/health", "/search", "/search?q=requests", "/entities/package:7", "/projects/package:7", "/summaries/package:7"} {
		r := get(path)
		if r.Code != 404 || r.Header().Get("Location") != "" {
			t.Errorf("old route remains available: %s (%d)", path, r.Code)
		}
	}
	for _, path := range []string{"/api/v1/entities/package:7", "/api/v1/projects/package:7", "/api/v1/summaries/package:7"} {
		if get(path).Code != 404 {
			t.Errorf("removed endpoint remains available: %s", path)
		}
	}
	if strings.Contains(get("/?q="+url.QueryEscape("<script>alert(1)</script>")).Body.String(), "<script>alert") {
		t.Fatal("query HTML not escaped")
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 10 {
				if get("/api/v1/search?q=requests").Code != 200 {
					t.Error("concurrent search failed")
				}
			}
		})
	}
	wg.Wait()
	if !reflect.DeepEqual(before, indexSnapshot(t, dir)) {
		t.Fatal("search changed snapshot files")
	}
}

func TestSnapshotIntegrity(t *testing.T) {
	output := filepath.Join(t.TempDir(), "release")
	if err := Run(snapshotArgs(t, packFixture(t, archiveFixture(t), nil), output), io.Discard); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(output, "snapshot")
	for _, name := range []string{"serving-catalog.ndjson", "index-checksums.json", "index/store/root.bolt"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(append([]byte{}, original...), byte(' ')), 0644); err != nil {
				t.Fatal(err)
			}
			if e, err := search.Open(filepath.Join(dir, "index"), dir); err == nil {
				_ = e.Close()
				t.Fatal("modified snapshot accepted")
			}
			if err := os.WriteFile(path, original, 0444); err != nil {
				t.Fatal(err)
			}
		})
	}
	extra := filepath.Join(dir, "index", "unexpected")
	if err := os.WriteFile(extra, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// Unknown files within the index store must also be rejected.
	if err := os.Rename(extra, filepath.Join(dir, "index/store/unexpected")); err != nil {
		t.Fatal(err)
	}
	if e, err := search.Open(filepath.Join(dir, "index"), dir); err == nil {
		_ = e.Close()
		t.Fatal("unlisted index file accepted")
	}
}
