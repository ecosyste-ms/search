package project

import (
	"time"
)

type Package struct {
	ID            int            `json:"id"`
	Name          string         `json:"name"`
	PURL          string         `json:"purl"`
	Ecosystem     string         `json:"ecosystem"`
	Description   string         `json:"description"`
	RepositoryURL string         `json:"repository_url"`
	Language      string         `json:"language"`
	Licenses      []string       `json:"normalized_licenses"`
	Keywords      []string       `json:"keywords_array"`
	Repo          Repository     `json:"repo_metadata"`
	Signals       PackageSignals `json:"signals"`
}

type PackageSignals struct {
	DependentRepos int64 `json:"dependent_repos_count"`
}

type Repository struct {
	ID            int      `json:"id"`
	FullName      string   `json:"full_name"`
	URL           string   `json:"html_url"`
	Description   string   `json:"description"`
	Language      string   `json:"language"`
	License       string   `json:"license"`
	Topics        []string `json:"topics"`
	PreviousNames []string `json:"previous_names"`
	Pushed        string   `json:"pushed_at"`
	Archived      *bool    `json:"archived"`
	Synced        string   `json:"last_synced_at"`
}

type PackageRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	PURL string `json:"purl"`
}

type Project struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Names        []string     `json:"names"`
	Descriptions []string     `json:"descriptions"`
	Aliases      []string     `json:"aliases"`
	Packages     []PackageRef `json:"packages"`
	Repositories []string     `json:"repositories"`
	Ecosystems   []string     `json:"ecosystems"`
	Languages    []string     `json:"languages"`
	Licenses     []string     `json:"licenses"`
	Evidence     string       `json:"evidence"`
}

type Metadata struct {
	Pushed                   string  `json:"pushed_at"`
	Archived                 bool    `json:"archived"`
	Activity                 string  `json:"activity"`
	DependentReposPercentile float64 `json:"dependent_repos_percentile,omitempty"`
}

type Corpus struct {
	Projects []Project           `json:"projects"`
	Metadata map[string]Metadata `json:"metadata"`
	AsOf     time.Time           `json:"as_of"`
	Source   *CorpusSource       `json:"source,omitempty"`
}

type RegistryRef struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
	URL       string `json:"url"`
}

type CorpusSource struct {
	Path        string                `json:"path"`
	SHA256      string                `json:"sha256"`
	Provenance  string                `json:"provenance"`
	Assignments *AssignmentProvenance `json:"assignments,omitempty"`
}

type ProjectAssignment struct {
	PackageID         int    `json:"package_id"`
	PreviousProjectID string `json:"previous_project_id"`
	ProjectID         string `json:"project_id"`
	DisplayName       string `json:"display_name"`
	Reason            string `json:"reason"`
}

type AssignmentManifest struct {
	SourcePackageSHA256 string `json:"source_package_sha256"`
	SourceCorpusSHA256  string `json:"source_corpus_sha256"`
	AssignmentsFile     string `json:"assignments_file"`
	AssignmentsSHA256   string `json:"assignments_sha256"`
	Packages            int    `json:"packages"`
	Projects            int    `json:"projects"`
	Description         string `json:"description"`
}

type AssignmentProvenance struct {
	AssignmentManifest
	ManifestSHA256 string `json:"manifest_sha256"`
}

type AssignmentSet struct {
	Rows       map[int]ProjectAssignment
	Projects   map[string]ProjectAssignment
	Provenance *AssignmentProvenance
	AsOf       time.Time
}
