package search

import (
	"context"
	"fmt"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/ecosyste-ms/search/internal/project"
)

const candidateLimit = 500
const resultLimit = 100

type CandidateCatalog struct {
	Rows   [][4]uint16
	Values [4][][]string
}

func (c CandidateCatalog) suggestions(ordinals []uint32) map[string][]string {
	result := map[string][]string{}
	for field, name := range FacetFields {
		seen := map[uint16]bool{}
		values := []string{}
		for _, ordinal := range ordinals {
			id := c.Rows[ordinal][field]
			if !seen[id] {
				seen[id] = true
				values = append(values, c.Values[field][id]...)
			}
		}
		result[name] = project.Unique(values)
	}
	return result
}

type Engine struct {
	Snapshot  *SnapshotInfo
	Index     bleve.Index
	Count     int
	Positions map[string]uint32
	Catalog   CandidateCatalog
	Compact   *compactCatalog
}

func (e *Engine) Ordinal(id string) (uint32, error) {
	if i, ok := e.Positions[id]; ok {
		return i, nil
	}
	return 0, fmt.Errorf("unknown project %s", id)
}

type Response struct {
	Hits               []Hit               `json:"hits"`
	Suggestions        map[string][]string `json:"suggestions,omitempty"`
	SuggestionsPartial bool                `json:"suggestions_partial,omitempty"`
}

func (e *Engine) Search(ctx context.Context, text, ecosystem string, suggestions bool, display int) (Response, error) {
	result := Response{Hits: []Hit{}}
	if strings.TrimSpace(text) == "" || (display != 10 && display != resultLimit) {
		return result, fmt.Errorf("query required and result limit must be 10 or 100")
	}
	q := mappedLexicalQuery(text, e.Index.Mapping())
	if ecosystem = strings.ToLower(strings.TrimSpace(ecosystem)); ecosystem != "" {
		filter := bleve.NewTermQuery(ecosystem)
		filter.SetField("ecosystem")
		filter.SetBoost(0)
		q = bleve.NewConjunctionQuery(q, filter)
	}
	request := bleve.NewSearchRequestOptions(q, candidateLimit, 0, false)
	found, err := e.Index.SearchInContext(ctx, request)
	if err != nil {
		return result, err
	}
	for _, h := range found.Hits {
		result.Hits = append(result.Hits, Hit{ID: h.ID, Lexical: h.Score, Score: h.Score})
	}
	if err := e.fuseRanks(result.Hits); err != nil {
		return result, err
	}
	if err := e.preferExact(ctx, text, result.Hits); err != nil {
		return result, err
	}
	result.Hits = result.Hits[:min(resultLimit, len(result.Hits))]
	ordinals := make([]uint32, 0, len(result.Hits))
	for i := range result.Hits {
		h := &result.Hits[i]
		ordinal, err := e.Ordinal(h.ID)
		if err != nil {
			return result, err
		}
		ordinals = append(ordinals, ordinal)
		if i >= display {
			continue
		}
		description, err := e.Description(ctx, ordinal)
		if err != nil {
			return result, err
		}
		h.Name = e.DisplayName(ordinal)
		h.Description = description
	}
	if suggestions {
		result.Suggestions = e.Catalog.suggestions(ordinals)
		result.SuggestionsPartial = true
	}
	result.Hits = result.Hits[:min(display, len(result.Hits))]
	return result, nil
}
