package search

type SnapshotInfo struct {
	Version         string `json:"version"`
	CreatedAt       string `json:"created_at"`
	SourceSHA256    string `json:"source_sha256"`
	ManifestSHA256  string `json:"manifest_sha256,omitempty"`
	CatalogueSHA256 string `json:"catalogue_sha256,omitempty"`
}
