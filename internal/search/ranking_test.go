package search

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func fusionFixture(values []float64) (*Engine, []Hit) {
	e := &Engine{Positions: map[string]uint32{}, Compact: &compactCatalog{}}
	hits := []Hit{}
	for i, value := range values {
		id := fmt.Sprintf("project:%d", i)
		e.Positions[id] = uint32(i)
		e.Compact.Cards = append(e.Compact.Cards, compactCard{DependentReposPercentile: value})
		hits = append(hits, Hit{ID: id, Lexical: float64(100 - i), Score: float64(100 - i)})
	}
	return e, hits
}

func TestRankFusion(t *testing.T) {
	e, hits := fusionFixture([]float64{0, .8, .8, .4})
	scaled := slices.Clone(hits)
	for i := range scaled {
		scaled[i].Lexical *= 100
		scaled[i].Score *= 100
	}
	if err := e.fuseRanks(hits); err != nil {
		t.Fatal(err)
	}
	if err := e.fuseRanks(scaled); err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{"project:0": 1.0 / 61, "project:1": 1.0/62 + 1.0/61, "project:2": 1.0/63 + 1.0/61, "project:3": 1.0/64 + 1.0/63}
	for i, hit := range hits {
		if math.Abs(hit.Score-want[hit.ID]) > 1e-12 || hit.Score != scaled[i].Score || hit.ID != scaled[i].ID || hit.Lexical*100 != scaled[i].Lexical {
			t.Fatalf("incorrect fusion: %+v", hits)
		}
	}
}

func TestRankFusionFallbackAndTies(t *testing.T) {
	for _, values := range [][]float64{nil, {0, 0, 0}, {.1, 1}} {
		e, hits := fusionFixture(values)
		before := slices.Clone(hits)
		if err := e.fuseRanks(hits); err != nil {
			t.Fatal(err)
		}
		for i, h := range hits {
			if h.ID != before[i].ID {
				t.Fatal("unstable tie or fallback")
			}
			if values[i] == 0 && h.Score != before[i].Score {
				t.Fatal("missing usage changed scores")
			}
		}
	}
	e, hits := fusionFixture([]float64{1})
	delete(e.Positions, hits[0].ID)
	if err := e.fuseRanks(hits); err == nil {
		t.Fatal("unknown candidate accepted")
	}
}

func TestSearchFusionBeforeResultLimit(t *testing.T) {
	e := expandedCandidateFixture(t)
	before, err := e.Search(context.Background(), "needle", "", true, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range before.Hits {
		if hit.ID == expandedPopularID {
			t.Fatal("fixture must place popular project beyond 100")
		}
	}
	e.Compact.Cards[199].DependentReposPercentile = 1
	result, err := e.Search(context.Background(), "needle", "", true, 100)
	if err != nil {
		t.Fatal(err)
	}
	if result.Hits[0].ID != expandedExactID || result.Hits[1].ID != expandedPopularID {
		t.Fatal("exact precedence or expanded-pool fusion failed", result.Hits[:2])
	}
	if !slices.Equal(result.Suggestions["ecosystem"], []string{expandedPopularEcosystem, expandedExactEcosystem, expandedCommonEcosystem}) {
		t.Fatal("suggestions ignore fusion", result.Suggestions)
	}
	filtered, err := e.Search(context.Background(), "needle", expandedPopularEcosystem, true, 10)
	if err != nil || len(filtered.Hits) != 1 || filtered.Hits[0].Score != 2.0/61 || filtered.Hits[0].Lexical != result.Hits[1].Lexical {
		t.Fatalf("filter applied after fusion: %+v %v", filtered, err)
	}
}

func BenchmarkSearchRankFusion(b *testing.B) {
	dir := os.Getenv("SEARCH_SNAPSHOT")
	if dir == "" {
		b.Skip("set SEARCH_SNAPSHOT to an enriched snapshot")
	}
	e, err := Open(filepath.Join(dir, "index"), dir)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	values := make([]float64, len(e.Compact.Cards))
	for i, card := range e.Compact.Cards {
		values[i] = card.DependentReposPercentile
	}
	if slices.Max(values) == 0 {
		b.Fatal("snapshot has no popularity data")
	}
	for _, text := range []string{"numpy", "http client", "library"} {
		for _, mode := range []string{"bm25", "rrf"} {
			b.Run(text+"/"+mode, func(b *testing.B) {
				for i := range e.Compact.Cards {
					e.Compact.Cards[i].DependentReposPercentile = 0
					if mode == "rrf" {
						e.Compact.Cards[i].DependentReposPercentile = values[i]
					}
				}
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if _, err := e.Search(context.Background(), text, "", true, 100); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
