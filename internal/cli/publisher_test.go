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
	"github.com/ecosyste-ms/search/internal/web"
)

func TestPublisherArchivesThroughCLI(t *testing.T) {
	const expectedID = "package:1"
	for _, name := range []string{"replay", "database"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "publisher", name+".tar.gz"))
			if err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(t.TempDir(), "release")
			if err := Run(snapshotArgs(t, data, output), io.Discard); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(output, "snapshot")
			e, err := search.Open(filepath.Join(dir, "index"), dir)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = e.Close() }()
			h := web.Handler(e)
			r := httptest.NewRecorder()
			h.ServeHTTP(r, httptest.NewRequest("GET", "/api/v1/search?q=%40example%2Ftransfer", nil))
			var result search.Response
			if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if r.Code != 200 || len(result.Hits) != 1 || result.Hits[0].ID != expectedID || result.Hits[0].Identity == nil {
				t.Fatalf("publisher package missing: %d %s", r.Code, r.Body.String())
			}
			if !strings.Contains(string(fixtureJSON(t, result.Hits[0].Public)), "pkg:npm/%40example/transfer") || !strings.Contains(r.Body.String(), "npmjs.org") {
				t.Fatalf("publisher public identifiers missing: %d %s", r.Code, r.Body.String())
			}
			if _, err := os.Stat(filepath.Join(output, "search.tar.gz")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSnapshotRejectsInvalidReports(t *testing.T) {
	for _, report := range []string{"corpus.json", "identity.json", "selection.json", "selection.ndjson.gz"} {
		for _, problem := range []string{"report-checksum", "size", "unlisted", "missing", "unknown", "missing-required"} {
			t.Run(report+"/"+problem, func(t *testing.T) {
				files := archiveFixture(t)
				files[report] = []byte("{}")
				data := packFixture(t, files, func(m map[string]any) {
					metadata := m["files"].(map[string]any)
					info := metadata[report].(map[string]any)
					switch problem {
					case "report-checksum":
						info["sha256"] = strings.Repeat("0", 64)
					case "size":
						info["bytes"] = 1
					case "unlisted":
						delete(metadata, report)
					case "missing":
						delete(files, report)
					case "unknown":
						delete(metadata, report)
						metadata["unknown.json"] = info
					case "missing-required":
						delete(files, "excluded.json")
						delete(metadata, "excluded.json")
					}
				})
				root := t.TempDir()
				previous := filepath.Join(root, "previous")
				if err := os.WriteFile(previous, []byte("working"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := Run(snapshotArgs(t, data, filepath.Join(root, "new")), io.Discard); err == nil {
					t.Fatal("invalid archive accepted")
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
}
