package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ecosyste-ms/search/internal/project"
	"github.com/ecosyste-ms/search/internal/search"
	"github.com/ecosyste-ms/search/internal/web"
)

func fixtureJSON(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func fixtureGzip(t *testing.T, rows ...any) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	for _, row := range rows {
		if err := json.NewEncoder(gz).Encode(row); err != nil {
			t.Fatal(err)
		}
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func archiveFixture(t *testing.T) map[string][]byte {
	t.Helper()
	rows := []any{}
	assignments := []any{}
	groups := []any{}
	for i, name := range []string{"@babel/core", "curl", "curl"} {
		id := i + 1
		projectID, display, reason := "project:curl", "curl", "distribution_release"
		registry, ecosystem := "nixpkgs-stable", "nixpkgs"
		var purl any = "pkg:nix/curl?channel=stable"
		if i == 0 {
			projectID, display, reason = "project:babel", "babel/babel", "repository"
			registry, ecosystem = "npmjs.org", "npm"
			purl = "pkg:npm/%40babel/core"
		}
		if i == 2 {
			registry = "nixpkgs-unstable"
			purl = nil
		}
		rows = append(rows, map[string]any{"package_id": id, "name": name, "purl": purl, "ecosystem": ecosystem, "registry": map[string]any{"id": id, "name": registry, "ecosystem": ecosystem}, "namespace": nil, "description": "Parsing libraries for JavaScript", "rankings": map[string]any{}, "repository": nil, "status": nil})
		assignments = append(assignments, project.ProjectAssignment{PackageID: id, PreviousProjectID: "package:old", ProjectID: projectID, DisplayName: display, Reason: reason})
	}
	groups = append(groups, map[string]any{"id": "project:babel", "name": "babel/babel", "reason": "repository", "package_ids": []int{1}}, map[string]any{"id": "project:curl", "name": "curl", "reason": "distribution_release", "package_ids": []int{2, 3}})
	return map[string][]byte{"packages.ndjson.gz": fixtureGzip(t, rows...), "projects.ndjson.gz": fixtureGzip(t, groups...), "assignments.ndjson.gz": fixtureGzip(t, assignments...), "excluded.json": []byte("[]")}
}
func packFixture(t *testing.T, files map[string][]byte, change func(map[string]any)) []byte {
	t.Helper()
	sha := func(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }
	packageCount, projectCount := fixtureCount(t, files["packages.ndjson.gz"]), fixtureCount(t, files["projects.ndjson.gz"])
	files["assignment-manifest.json"] = fixtureJSON(t, map[string]any{"packages": packageCount, "projects": projectCount, "source_package_sha256": sha(files["packages.ndjson.gz"]), "assignments_file": "assignments.ndjson.gz", "assignments_sha256": sha(files["assignments.ndjson.gz"]), "as_of": "2026-10-31T00:00:00Z"})
	info := map[string]any{}
	for name, b := range files {
		info[name] = map[string]any{"bytes": len(b), "sha256": sha(b)}
	}
	m := map[string]any{"records": packageCount, "projects": projectCount, "sha256": sha(files["packages.ndjson.gz"]), "extraction_finished_at": "2026-10-31T00:00:00Z", "files": info}
	if change != nil {
		change(m)
	}
	files["manifest.json"] = fixtureJSON(t, m)
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func snapshotArgs(t *testing.T, data []byte, output string) []string {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(data) }))
	t.Cleanup(s.Close)
	return []string{"snapshot", "-url", s.URL + "/corpus.tar.gz", "-sha256", fmt.Sprintf("%x", sha256.Sum256(data)), "-output", output}
}
func TestSnapshotFromHTTPThroughCLI(t *testing.T) {
	output := filepath.Join(t.TempDir(), "release")
	args := snapshotArgs(t, packFixture(t, archiveFixture(t), nil), output)
	if err := Run(args, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := Run(args, io.Discard); err == nil {
		t.Fatal("existing snapshot replaced")
	}
	snapshot := filepath.Join(output, "snapshot")
	before := indexSnapshot(t, snapshot)
	e, err := search.Open(filepath.Join(snapshot, "index"), snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	assertSnapshotWithoutDocValues(t, e)
	h := web.Handler(e)
	for path, want := range map[string]string{"/api/v1/health": `"created_at":"2026-10-31T00:00:00Z"`, "/api/v1/search?q=%40babel%2Fcore": "babel/babel", "/?q=parsing+libraries": "babel/babel", "/api/v1/search?q=curl": "pkg:nix/curl?channel=stable", "/project/project:curl": "nixpkgs-unstable", "/openapi.yml": "openapi: 3.1.0"} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest("GET", path, nil))
		if rr.Code != 200 || !strings.Contains(rr.Body.String(), want) {
			t.Fatalf("%s: %d %s", path, rr.Code, rr.Body.String())
		}
	}
	if !reflect.DeepEqual(before, indexSnapshot(t, snapshot)) {
		t.Fatal("serving mutated snapshot")
	}
	f, err := os.Open(filepath.Join(output, "search.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	entries := 0
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		entries++
		if strings.Contains(header.Name, "sqlite") || strings.Contains(header.Name, "source") {
			t.Fatal("runtime dependency", header.Name)
		}
	}
	if entries < 4 {
		t.Fatal("incomplete bundle")
	}
	link := filepath.Join(t.TempDir(), "current")
	if err := os.Symlink(output, link); err != nil {
		t.Fatal(err)
	}
	active, err := search.Open(filepath.Join(link, "snapshot/index"), filepath.Join(link, "snapshot"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = active.Close() }()
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	web.Handler(active).ServeHTTP(rr, httptest.NewRequest("GET", "/api/v1/search?q=curl", nil))
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "project:curl") {
		t.Fatal("open snapshot changed with symlink", rr.Body.String())
	}
}

func TestSnapshotRejectsInvalidArchives(t *testing.T) {
	for _, name := range []string{"checksum", "counts", "schema", "file-checksum", "truncated", "traversal", "missing-purl", "missing-registry", "unassigned", "memberships", "duplicate", "missing-file"} {
		t.Run(name, func(t *testing.T) {
			files := archiveFixture(t)
			switch name {
			case "traversal":
				files["../escape"] = []byte("bad")
			case "missing-file":
				delete(files, "excluded.json")
			case "memberships":
				files["projects.ndjson.gz"] = fixtureGzip(t, map[string]any{"id": "project:curl", "name": "curl", "reason": "distribution_release", "package_ids": []int{1, 2, 3}})
			case "missing-purl", "missing-registry", "unassigned", "duplicate":
				var rows []any
				gz, err := gzip.NewReader(bytes.NewReader(files["packages.ndjson.gz"]))
				if err != nil {
					t.Fatal(err)
				}
				d := json.NewDecoder(gz)
				for {
					var row map[string]any
					if err := d.Decode(&row); err == io.EOF {
						break
					} else if err != nil {
						t.Fatal(err)
					}
					rows = append(rows, row)
				}
				_ = gz.Close()
				row := rows[0].(map[string]any)
				switch name {
				case "missing-purl":
					delete(row, "purl")
				case "missing-registry":
					row["registry"] = map[string]any{}
				case "unassigned":
					row["package_id"] = 9
				case "duplicate":
					rows[2] = rows[0]
				}
				files["packages.ndjson.gz"] = fixtureGzip(t, rows...)
			}
			data := packFixture(t, files, func(m map[string]any) {
				switch name {
				case "counts":
					m["records"] = 4
				case "schema":
					delete(m, "files")
				case "file-checksum":
					m["sha256"] = strings.Repeat("0", 64)
				}
			})
			if name == "truncated" {
				data = data[:len(data)-8]
			}
			root := t.TempDir()
			previous := filepath.Join(root, "previous")
			if err := os.WriteFile(previous, []byte("working"), 0644); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(root, "new")
			args := snapshotArgs(t, data, output)
			if name == "checksum" {
				args[4] = strings.Repeat("0", 64)
			}
			if err := Run(args, io.Discard); err == nil {
				t.Fatal("invalid snapshot accepted")
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 1 {
				t.Fatal("failed output remains")
			}
			b, err := os.ReadFile(previous)
			if err != nil || string(b) != "working" {
				t.Fatal("previous snapshot modified")
			}
		})
	}
}

func fixtureCount(t *testing.T, data []byte) int {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gz.Close() }()
	d := json.NewDecoder(gz)
	n := 0
	for {
		var row json.RawMessage
		err := d.Decode(&row)
		if err == io.EOF {
			return n
		}
		if err != nil {
			t.Fatal(err)
		}
		n++
	}
}
