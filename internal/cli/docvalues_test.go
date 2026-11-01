package cli

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/ecosyste-ms/search/internal/search"
	"github.com/ecosyste-ms/search/internal/web"
)

func assertSnapshotWithoutDocValues(t *testing.T, e *search.Engine) {
	t.Helper()
	const unknown = "unknown"
	const packageName, projectID = "curl", "project:curl"
	for name, property := range e.Index.Mapping().(*mapping.IndexMappingImpl).DefaultMapping.Properties {
		for _, field := range property.Fields {
			if field.DocValues {
				t.Fatalf("snapshot still writes doc values for %s", name)
			}
		}
	}
	for field, value := range map[string]string{"exact": packageName, "ecosystem": "nixpkgs", "language": unknown, "license": unknown, "activity": unknown} {
		q := bleve.NewTermQuery(value)
		q.SetField(field)
		r, err := e.Index.Search(bleve.NewSearchRequest(q))
		if err != nil {
			t.Fatal(err)
		}
		want := 2
		if field == "exact" || field == "ecosystem" {
			want = 1
		}
		if len(r.Hits) != want || (want == 1 && r.Hits[0].ID != projectID) {
			t.Errorf("filter %s=%s returned unexpected projects: %+v", field, value, r.Hits)
		}
	}
	h := web.Handler(e)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/api/v1/search?q=curl", nil))
	var response search.Response
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	wantSuggestions := map[string][]string{"ecosystem": {"nixpkgs"}, "language": {unknown}, "license": {unknown}, "activity": {unknown}}
	if rr.Code != 200 || !reflect.DeepEqual(response.Suggestions, wantSuggestions) || !response.SuggestionsPartial {
		t.Fatalf("candidate suggestions changed: %s", rr.Body.String())
	}
}
