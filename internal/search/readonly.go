package search

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/blevesearch/bleve/v2"
)

func openSearchIndex(path string) (bleve.Index, error) {
	// Scorch can create a missing Bolt file even with read_only enabled.
	info, err := os.Stat(filepath.Join(path, "store", "root.bolt"))
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("index root.bolt is not a regular file")
	}
	return bleve.OpenUsing(path, map[string]interface{}{"read_only": true})
}
