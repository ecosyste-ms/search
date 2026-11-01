package snapshot

import (
	"archive/tar"
	"compress/gzip"
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
	"time"

	"github.com/ecosyste-ms/search/internal/project"
	"github.com/ecosyste-ms/search/internal/search"
	"github.com/ecosyste-ms/search/internal/storage"
)

var corpusFiles = []string{"manifest.json", "packages.ndjson.gz", "projects.ndjson.gz", "assignments.ndjson.gz", "assignment-manifest.json", "excluded.json"}
var corpusReports = []string{"corpus.json", "identity.json", "selection.json", "selection.ndjson.gz"}

func allowedCorpusFile(name string) bool {
	return slices.Contains(corpusFiles, name) || slices.Contains(corpusReports, name)
}

type corpusArchiveManifest struct {
	Records  int    `json:"records"`
	Projects int    `json:"projects"`
	SHA256   string `json:"sha256"`
	Finished string `json:"extraction_finished_at"`
	Files    map[string]struct {
		Bytes  int64  `json:"bytes"`
		SHA256 string `json:"sha256"`
	} `json:"files"`
}

func fetchCorpus(ctx context.Context, o releaseOptions, stage string) (publishedManifest, error) {
	var m publishedManifest
	archive := filepath.Join(stage, "source.tar.gz")
	f, err := os.Create(archive)
	if err != nil {
		return m, err
	}
	hash := sha256.New()
	_, fetchErr := download(ctx, o.URL, io.MultiWriter(f, hash), 5_000_000_000)
	if err := errors.Join(fetchErr, f.Close()); err != nil {
		return m, err
	}
	if fmt.Sprintf("%x", hash.Sum(nil)) != o.SHA {
		return m, fmt.Errorf("archive checksum mismatch")
	}
	dir := filepath.Join(stage, "source")
	if err := os.Mkdir(dir, 0755); err != nil {
		return m, err
	}
	if err := unpackCorpus(archive, dir); err != nil {
		return m, err
	}
	m, err = prepareArchive(dir, filepath.Join(stage, "source-projects.ndjson.gz"), o.SHA)
	if err != nil {
		return m, err
	}
	if err := os.RemoveAll(dir); err != nil {
		return m, err
	}
	return m, os.Remove(archive)
}

func unpackCorpus(path, dir string) error {
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
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if !allowedCorpusFile(h.Name) || seen[h.Name] || h.Typeflag != tar.TypeReg || h.Size < 0 || h.Size > 2<<30 {
			return fmt.Errorf("unexpected archive entry %q", h.Name)
		}
		seen[h.Name] = true
		out, err := os.OpenFile(filepath.Join(dir, h.Name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, tr)
		if err := errors.Join(copyErr, out.Close()); err != nil {
			return err
		}
	}
	// Consume the gzip trailer as well as the tar end marker.
	if _, err := io.Copy(io.Discard, io.LimitReader(gz, 1<<20)); err != nil {
		return err
	}
	for _, name := range corpusFiles {
		if !seen[name] {
			return fmt.Errorf("incomplete corpus archive: missing %s", name)
		}
	}
	return nil
}

func archiveManifest(dir string) (corpusArchiveManifest, error) {
	var m corpusArchiveManifest
	if err := storage.ReadJSON(filepath.Join(dir, "manifest.json"), &m); err != nil {
		return m, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return m, err
	}
	if m.Records < 1 || m.Projects < 1 || !validSHA(m.SHA256) || len(m.Files) != len(entries)-1 {
		return m, fmt.Errorf("unsupported or incomplete corpus manifest")
	}
	if _, err := time.Parse(time.RFC3339, m.Finished); err != nil {
		return m, err
	}
	for _, name := range corpusFiles[1:] {
		if _, ok := m.Files[name]; !ok {
			return m, fmt.Errorf("missing file metadata for %s", name)
		}
	}
	for name, info := range m.Files {
		if name == "manifest.json" || !allowedCorpusFile(name) || info.Bytes < 1 || !validSHA(info.SHA256) {
			return m, fmt.Errorf("invalid file metadata for %s", name)
		}
		path := filepath.Join(dir, name)
		stat, err := os.Stat(path)
		if err != nil {
			return m, err
		}
		digest, err := storage.FileSHA(path)
		if err != nil {
			return m, err
		}
		if stat.Size() != info.Bytes || digest != info.SHA256 {
			return m, fmt.Errorf("file verification failed for %s", name)
		}
	}
	if m.Files["packages.ndjson.gz"].SHA256 != m.SHA256 {
		return m, fmt.Errorf("source checksum mismatch")
	}
	return m, nil
}

func archiveAssignments(dir string, m corpusArchiveManifest) (*project.AssignmentSet, error) {
	var am struct {
		project.AssignmentManifest
		AsOf string `json:"as_of"`
	}
	if err := storage.ReadJSON(filepath.Join(dir, "assignment-manifest.json"), &am); err != nil {
		return nil, err
	}
	if am.Packages != m.Records || am.Projects != m.Projects || am.SourcePackageSHA256 != m.SHA256 || am.AssignmentsFile != "assignments.ndjson.gz" || am.AssignmentsSHA256 != m.Files["assignments.ndjson.gz"].SHA256 {
		return nil, fmt.Errorf("assignment manifest mismatch")
	}
	asOf := m.Finished
	if am.AsOf != "" {
		asOf = am.AsOf
	}
	when, err := time.Parse(time.RFC3339, asOf)
	if err != nil {
		return nil, err
	}
	a := &project.AssignmentSet{Rows: map[int]project.ProjectAssignment{}, Projects: map[string]project.ProjectAssignment{}, AsOf: when}
	err = storage.ReadGzipRows(filepath.Join(dir, "assignments.ndjson.gz"), func(d *json.Decoder) error {
		for {
			var row project.ProjectAssignment
			if err := d.Decode(&row); err == io.EOF {
				return nil
			} else if err != nil {
				return err
			}
			if row.PackageID <= 0 || strings.TrimSpace(row.ProjectID) == "" || strings.TrimSpace(row.DisplayName) == "" || row.PreviousProjectID == "" || !slices.Contains([]string{"repository", "package", "distribution_release", "registry_mirror"}, row.Reason) {
				return fmt.Errorf("invalid assignment")
			}
			if _, ok := a.Rows[row.PackageID]; ok {
				return fmt.Errorf("duplicate assignment")
			}
			if prior, ok := a.Projects[row.ProjectID]; ok && (prior.DisplayName != row.DisplayName || prior.Reason != row.Reason) {
				return fmt.Errorf("conflicting project assignment")
			}
			a.Rows[row.PackageID], a.Projects[row.ProjectID] = row, row
		}
	})
	if err != nil {
		return nil, err
	}
	if len(a.Rows) != m.Records || len(a.Projects) != m.Projects {
		return nil, fmt.Errorf("assignment counts differ")
	}
	if err := validateArchiveProjects(dir, m, a); err != nil {
		return nil, err
	}
	return a, nil
}

func validateArchiveProjects(dir string, m corpusArchiveManifest, a *project.AssignmentSet) error {
	seen := map[int]bool{}
	groups := map[string]bool{}
	err := storage.ReadGzipRows(filepath.Join(dir, "projects.ndjson.gz"), func(d *json.Decoder) error {
		for {
			var row struct {
				ID, Name, Reason string
				Packages         []int `json:"package_ids"`
			}
			if err := d.Decode(&row); err == io.EOF {
				return nil
			} else if err != nil {
				return err
			}
			assignment, ok := a.Projects[row.ID]
			if !ok || groups[row.ID] || assignment.DisplayName != row.Name || assignment.Reason != row.Reason || len(row.Packages) == 0 {
				return fmt.Errorf("project membership mismatch")
			}
			groups[row.ID] = true
			for _, id := range row.Packages {
				if seen[id] || a.Rows[id].ProjectID != row.ID {
					return fmt.Errorf("package membership mismatch")
				}
				seen[id] = true
			}
		}
	})
	if err != nil {
		return err
	}
	if len(seen) != m.Records || len(groups) != m.Projects {
		return fmt.Errorf("project counts differ")
	}
	return nil
}

func prepareArchive(dir, output, archiveSHA string) (publishedManifest, error) {
	var result publishedManifest
	m, err := archiveManifest(dir)
	if err != nil {
		return result, err
	}
	a, err := archiveAssignments(dir, m)
	if err != nil {
		return result, err
	}
	rows, public, err := readArchivePackages(dir, m, a)
	if err != nil {
		return result, err
	}
	slices.SortFunc(rows, func(a, b project.Package) int { return a.ID - b.ID })
	c, err := project.Assemble(rows, a.AsOf, a)
	if err != nil {
		return result, err
	}
	if err := writePreparedProjects(output, c, public); err != nil {
		return result, err
	}
	digest, err := storage.FileSHA(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return result, err
	}
	result = publishedManifest{Snapshot: search.SnapshotInfo{Version: archiveSHA, CreatedAt: m.Finished, SourceSHA256: m.SHA256, ManifestSHA256: digest}, Projects: m.Projects, Packages: m.Records}
	return result, nil
}

func readArchivePackages(dir string, m corpusArchiveManifest, a *project.AssignmentSet) ([]project.Package, map[int]search.PublicPackage, error) {
	rows := make([]project.Package, 0, m.Records)
	public := map[int]search.PublicPackage{}
	err := storage.ReadGzipRows(filepath.Join(dir, "packages.ndjson.gz"), func(d *json.Decoder) error {
		for line := 1; ; line++ {
			var raw json.RawMessage
			if err := d.Decode(&raw); err == io.EOF {
				return nil
			} else if err != nil {
				return err
			}
			var row project.Record
			if err := json.Unmarshal(raw, &row); err != nil {
				return err
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				return err
			}
			var status string
			if err := json.Unmarshal(fields["status"], &status); err != nil && fields["status"] != nil {
				return err
			}
			if strings.EqualFold(status, "removed") || fields["purl"] == nil || string(fields["purl"]) == `""` {
				return fmt.Errorf("removed package or missing explicit PURL")
			}
			if _, ok := a.Rows[row.PackageID]; !ok {
				return fmt.Errorf("unassigned package")
			}
			if _, ok := public[row.PackageID]; ok {
				return fmt.Errorf("duplicate package")
			}
			p, err := row.PackageRow()
			if err != nil {
				return err
			}
			if p.Name == "" || row.Registry.Name == "" || row.Registry.Ecosystem == "" || p.Ecosystem == "" {
				return fmt.Errorf("missing public package identity")
			}
			rows = append(rows, p)
			public[p.ID] = search.PublicPackage{SnapshotID: p.ID, Name: p.Name, Registry: row.Registry.Name, Ecosystem: row.Registry.Ecosystem, Namespace: row.Namespace, PURL: search.PublicPURL(p.PURL), SourceLine: line, DeclaredRepository: p.RepositoryURL, CachedRepository: p.Repo.URL}
		}
	})
	if err != nil {
		return nil, nil, err
	}
	if len(rows) != m.Records {
		return nil, nil, fmt.Errorf("package count mismatch")
	}
	return rows, public, nil
}

func writePreparedProjects(path string, c project.Corpus, packages map[int]search.PublicPackage) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(f)
	err = func() error {
		for _, p := range c.Projects {
			public := search.PublicProject{Packages: []search.PublicPackage{}, VerifiedRepositories: []string{}, Evidence: p.Evidence}
			ids := []int{}
			purls := [][]any{}
			for _, pkg := range p.Packages {
				public.Packages = append(public.Packages, packages[pkg.ID])
				ids = append(ids, pkg.ID)
				purls = append(purls, []any{pkg.ID, search.PublicPURL(pkg.PURL)})
			}
			for _, root := range p.Repositories {
				public.VerifiedRepositories = append(public.VerifiedRepositories, "https://"+root)
			}
			row := map[string]any{"id": p.ID, "name": p.Name, "names": p.Names, "descriptions": p.Descriptions, "aliases": p.Aliases, "filters": search.FacetValues(p, c.Metadata[p.ID]), "identifiers": map[string]any{"package_ids": ids, "package_purls": purls, "repositories": p.Repositories}, "public": public}
			if err := json.NewEncoder(gz).Encode(row); err != nil {
				return err
			}
		}
		return nil
	}()
	return errors.Join(err, gz.Close(), f.Close())
}
