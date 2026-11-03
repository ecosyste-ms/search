package search

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCatalogDependentPercentile(t *testing.T) {
	for _, tc := range []struct {
		value   string
		want    float64
		invalid bool
	}{
		{"", 0, true}, {`,"dependent_repos_percentile":0`, 0, false}, {`,"dependent_repos_percentile":0.5`, .5, false},
		{`,"dependent_repos_percentile":1`, 1, false}, {`,"dependent_repos_percentile":null`, 0, true},
		{`,"dependent_repos_percentile":-0.1`, 0, true}, {`,"dependent_repos_percentile":1.01`, 0, true},
		{`,"dependent_repos_percentile":"0.5"`, 0, true},
	} {
		t.Run(tc.value, func(t *testing.T) {
			data := []byte(`{"id":"project:1","name":"example","description":"Example package","filters":{},"public":{"packages":[],"verified_repositories":[]}}`)
			data = append(data[:len(data)-1], []byte(tc.value+`}`)...)
			path := filepath.Join(t.TempDir(), "serving-catalog.ndjson")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			var manifest compactExport
			if err := json.Unmarshal([]byte(fmt.Sprintf(`{"projects":1,"files":{"serving-catalog.ndjson":{"sha256":"%x"}}}`, sha256.Sum256(data))), &manifest); err != nil {
				t.Fatal(err)
			}
			e, err := readServingCatalog(path, manifest)
			if tc.invalid {
				if err == nil {
					_ = e.closeDetails()
					t.Fatal("invalid percentile accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = e.closeDetails() }()
			if e.Compact.Cards[0].DependentReposPercentile != tc.want {
				t.Fatal("percentile lost")
			}
		})
	}
}
