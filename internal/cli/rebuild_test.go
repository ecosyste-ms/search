package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ecosyste-ms/search/internal/search"
	"github.com/ecosyste-ms/search/internal/storage"
	"github.com/ecosyste-ms/search/internal/web"
)

func compareRankings(before, after []search.Hit, limit int) error {
	if len(before) != len(after) {
		return fmt.Errorf("result count changed: %d to %d", len(before), len(after))
	}
	scores := make(map[string]float64, len(before))
	for i, hit := range before {
		scores[hit.ID] = hit.Score
		if hit.Score != after[i].Score {
			return fmt.Errorf("score at position %d changed: %g to %g", i+1, hit.Score, after[i].Score)
		}
	}
	for _, hit := range after {
		if score, ok := scores[hit.ID]; ok && score != hit.Score {
			return fmt.Errorf("score for %s changed: %g to %g", hit.ID, score, hit.Score)
		}
	}
	for start := 0; start < len(before); {
		end := start + 1
		for end < len(before) && before[end].Score == before[start].Score {
			end++
		}
		// The final score group may straddle the candidate cutoff.
		if end == limit {
			break
		}
		a, b := []string{}, []string{}
		for i := start; i < end; i++ {
			a, b = append(a, before[i].ID), append(b, after[i].ID)
		}
		slices.Sort(a)
		slices.Sort(b)
		if !slices.Equal(a, b) {
			return fmt.Errorf("projects changed in score group %g: %v to %v", before[start].Score, a, b)
		}
		start = end
	}
	return nil
}

func TestCompareRankings(t *testing.T) {
	a, b, c := search.Hit{ID: "a", Score: 2}, search.Hit{ID: "b", Score: 1}, search.Hit{ID: "c", Score: 1}
	for _, tc := range []struct {
		name          string
		before, after []search.Hit
		limit         int
		wantError     bool
	}{
		{"identical", []search.Hit{a, b}, []search.Hit{a, b}, 3, false},
		{"tied order", []search.Hit{a, b, c}, []search.Hit{a, c, b}, 4, false},
		{"cutoff tie", []search.Hit{a, b}, []search.Hit{a, c}, 2, false},
		{"missing result", []search.Hit{a, b}, []search.Hit{a}, 3, true},
		{"missing inside limit", []search.Hit{a, b}, []search.Hit{a, c}, 3, true},
		{"unequal swap", []search.Hit{a, b}, []search.Hit{b, a}, 3, true},
		{"score drift", []search.Hit{a}, []search.Hit{{ID: "a", Score: 3}}, 1, true},
		{"shared cutoff score drift", []search.Hit{a, b}, []search.Hit{{ID: "b", Score: 2}, c}, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := compareRankings(tc.before, tc.after, tc.limit); (err != nil) != tc.wantError {
				t.Fatalf("unexpected comparison: %v", err)
			}
		})
	}
}

func TestSnapshotRebuildRanking(t *testing.T) {
	root := t.TempDir()
	args := snapshotArgs(t, packFixture(t, ecosystemFixture(t), nil), filepath.Join(root, "first"))
	queries := []string{"http", "quiet-client", "client library", "missing"}
	if archive := os.Getenv("SEARCH_REBUILD_ARCHIVE"); archive != "" {
		archive, err := filepath.Abs(archive)
		if err != nil {
			t.Fatal(err)
		}
		sha, err := storage.FileSHA(archive)
		if err != nil {
			t.Fatal(err)
		}
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, archive) }))
		defer s.Close()
		args[2], args[4] = s.URL+"/corpus.tar.gz", sha
		var probes []struct{ Query string }
		if err := storage.ReadJSON("testdata/queries.json", &probes); err != nil {
			t.Fatal(err)
		}
		queries = nil
		for _, probe := range probes {
			queries = append(queries, probe.Query)
		}
	}
	var baseline map[string][]search.Hit
	for _, name := range []string{"first", "second"} {
		args[6] = filepath.Join(root, name)
		if err := Run(args, io.Discard); err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(args[6], "snapshot")
		current := captureRankings(t, dir, queries)
		if baseline == nil {
			baseline = current
			continue
		}
		for query, before := range baseline {
			if err := compareRankings(before, current[query], 100); err != nil {
				t.Errorf("%q: %v", query, err)
			}
		}
	}
	t.Logf("Compared %d queries across two CLI snapshot builds", len(baseline))
}

func captureRankings(t *testing.T, dir string, queries []string) map[string][]search.Hit {
	t.Helper()
	e, err := search.Open(filepath.Join(dir, "index"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = e.Close() }()
	h := web.Handler(e)
	results := make(map[string][]search.Hit, len(queries))
	for _, q := range queries {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", "/api/v1/search?limit=100&q="+url.QueryEscape(q), nil))
		if r.Code != 200 {
			t.Fatalf("%q: %d %s", q, r.Code, r.Body.String())
		}
		var response search.Response
		if err := json.Unmarshal(r.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		results[q] = response.Hits
	}
	return results
}
