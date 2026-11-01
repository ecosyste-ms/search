package search

import (
	"context"
	"slices"
	"strings"

	"github.com/blevesearch/bleve/v2"
)

func (e *Engine) preferExact(ctx context.Context, text string, hits []Hit) error {
	if len(hits) < 2 {
		return nil
	}
	ids := make([]string, len(hits))
	for i, h := range hits {
		ids[i] = h.ID
	}
	q := bleve.NewTermQuery(strings.ToLower(strings.TrimSpace(text)))
	q.SetField("exact")
	// Restrict the lookup to candidates so common exact names cannot hide a candidate.
	req := bleve.NewSearchRequestOptions(bleve.NewConjunctionQuery(q, bleve.NewDocIDQuery(ids)), len(ids), 0, false)
	req.Score = bleve.ScoreNone
	found, err := e.Index.SearchInContext(ctx, req)
	if err != nil {
		return err
	}
	exact := make(map[string]bool, len(found.Hits))
	for _, h := range found.Hits {
		exact[h.ID] = true
	}
	slices.SortStableFunc(hits, func(a, b Hit) int {
		if exact[a.ID] == exact[b.ID] {
			return 0
		}
		if exact[a.ID] {
			return -1
		}
		return 1
	})
	return nil
}
