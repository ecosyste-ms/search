package snapshot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ecosyste-ms/search/internal/project"
	"github.com/ecosyste-ms/search/internal/search"
	"github.com/ecosyste-ms/search/internal/storage"
)

func TestPreparedContent(t *testing.T) {
	dir, baseline := os.Getenv("SEARCH_EXTRACTED_FIXTURE"), os.Getenv("SEARCH_BASELINE_CORPUS")
	if dir == "" || baseline == "" {
		t.Skip("set SEARCH_EXTRACTED_FIXTURE and SEARCH_BASELINE_CORPUS for the real-data preparation check")
	}
	path := filepath.Join(t.TempDir(), "projects.ndjson.gz")
	if _, err := prepareArchive(dir, path, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	fingerprints := map[string]string{}
	if err := scanPublished(path, func(row publishedProject, _ []byte) error {
		p := project.Project{ID: row.ID, Name: row.Name, Names: row.Names, Descriptions: row.Descriptions, Aliases: row.Aliases, Ecosystems: row.Filters["ecosystem"], Languages: row.Filters["language"], Licenses: row.Filters["license"]}
		fingerprints[p.ID] = storage.Fingerprint(search.Document(p, project.Metadata{Activity: row.Filters["activity"][0]}))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var c project.Corpus
	if err := storage.ReadJSON(baseline, &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Projects) != len(fingerprints) {
		t.Fatal("project count changed")
	}
	for _, p := range c.Projects {
		if fingerprints[p.ID] != storage.Fingerprint(search.Document(p, c.Metadata[p.ID])) {
			t.Fatal("indexed content changed", p.ID)
		}
	}
	t.Logf("All indexed fields unchanged across %d projects", len(c.Projects))
}
