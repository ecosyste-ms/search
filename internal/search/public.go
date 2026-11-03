package search

import (
	"cmp"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/ecosyste-ms/search/internal/project"
)

type PublicPURL string

const publicPackageLimit = 10

func (p PublicPURL) MarshalJSON() ([]byte, error) {
	if p == "" {
		return []byte("null"), nil
	}
	return json.Marshal(string(p))
}

type PublicPackage struct {
	SnapshotID         int        `json:"snapshot_id"`
	Name               string     `json:"name"`
	Registry           string     `json:"registry"`
	Ecosystem          string     `json:"ecosystem"`
	Namespace          *string    `json:"namespace"`
	PURL               PublicPURL `json:"purl"`
	SourceLine         int        `json:"source_line"`
	DeclaredRepository string     `json:"declared_repository,omitempty"`
	CachedRepository   string     `json:"cached_repository,omitempty"`
}

type PublicProject struct {
	Packages             []PublicPackage `json:"packages"`
	VerifiedRepositories []string        `json:"verified_repositories"`
	Evidence             string          `json:"grouping_evidence"`
}

type publicAssignmentSource struct {
	SourcePackageSHA256 string `json:"source_package_sha256"`
	SourceCorpusSHA256  string `json:"source_corpus_sha256"`
	AssignmentsSHA256   string `json:"assignments_sha256"`
	ManifestSHA256      string `json:"manifest_sha256"`
	Packages            int    `json:"packages"`
	Projects            int    `json:"projects"`
}

type publicCorpusSource struct {
	SHA256      string                  `json:"sha256"`
	Assignments *publicAssignmentSource `json:"assignments,omitempty"`
}

func publicSource(source *project.CorpusSource) *publicCorpusSource {
	if source == nil {
		return nil
	}
	result := &publicCorpusSource{SHA256: source.SHA256}
	if a := source.Assignments; a != nil {
		result.Assignments = &publicAssignmentSource{
			SourcePackageSHA256: a.SourcePackageSHA256, SourceCorpusSHA256: a.SourceCorpusSHA256,
			AssignmentsSHA256: a.AssignmentsSHA256, ManifestSHA256: a.ManifestSHA256,
			Packages: a.Packages, Projects: a.Projects,
		}
	}
	return result
}

type PublicIdentity struct {
	Label    string `json:"label"`
	URL      string `json:"url,omitempty"`
	PURL     string `json:"purl,omitempty"`
	Packages int    `json:"package_count"`
}

func packagePage(p PublicPackage) string {
	if p.Registry == "" || p.Name == "" {
		return ""
	}
	return "https://packages.ecosyste.ms/registries/" + url.PathEscape(p.Registry) + "/packages/" + url.PathEscape(p.Name)
}

func repositoryPage(raw string) string {
	root, ok := project.RepositoryRoot(raw)
	if !ok {
		return ""
	}
	return "https://repos.ecosyste.ms/hosts/GitHub/repositories/" + url.PathEscape(strings.TrimPrefix(root, "github.com/"))
}

func ecosystemLabel(s string) string {
	switch strings.ToLower(s) {
	case "rubygems":
		return "RubyGems"
	case "pypi":
		return "PyPI"
	case "homebrew":
		return "Homebrew"
	case "maven":
		return "Maven"
	case "go":
		return "Go"
	case "cargo":
		return "Cargo"
	default:
		return s
	}
}

func (p *PublicProject) identity() *PublicIdentity {
	if p == nil {
		return nil
	}
	result := &PublicIdentity{Packages: len(p.Packages)}
	ecosystems := []string{}
	for _, pkg := range p.Packages {
		ecosystems = append(ecosystems, ecosystemLabel(pkg.Ecosystem))
	}
	ecosystems = project.Unique(ecosystems)
	result.Label = strings.Join(ecosystems, " · ")
	switch {
	case len(p.Packages) == 1:
		pkg := p.Packages[0]
		result.PURL = string(pkg.PURL)
		result.Label = ecosystemLabel(pkg.Ecosystem) + " · " + pkg.Name + " · " + pkg.Registry
		result.URL = packagePage(pkg)
	case len(ecosystems) == 1:
		result.Label = fmt.Sprintf("%s · %d packages", ecosystems[0], len(p.Packages))
	case len(p.Packages) == 0 && len(p.VerifiedRepositories) > 0:
		result.Label = "GitHub · " + strings.TrimPrefix(p.VerifiedRepositories[0], "https://github.com/")
	}
	if len(p.VerifiedRepositories) == 1 {
		result.URL = repositoryPage(p.VerifiedRepositories[0])
	}
	return result
}

type entityCard struct {
	Count, RepositoryCount                      int
	Name, Registry, Ecosystem, PURL, Repository string
}

func makeEntityCard(p *PublicProject) *entityCard {
	c := &entityCard{Count: len(p.Packages), RepositoryCount: len(p.VerifiedRepositories)}
	if c.Count == 1 {
		pkg := p.Packages[0]
		c.Name, c.Registry, c.Ecosystem, c.PURL = pkg.Name, pkg.Registry, pkg.Ecosystem, string(pkg.PURL)
	}
	if c.RepositoryCount == 1 {
		c.Repository = p.VerifiedRepositories[0]
	}
	return c
}

func (e *Engine) PublicProject(ordinal uint32) (*PublicProject, error) {
	card := e.Compact.Cards[ordinal]
	body := make([]byte, card.Length)
	if _, err := e.Compact.File.ReadAt(body, card.Offset); err != nil {
		return nil, err
	}
	var row CatalogRecord
	if err := json.Unmarshal(body, &row); err != nil {
		return nil, err
	}
	return row.Public, nil
}

func (e *Engine) AttachIdentities(hits []Hit) {
	for i := range hits {
		ordinal, err := e.Ordinal(hits[i].ID)
		if err != nil {
			continue
		}
		card := e.Compact.Cards[ordinal].Entity
		identity := &PublicIdentity{Packages: card.Count}
		ecosystems := []string{}
		for _, value := range e.Catalog.Values[0][e.Catalog.Rows[ordinal][0]] {
			ecosystems = append(ecosystems, ecosystemLabel(value))
		}
		slices.SortStableFunc(ecosystems, func(a, b string) int {
			return cmp.Compare(e.ecosystemSizes[b], e.ecosystemSizes[a])
		})
		identity.Label = strings.Join(ecosystems, " · ")
		if card.Count == 1 {
			pkg := PublicPackage{Name: card.Name, Registry: card.Registry, Ecosystem: card.Ecosystem, PURL: PublicPURL(card.PURL)}
			identity = (&PublicProject{Packages: []PublicPackage{pkg}}).identity()
		} else if len(ecosystems) == 1 {
			identity.Label = fmt.Sprintf("%s · %d packages", ecosystems[0], card.Count)
		}
		if card.RepositoryCount == 1 {
			identity.URL = repositoryPage(card.Repository)
			if card.Count == 0 {
				identity.Label = "GitHub · " + strings.TrimPrefix(card.Repository, "https://github.com/")
			}
		}
		hits[i].Identity = identity
	}
}

type bulkLookup struct {
	URL        string         `json:"url"`
	Body       map[string]any `json:"body"`
	Registries []string       `json:"verify_registries"`
}

func (e *Engine) AttachPublicDetails(hits []Hit, query string) error {
	e.AttachIdentities(hits)
	for i := range hits {
		ordinal, err := e.Ordinal(hits[i].ID)
		if err != nil {
			return err
		}
		details, err := e.Entities(ordinal, query)
		if err != nil {
			return err
		}
		hits[i].Public = details
	}
	return nil
}

type registryKey struct{ ecosystem, registry string }

func (p PublicPackage) registryKey() registryKey {
	return registryKey{strings.ToLower(p.Ecosystem), strings.ToLower(p.Registry)}
}

func packagePreview(packages []PublicPackage, query string, sizes map[registryKey]int) []PublicPackage {
	query = strings.TrimSpace(query)
	shown := make([]PublicPackage, 0, min(publicPackageLimit, len(packages)))
	selected := map[int]bool{}
	registries := map[registryKey]bool{}
	type candidate struct {
		index, size int
		registry    registryKey
	}
	for pass := range 3 {
		if pass == 0 && query == "" {
			continue
		}
		remaining := publicPackageLimit - len(shown)
		if remaining == 0 {
			return shown
		}
		candidates := make([]candidate, 0, remaining+1)
		for i, pkg := range packages {
			match := query != "" && (strings.EqualFold(pkg.Name, query) || strings.EqualFold(string(pkg.PURL), query))
			if selected[i] || (pass == 0 && !match) {
				continue
			}
			registry := pkg.registryKey()
			if pass == 1 && (registries[registry] || slices.ContainsFunc(candidates, func(c candidate) bool { return c.registry == registry })) {
				continue
			}
			candidates = append(candidates, candidate{i, sizes[registry], registry})
			if pass != 2 {
				slices.SortStableFunc(candidates, func(a, b candidate) int { return cmp.Compare(b.size, a.size) })
			}
			candidates = candidates[:min(remaining, len(candidates))]
			if pass == 2 && len(candidates) == remaining {
				break
			}
		}
		for _, c := range candidates {
			shown = append(shown, packages[c.index])
			selected[c.index], registries[c.registry] = true, true
		}
	}
	return shown
}

func (e *Engine) PublicResponse(p *PublicProject, source *project.CorpusSource, query string) map[string]any {
	type resolvedPackage struct {
		PublicPackage
		URL      string `json:"url"`
		APIURL   string `json:"api_url"`
		Verified bool   `json:"repository_verified"`
	}
	shown := packagePreview(p.Packages, query, e.registrySizes)
	packages := make([]resolvedPackage, 0, len(shown))
	groups := map[string][]PublicPackage{}
	for _, pkg := range shown {
		root, ok := project.RepositoryRoot(pkg.DeclaredRepository)
		cached, cachedOK := project.RepositoryRoot(pkg.CachedRepository)
		page := packagePage(pkg)
		packages = append(packages, resolvedPackage{pkg, page, strings.Replace(page, "/registries/", "/api/v1/registries/", 1), ok && cachedOK && root == cached})
		key := pkg.Ecosystem
		if pkg.PURL != "" {
			key = ""
		}
		groups[key] = append(groups[key], pkg)
	}
	requests := []bulkLookup{}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		group := groups[key]
		for start := 0; start < len(group); start += 100 {
			chunk := group[start:min(start+100, len(group))]
			values, registries := []string{}, []string{}
			for _, pkg := range chunk {
				value := pkg.Name
				if key == "" {
					value = string(pkg.PURL)
				}
				values = append(values, value)
				registries = append(registries, pkg.Registry)
			}
			body := map[string]any{"names": project.Unique(values), "ecosystem": key}
			if key == "" {
				body = map[string]any{"purls": project.Unique(values)}
			}
			requests = append(requests, bulkLookup{"https://packages.ecosyste.ms/api/v1/packages/bulk_lookup", body, project.Unique(registries)})
		}
	}
	repos := []map[string]string{}
	for _, raw := range p.VerifiedRepositories {
		repos = append(repos, map[string]string{"url": raw, "page_url": repositoryPage(raw), "lookup_url": "https://repos.ecosyste.ms/api/v1/repositories/lookup?" + url.Values{"url": {raw}}.Encode(), "verification": "package declaration agrees with cached repository metadata"})
	}
	return map[string]any{"packages": packages, "package_count": len(p.Packages), "packages_truncated": len(shown) < len(p.Packages), "repositories": repos, "bulk_lookup_requests": requests, "grouping_evidence": p.Evidence, "source": publicSource(source)}
}
