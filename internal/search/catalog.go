package search

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ecosyste-ms/search/internal/project"
	"github.com/ecosyste-ms/search/internal/storage"
)

type compactCard struct {
	DependentReposPercentile float64
	ID, Name, Description    string
	Entity                   *entityCard
	Offset, Length           int64
}

type compactCatalog struct {
	Cards      []compactCard
	File       *os.File
	Source     *project.CorpusSource
	PURLSource string
}

type compactExport struct {
	Snapshot       *SnapshotInfo         `json:"snapshot,omitempty"`
	Projects       int                   `json:"projects"`
	PublicEntities bool                  `json:"public_entities"`
	Source         *project.CorpusSource `json:"source"`
	PURLSource     string                `json:"purl_source_sha256"`
	Files          map[string]struct {
		SHA256 string `json:"sha256"`
	} `json:"files"`
}

type CatalogRecord struct {
	DependentReposPercentile float64             `json:"dependent_repos_percentile"`
	ID                       string              `json:"id"`
	Name                     string              `json:"name"`
	Filters                  map[string][]string `json:"filters"`
	Public                   *PublicProject      `json:"public"`
	Description              string              `json:"description"`
}

func readServingCatalog(path string, manifest compactExport) (*Engine, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	keepFile := false
	defer func() {
		if !keepFile {
			_ = f.Close()
		}
	}()
	hash := sha256.New()
	reader := io.TeeReader(f, hash)
	e := &Engine{Count: manifest.Projects, Positions: make(map[string]uint32, manifest.Projects), Compact: &compactCatalog{Cards: make([]compactCard, 0, manifest.Projects)}, Catalog: CandidateCatalog{Rows: make([][4]uint16, 0, manifest.Projects)}}
	e.registrySizes = map[registryKey]int{}
	e.ecosystemSizes = map[string]int{}
	var dictionaries [4]map[string]uint16
	for i := range dictionaries {
		dictionaries[i] = map[string]uint16{}
	}
	decoder := json.NewDecoder(reader)
	interned := map[string]string{}
	intern := func(value string) string {
		if existing, ok := interned[value]; ok {
			return existing
		}
		interned[value] = value
		return value
	}
	for {
		var record struct {
			CatalogRecord
			Percentile *float64 `json:"dependent_repos_percentile"`
		}
		start := decoder.InputOffset()
		if err := decoder.Decode(&record); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		if record.ID == "" || len(e.Compact.Cards) >= e.Count {
			return nil, fmt.Errorf("invalid catalog record")
		}
		if record.Percentile == nil || *record.Percentile < 0 || *record.Percentile > 1 {
			return nil, fmt.Errorf("invalid dependent repository percentile for %s", record.ID)
		}
		if record.Public == nil {
			return nil, fmt.Errorf("public record missing: %s", record.ID)
		}
		if strings.TrimSpace(record.Description) == "" {
			return nil, fmt.Errorf("description missing: %s", record.ID)
		}
		if _, exists := e.Positions[record.ID]; exists {
			return nil, fmt.Errorf("duplicate project %s", record.ID)
		}
		var row [4]uint16
		for i, field := range FacetFields {
			values := project.Unique(slices.Clone(record.Filters[field]))
			key := storage.Fingerprint(values)
			id, exists := dictionaries[i][key]
			if !exists {
				if len(e.Catalog.Values[i]) >= 65536 {
					return nil, fmt.Errorf("too many %s value sets", field)
				}
				id = uint16(len(e.Catalog.Values[i]))
				dictionaries[i][key] = id
				e.Catalog.Values[i] = append(e.Catalog.Values[i], values)
			}
			row[i] = id
		}
		e.Positions[record.ID] = uint32(len(e.Compact.Cards))
		card := compactCard{ID: record.ID, Name: record.Name, Description: record.Description, Offset: start, Length: decoder.InputOffset() - start}
		card.DependentReposPercentile = *record.Percentile
		for _, pkg := range record.Public.Packages {
			key := pkg.registryKey()
			e.registrySizes[key]++
			e.ecosystemSizes[ecosystemLabel(key.ecosystem)]++
		}
		card.Entity = makeEntityCard(record.Public)
		card.Entity.Registry = intern(card.Entity.Registry)
		card.Entity.Ecosystem = intern(card.Entity.Ecosystem)
		if card.Entity.Name == card.Name {
			card.Entity.Name = card.Name
		}
		e.Compact.Cards = append(e.Compact.Cards, card)
		e.Catalog.Rows = append(e.Catalog.Rows, row)
	}
	if len(e.Compact.Cards) != e.Count {
		return nil, fmt.Errorf("catalog count mismatch")
	}
	if fmt.Sprintf("%x", hash.Sum(nil)) != manifest.Files[filepath.Base(path)].SHA256 {
		return nil, fmt.Errorf("catalog checksum mismatch")
	}
	e.Compact.File = f
	keepFile = true
	return e, nil
}

func Open(indexPath, dir string) (*Engine, error) {
	if dir == "" {
		return nil, fmt.Errorf("a public serving catalogue is required")
	}
	// Pin open files and later index reads to the same snapshot during a switch.
	colocated := filepath.Clean(indexPath) == filepath.Join(filepath.Clean(dir), "index")
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	dir = resolved
	if colocated {
		indexPath = filepath.Join(dir, "index")
	} else {
		indexPath, err = filepath.EvalSymlinks(indexPath)
		if err != nil {
			return nil, err
		}
	}
	var manifest compactExport
	if err := storage.ReadJSON(filepath.Join(dir, "manifest.json"), &manifest); err != nil {
		return nil, err
	}
	if manifest.Projects < 1 || !manifest.PublicEntities || manifest.Snapshot == nil {
		return nil, fmt.Errorf("invalid compact export manifest")
	}
	if err := verifySnapshotIndex(indexPath, dir, manifest); err != nil {
		return nil, err
	}
	const name = "serving-catalog.ndjson"
	if _, ok := manifest.Files[name]; !ok {
		return nil, fmt.Errorf("serving-catalog.ndjson required")
	}
	e, err := readServingCatalog(filepath.Join(dir, name), manifest)
	if err != nil {
		return nil, err
	}
	e.Compact.Source = manifest.Source
	e.Snapshot = manifest.Snapshot
	e.Compact.PURLSource = manifest.PURLSource
	if _, hasDetails := manifest.Files["metadata.sqlite"]; hasDetails {
		_ = e.closeDetails()
		return nil, fmt.Errorf("use a standalone public snapshot without SQLite metadata")
	}
	idx, err := openSearchIndex(indexPath)
	if err != nil {
		_ = e.closeDetails()
		return nil, err
	}
	docs, err := idx.DocCount()
	if err != nil || docs != uint64(e.Count) {
		_ = idx.Close()
		_ = e.closeDetails()
		return nil, fmt.Errorf("index/catalog count mismatch: %d vs %d: %v", docs, e.Count, err)
	}
	e.Index = idx
	return e, nil
}

func (e *Engine) closeDetails() error {
	if e.Compact.File != nil {
		return e.Compact.File.Close()
	}
	return nil
}
func (e *Engine) Close() error                      { return errors.Join(e.closeDetails(), e.Index.Close()) }
func (e *Engine) DisplayName(ordinal uint32) string { return e.Compact.Cards[ordinal].Name }
func (e *Engine) Description(_ context.Context, ordinal uint32) (string, error) {
	return cleanDescription(e.Compact.Cards[ordinal].Description), nil
}
func (e *Engine) Entities(ordinal uint32, query string) (map[string]any, error) {
	p, err := e.PublicProject(ordinal)
	if err != nil {
		return nil, err
	}
	response := e.PublicResponse(p, e.Compact.Source, query)
	response["purl_source_sha256"] = e.Compact.PURLSource
	return response, nil
}
