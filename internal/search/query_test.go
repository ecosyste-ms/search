package search

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/blevesearch/bleve/v2"
)

func expandedCandidateFixture(t *testing.T) *Engine {
	t.Helper()
	idx, err := bleve.New(filepath.Join(t.TempDir(), "index"), Mapping())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	e := &Engine{Index: idx, Positions: map[string]uint32{}, Compact: &compactCatalog{}}
	e.Catalog.Values[0] = [][]string{{"swiftpm"}, {"hex"}, {"hackage"}}
	batch := idx.NewBatch()
	for i := 0; i < 1201; i++ {
		id := fmt.Sprintf("project:%04d", i)
		name, names, category := "background", []string{"background"}, uint16(0)
		if i < 200 {
			name, names = "needle helper", []string{"needle helper"}
		}
		if i == 200 {
			name, category = "needle", 1
			names = []string{name}
			for j := 0; j < 1000; j++ {
				names = append(names, fmt.Sprintf("component-%d", j))
			}
		}
		if i == 199 {
			category = 2
			names = []string{"needle helper" + strings.Repeat(" additional", 30)}
		}
		if err := batch.Index(id, map[string]any{"names": names, "exact": names, "ecosystem": e.Catalog.Values[0][category]}); err != nil {
			t.Fatal(err)
		}
		e.Positions[id] = uint32(i)
		e.Compact.Cards = append(e.Compact.Cards, compactCard{ID: id, Name: name, Description: "Package description"})
		e.Catalog.Rows = append(e.Catalog.Rows, [4]uint16{category})
	}
	if err := idx.Batch(batch); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(e.Catalog.Values); i++ {
		e.Catalog.Values[i] = [][]string{{}}
	}
	return e
}

func TestSearchOrdersExpandedCandidatesBeforeLimiting(t *testing.T) {
	e := expandedCandidateFixture(t)
	raw, err := e.Index.Search(bleve.NewSearchRequestOptions(mappedLexicalQuery("needle", e.Index.Mapping()), 500, 0, false))
	if err != nil {
		t.Fatal(err)
	}
	position := 0
	for i, h := range raw.Hits {
		if h.ID == "project:0200" {
			position = i + 1
		}
	}
	if position <= 100 {
		t.Fatalf("fixture must put the exact name beyond the old pool: %d", position)
	}
	large, err := e.Search(context.Background(), "needle", "", true, 100)
	if err != nil {
		t.Fatal(err)
	}
	small, err := e.Search(context.Background(), "needle", "", true, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(large.Hits) != 100 || large.Hits[0].ID != "project:0200" || !reflect.DeepEqual(small.Hits, large.Hits[:10]) {
		t.Fatal("exact match or display limit lost")
	}
	if !reflect.DeepEqual(large.Suggestions["ecosystem"], []string{"hex", "swiftpm"}) || !reflect.DeepEqual(small.Suggestions, large.Suggestions) {
		t.Fatalf("suggestions must describe selected top 100: %v", large.Suggestions)
	}
	for _, hit := range large.Hits {
		if hit.Name == "" || hit.Description != "Package description" || hit.Score != hit.Lexical {
			t.Fatalf("display data or BM25 score lost: %+v", hit)
		}
	}
	filtered, err := e.Search(context.Background(), "needle", "hackage", true, 100)
	if err != nil || len(filtered.Hits) != 1 || filtered.Hits[0].ID != "project:0199" {
		t.Fatalf("filter must apply before retrieval: %+v, %v", filtered, err)
	}
}
