package search

import (
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/ecosyste-ms/search/internal/project"
)

func TestMappingWithoutDocValues(t *testing.T) {
	m := Mapping()
	for name, property := range m.DefaultMapping.Properties {
		for _, field := range property.Fields {
			if field.DocValues || field.Store || field.IncludeTermVectors || field.IncludeInAll || field.SkipFreqNorm || !field.Index {
				t.Errorf("unexpected index options for %s: %+v", name, field)
			}
		}
	}
	if m.DefaultMapping.Dynamic || m.ScoringModel != "bm25" || m.AnalyzerNameForPath("descriptions") != "en" {
		t.Fatal("analysis or scoring configuration changed")
	}
}

func TestDocValuesPreserveScoresAndOrder(t *testing.T) {
	const scopedName, clientName = "@babel/core", "requests"
	const babelName = "babel"
	projects := []project.Project{
		{ID: babelName, Name: babelName, Names: []string{babelName, scopedName}, Descriptions: []string{"JavaScript compiler and parsing libraries"}, Ecosystems: []string{"npm"}},
		{ID: clientName, Name: clientName, Names: []string{clientName}, Descriptions: []string{"HTTP client library for Python"}, Ecosystems: []string{"pypi"}},
		{ID: "http", Name: "http-tools", Names: []string{"http-tools"}, Descriptions: []string{"Testing tools for HTTP client libraries"}, Ecosystems: []string{"npm"}},
	}
	queries := []string{scopedName, clientName, "http client", "parsing libraries", "testing", "library"}
	var baseline [][]Hit
	for _, docValues := range []bool{true, false} {
		m := Mapping()
		for _, name := range append([]string{"exact"}, FacetFields...) {
			m.DefaultMapping.Properties[name].Fields[0].DocValues = docValues
		}
		idx, err := bleve.New(filepath.Join(t.TempDir(), "index"), m)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = idx.Close() })
		batch := idx.NewBatch()
		for _, p := range projects {
			if err := batch.Index(p.ID, Document(p, project.Metadata{Activity: "active"})); err != nil {
				t.Fatal(err)
			}
		}
		if err := idx.Batch(batch); err != nil {
			t.Fatal(err)
		}
		var responses [][]Hit
		for _, q := range queries {
			r, err := idx.Search(bleve.NewSearchRequestOptions(mappedLexicalQuery(q, m), 100, 0, false))
			if err != nil {
				t.Fatal(err)
			}
			if len(r.Hits) == 0 {
				t.Fatalf("no results for %q", q)
			}
			hits := make([]Hit, 0, len(r.Hits))
			for _, h := range r.Hits {
				hits = append(hits, Hit{ID: h.ID, Score: h.Score})
			}
			sortTiedHits(hits)
			responses = append(responses, hits)
		}
		if docValues {
			baseline = responses
		} else if !reflect.DeepEqual(baseline, responses) {
			t.Fatalf("doc values changed ranking: before=%+v after=%+v", baseline, responses)
		}
	}
}

func sortTiedHits(hits []Hit) {
	// Equal scores have no fixed order across builds.
	for start := 0; start < len(hits); {
		end := start + 1
		for end < len(hits) && hits[end].Score == hits[start].Score {
			end++
		}
		slices.SortFunc(hits[start:end], func(a, b Hit) int { return strings.Compare(a.ID, b.ID) })
		start = end
	}
}
