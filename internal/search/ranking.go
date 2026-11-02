package search

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/blevesearch/bleve/v2"
)

const rrfK = 60

func (e *Engine) fuseRanks(hits []Hit) error {
	type observation struct {
		position int
		value    float64
	}
	var positive []observation
	for i, hit := range hits {
		ordinal, err := e.Ordinal(hit.ID)
		if err != nil {
			return err
		}
		if value := e.Compact.Cards[ordinal].DependentReposPercentile; value > 0 {
			if positive == nil {
				positive = make([]observation, 0, len(hits))
			}
			positive = append(positive, observation{position: i, value: value})
		}
	}
	if len(positive) == 0 {
		return nil
	}
	slices.SortStableFunc(positive, func(a, b observation) int { return cmp.Compare(b.value, a.value) })
	for i := range hits {
		hits[i].Score = 1 / float64(rrfK+i+1)
	}
	rank, previous := 0, 0.0
	for i, item := range positive {
		if item.value != previous {
			rank, previous = i+1, item.value
		}
		hits[item.position].Score += 1 / float64(rrfK+rank)
	}
	slices.SortStableFunc(hits, func(a, b Hit) int { return cmp.Compare(b.Score, a.Score) })
	return nil
}

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
