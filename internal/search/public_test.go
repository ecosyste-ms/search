package search

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ecosyste-ms/search/internal/project"
)

func TestPublicProvenanceAllowlist(t *testing.T) {
	source := &project.CorpusSource{Path: "/Users/private-user/source", Provenance: "/private/metadata", SHA256: "input-checksum", Assignments: &project.AssignmentProvenance{ManifestSHA256: "manifest-checksum", AssignmentManifest: project.AssignmentManifest{AssignmentsFile: "/private/assignments", Description: "internal details", AssignmentsSHA256: "assignment-checksum", Projects: 1, Packages: 2}}}
	b, err := json.Marshal(PublicResponse(&PublicProject{Packages: []PublicPackage{}, VerifiedRepositories: []string{}}, source))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"/private/", "/Users/", "internal details", "assignments_file"} {
		if strings.Contains(string(b), value) {
			t.Fatal("private provenance leaked", string(b))
		}
	}
	for _, value := range []string{"input-checksum", "manifest-checksum", "assignment-checksum"} {
		if !strings.Contains(string(b), value) {
			t.Fatal("public checksum missing", value)
		}
	}
}
func TestPublicLinksPreserveIdentifiers(t *testing.T) {
	for _, p := range []PublicPackage{{Registry: "npmjs.org", Name: "@babel/core", PURL: "pkg:npm/%40babel/core"}, {Registry: "repo1.maven.org", Name: "org.example:core", PURL: "pkg:maven/org.example/core"}, {Registry: "rubygems.org", Name: "openssl", PURL: "pkg:gem/openssl"}, {Registry: "nixpkgs-unstable", Name: "curl", PURL: "pkg:nix/curl?channel=unstable"}} {
		response := PublicResponse(&PublicProject{Packages: []PublicPackage{p}, VerifiedRepositories: []string{}, Evidence: "package"}, nil)
		b, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), string(p.PURL)) || !strings.Contains(string(b), p.Registry) {
			t.Fatal("lost public identity", string(b))
		}
		if !strings.Contains(packagePage(p), "/registries/"+p.Registry+"/packages/") {
			t.Fatal("incorrect public page", packagePage(p))
		}
	}
}
