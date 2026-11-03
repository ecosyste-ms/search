package snapshot

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/ecosyste-ms/search/internal/project"
	"github.com/ecosyste-ms/search/internal/search"
	"github.com/ecosyste-ms/search/internal/storage"
)

type publishedManifest struct {
	Snapshot search.SnapshotInfo `json:"snapshot"`
	Projects int                 `json:"projects"`
	Packages int                 `json:"packages"`
}

type publishedProject struct {
	DependentReposPercentile float64             `json:"dependent_repos_percentile"`
	ID                       string              `json:"id"`
	Name                     string              `json:"name"`
	Description              string              `json:"description"`
	Names                    []string            `json:"names"`
	Descriptions             []string            `json:"descriptions"`
	Aliases                  []string            `json:"aliases"`
	Filters                  map[string][]string `json:"filters"`
	Identifiers              *struct {
		Packages     []int               `json:"package_ids"`
		PURLs        [][]json.RawMessage `json:"package_purls"`
		Repositories []string            `json:"repositories"`
	} `json:"identifiers"`
	Public *search.PublicProject `json:"public"`
}

func validSHA(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

func scanPublished(path string, consume func(publishedProject, []byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer func() { _ = gz.Close() }()
	s := bufio.NewScanner(gz)
	s.Buffer(make([]byte, 64<<10), 32<<20)
	line := 0
	for s.Scan() {
		line++
		var p publishedProject
		if err := json.Unmarshal(s.Bytes(), &p); err != nil {
			return fmt.Errorf("catalogue line %d: %w", line, err)
		}
		if err := consume(p, s.Bytes()); err != nil {
			return fmt.Errorf("catalogue line %d: %w", line, err)
		}
	}
	return s.Err()
}

func validatePublicMembership(p publishedProject, raw []byte, seen map[int]bool) error {
	if p.Identifiers == nil || p.Public == nil || p.Public.Evidence == "" {
		return fmt.Errorf("identifiers and public grouping evidence required")
	}
	ids := map[int]bool{}
	for _, id := range p.Identifiers.Packages {
		if id <= 0 || ids[id] || seen[id] {
			return fmt.Errorf("invalid or repeated package membership %d", id)
		}
		ids[id] = true
	}
	if len(ids) != len(p.Public.Packages) || len(ids) != len(p.Identifiers.PURLs) {
		return fmt.Errorf("package identity counts disagree")
	}
	purls := map[int]search.PublicPURL{}
	for _, pair := range p.Identifiers.PURLs {
		var id int
		var purl search.PublicPURL
		if len(pair) != 2 {
			return fmt.Errorf("invalid package/PURL pair")
		}
		if err := json.Unmarshal(pair[0], &id); err != nil {
			return err
		}
		if err := json.Unmarshal(pair[1], &purl); err != nil {
			return err
		}
		if _, ok := purls[id]; ok || !ids[id] {
			return fmt.Errorf("duplicate or unknown PURL membership")
		}
		purls[id] = purl
	}
	var presence struct {
		Public struct {
			Packages []map[string]json.RawMessage `json:"packages"`
		} `json:"public"`
	}
	if err := json.Unmarshal(raw, &presence); err != nil {
		return err
	}
	for i, pkg := range p.Public.Packages {
		if !ids[pkg.SnapshotID] || seen[pkg.SnapshotID] {
			return fmt.Errorf("duplicate or unknown public package %d", pkg.SnapshotID)
		}
		if pkg.Name == "" || pkg.Registry == "" || pkg.Ecosystem == "" {
			return fmt.Errorf("public package names, registries and ecosystems required")
		}
		if _, ok := presence.Public.Packages[i]["purl"]; !ok {
			return fmt.Errorf("PURL must be supplied or explicitly null")
		}
		if pkg.PURL != purls[pkg.SnapshotID] {
			return fmt.Errorf("package PURLs disagree")
		}
		if err := validatePURL(pkg.PURL); err != nil {
			return err
		}
		seen[pkg.SnapshotID] = true
	}
	return validatePublicRepositories(p)
}

func validatePublicRepositories(p publishedProject) error {
	roots := []string{}
	for _, repo := range p.Public.VerifiedRepositories {
		root, ok := project.RepositoryRoot(repo)
		if !ok || !strings.HasPrefix(repo, "https://") {
			return fmt.Errorf("invalid verified repository URL")
		}
		if len(p.Public.Packages) > 0 {
			matched := false
			for _, pkg := range p.Public.Packages {
				declared, a := project.RepositoryRoot(pkg.DeclaredRepository)
				cached, b := project.RepositoryRoot(pkg.CachedRepository)
				if a && b && declared == root && cached == root {
					matched = true
					break
				}
			}
			if !matched {
				return fmt.Errorf("verified repository lacks matching package evidence")
			}
		}
		roots = append(roots, root)
	}
	if len(p.Public.Packages) == 0 && len(roots) == 0 {
		return fmt.Errorf("project has no public identity")
	}
	if len(project.Unique(slices.Clone(roots))) != len(roots) || !slices.Equal(project.Unique(roots), project.Unique(slices.Clone(p.Identifiers.Repositories))) {
		return fmt.Errorf("repository identities disagree")
	}
	return nil
}

func validatePublished(path string, m publishedManifest) error {
	projects := map[string]bool{}
	packages := map[int]bool{}
	err := scanPublished(path, func(p publishedProject, raw []byte) error {
		if p.DependentReposPercentile < 0 || p.DependentReposPercentile > 1 {
			return fmt.Errorf("dependent repository percentile must be between zero and one")
		}
		if p.ID == "" || p.Name == "" || len(p.Names) == 0 || projects[p.ID] {
			return fmt.Errorf("missing or duplicate project identity")
		}
		projects[p.ID] = true
		for _, field := range search.FacetFields {
			if len(p.Filters[field]) == 0 {
				return fmt.Errorf("missing filter field %s", field)
			}
		}
		if len(p.Filters["activity"]) != 1 {
			return fmt.Errorf("one activity value required")
		}
		if err := validatePublicMembership(p, raw, packages); err != nil {
			return err
		}
		if len(projects) > m.Projects || len(packages) > m.Packages {
			return fmt.Errorf("snapshot exceeds declared counts")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(projects) != m.Projects || len(packages) != m.Packages {
		return fmt.Errorf("snapshot project/package counts disagree with manifest")
	}
	return nil
}

func buildPublished(input, output string, m publishedManifest) error {
	if err := os.Mkdir(output, 0755); err != nil {
		return err
	}
	idx, err := bleve.New(filepath.Join(output, "index"), search.Mapping())
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = idx.Close()
		}
	}()
	catalogPath := filepath.Join(output, "serving-catalog.ndjson")
	f, err := os.Create(catalogPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	encoder := json.NewEncoder(f)
	batch := idx.NewBatch()
	err = scanPublished(input, func(row publishedProject, _ []byte) error {
		p := project.Project{ID: row.ID, Name: row.Name, Names: row.Names, Descriptions: row.Descriptions, Aliases: row.Aliases, Ecosystems: row.Filters["ecosystem"], Languages: row.Filters["language"], Licenses: row.Filters["license"]}
		if err := batch.Index(p.ID, search.Document(p, project.Metadata{Activity: row.Filters["activity"][0]})); err != nil {
			return err
		}
		if batch.Size() == 1000 {
			if err := idx.Batch(batch); err != nil {
				return err
			}
			batch = idx.NewBatch()
		}
		return encoder.Encode(search.CatalogRecord{ID: p.ID, Name: p.Name, Description: row.Description, Filters: row.Filters, Public: row.Public, DependentReposPercentile: row.DependentReposPercentile})
	})
	if err != nil {
		return err
	}
	if batch.Size() > 0 {
		if err := idx.Batch(batch); err != nil {
			return err
		}
	}
	err = idx.Close()
	closed = true
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	hashes, err := search.IndexFileHashes(filepath.Join(output, "index"))
	if err != nil {
		return err
	}
	if err := storage.WriteJSON(filepath.Join(output, "index-checksums.json"), hashes); err != nil {
		return err
	}
	files := map[string]any{}
	for _, name := range []string{"index-checksums.json", "serving-catalog.ndjson"} {
		sha, err := storage.FileSHA(filepath.Join(output, name))
		if err != nil {
			return err
		}
		files[name] = map[string]string{"sha256": sha}
	}
	manifest := map[string]any{"projects": m.Projects, "packages": m.Packages, "public_entities": true, "snapshot": m.Snapshot, "source": map[string]string{"sha256": m.Snapshot.SourceSHA256}, "files": files}
	if err := storage.WriteJSON(filepath.Join(output, "manifest.json"), manifest); err != nil {
		return err
	}
	return filepath.WalkDir(output, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.Chmod(path, 0755)
		}
		return os.Chmod(path, 0444)
	})
}

func validatePURL(purl search.PublicPURL) error {
	if purl == "" {
		return nil
	}
	u, err := url.Parse(string(purl))
	if err != nil || u.Scheme != "pkg" || !strings.Contains(u.Opaque, "/") || strings.HasSuffix(u.Opaque, "/") || strings.ContainsAny(string(purl), " \r\n\t") {
		return fmt.Errorf("invalid package PURL")
	}
	if _, err := url.PathUnescape(string(purl)); err != nil {
		return fmt.Errorf("invalid PURL escaping")
	}
	return nil
}
