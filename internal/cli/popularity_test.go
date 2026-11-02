package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ecosyste-ms/search/internal/search"
	"github.com/ecosyste-ms/search/internal/web"
)

const popularityFixtureEcosystem = "nixpkgs"
const popularityFixtureProject = "project:curl"

func popularityArchive(t *testing.T, count int) map[string][]byte {
	t.Helper()
	files := archiveFixture(t)
	gz, err := gzip.NewReader(bytes.NewReader(files["packages.ndjson.gz"]))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gz.Close() }()
	decoder := json.NewDecoder(gz)
	rows := []any{}
	for {
		var row map[string]any
		if err := decoder.Decode(&row); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if row["ecosystem"] == popularityFixtureEcosystem {
			row["signals"] = map[string]any{"dependent_repos_count": count}
		}
		rows = append(rows, row)
	}
	files["packages.ndjson.gz"] = fixtureGzip(t, rows...)
	return files
}

func TestPopularitySnapshotThroughCLI(t *testing.T) {
	var baseline []search.Hit
	for _, count := range []int{0, 10} {
		output := filepath.Join(t.TempDir(), "release")
		if err := Run(snapshotArgs(t, packFixture(t, popularityArchive(t, count), nil), output), io.Discard); err != nil {
			t.Fatal(err)
		}
		snapshot := filepath.Join(output, "snapshot")
		assertBundlePopularity(t, filepath.Join(output, "search.tar.gz"), count)
		e, err := search.Open(filepath.Join(snapshot, "index"), snapshot)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = e.Close() }()
		r := httptest.NewRecorder()
		web.Handler(e).ServeHTTP(r, httptest.NewRequest("GET", "/api/v1/search?q=parsing+libraries&limit=100", nil))
		var response search.Response
		if r.Code != 200 {
			t.Fatal(r.Body.String())
		}
		if err := json.Unmarshal(r.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if len(response.Hits) != 2 {
			t.Fatal("lost project memberships")
		}
		if baseline == nil {
			baseline = response.Hits
		} else {
			assertPopularityRanking(t, baseline, response.Hits)
		}
	}
	output := filepath.Join(t.TempDir(), "invalid")
	if err := Run(snapshotArgs(t, packFixture(t, popularityArchive(t, -1), nil), output), io.Discard); err == nil {
		t.Fatal("negative usage accepted")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("invalid release published")
	}
}

func assertPopularityRanking(t *testing.T, baseline, hits []search.Hit) {
	t.Helper()
	if hits[0].ID != popularityFixtureProject {
		t.Fatal("popularity not used by the HTTP search endpoint")
	}
	for _, hit := range hits {
		for i, previous := range baseline {
			if hit.ID != previous.ID {
				continue
			}
			want := 1 / float64(60+i+1)
			if hit.ID == popularityFixtureProject {
				want += 1.0 / 61
			}
			if hit.Lexical != previous.Lexical || math.Abs(hit.Score-want) > 1e-12 {
				t.Fatalf("incorrect API scores: %+v", hit)
			}
		}
	}
}

func assertBundlePopularity(t *testing.T, path string, count int) {
	t.Helper()
	f, err := os.Open(path)
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
	for {
		h, err := tr.Next()
		if err != nil {
			t.Fatalf("catalogue missing from bundle: %v", err)
		}
		if h.Name != "serving-catalog.ndjson" {
			continue
		}
		d := json.NewDecoder(tr)
		seen := 0
		for {
			var record search.CatalogRecord
			if err := d.Decode(&record); err == io.EOF {
				break
			} else if err != nil {
				t.Fatal(err)
			}
			want := 0.0
			if count > 0 && record.ID == popularityFixtureProject {
				want = .5
			}
			if record.DependentReposPercentile != want {
				t.Fatalf("%s: got %g want %g", record.ID, record.DependentReposPercentile, want)
			}
			seen++
		}
		if seen != 2 {
			t.Fatal("catalogue incomplete")
		}
		return
	}
}
