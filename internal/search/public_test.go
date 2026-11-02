package search

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ecosyste-ms/search/internal/project"
)

func TestPublicPackagePreview(t *testing.T) {
	for _, count := range []int{0, 1, 10, 11, 101} {
		p := &PublicProject{Packages: []PublicPackage{}, VerifiedRepositories: []string{}}
		for i := range count {
			p.Packages = append(p.Packages, PublicPackage{Name: fmt.Sprintf("package-%d", i), Registry: "npmjs.org", Ecosystem: "npm"})
		}
		body, err := json.Marshal((&Engine{}).PublicResponse(p, nil, ""))
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Packages  []PublicPackage `json:"packages"`
			Count     int             `json:"package_count"`
			Truncated bool            `json:"packages_truncated"`
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Packages) != min(count, 10) || got.Count != count || got.Truncated != (count > 10) || len(p.Packages) != count {
			t.Fatalf("incorrect preview for %d packages: %s", count, body)
		}
	}
}

func TestPackagePreviewIdentifiers(t *testing.T) {
	for _, name := range []string{"@babel/core", "org.example:parser", "Requests"} {
		packages := make([]PublicPackage, 12)
		for i := range packages {
			packages[i] = PublicPackage{Name: fmt.Sprintf("other-%d", i), Registry: "registry", Ecosystem: "example"}
		}
		packages[11].Name, packages[11].PURL = name, PublicPURL("pkg:example/identifier")
		for _, q := range []string{" " + strings.ToLower(name) + " ", "pkg:example/identifier"} {
			shown := packagePreview(packages, q, nil)
			if len(shown) != 10 || shown[0].Name != name || packages[0].Name != "other-0" {
				t.Fatalf("incorrect preview for %q: %v", q, shown)
			}
		}
	}
}

func TestPackagePreviewRegistryOrder(t *testing.T) {
	packages := []PublicPackage{{Name: "numpy", Registry: "conda-forge.org", Ecosystem: "conda"}, {Name: "numpy", Registry: "pypi.org", Ecosystem: "PyPI"}, {Name: "other", Registry: "npmjs.org", Ecosystem: "npm"}}
	sizes := map[registryKey]int{packages[0].registryKey(): 10, packages[1].registryKey(): 100, packages[2].registryKey(): 1000}
	shown := packagePreview(packages, "numpy", sizes)
	if shown[0].Registry != "pypi.org" || shown[1].Registry != "conda-forge.org" || shown[2].Registry != "npmjs.org" {
		t.Fatalf("exact matches or registry sizes ignored: %v", shown)
	}
	shown = packagePreview(packages, "", sizes)
	if shown[0].Registry != "npmjs.org" || shown[1].Registry != "pypi.org" {
		t.Fatalf("registry order ignored: %v", shown)
	}
}

func BenchmarkPackagePreview(b *testing.B) {
	for _, count := range []int{10, 1000, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			packages := make([]PublicPackage, count)
			for i := range packages {
				packages[i] = PublicPackage{Name: fmt.Sprintf("package-%d", i), Registry: "registry", Ecosystem: "example"}
			}
			packages[count-1].Name = "target"
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				packagePreview(packages, "target", nil)
			}
		})
	}
}

func TestPublicProvenanceAllowlist(t *testing.T) {
	source := &project.CorpusSource{Path: "/Users/private-user/source", Provenance: "/private/metadata", SHA256: "input-checksum", Assignments: &project.AssignmentProvenance{ManifestSHA256: "manifest-checksum", AssignmentManifest: project.AssignmentManifest{AssignmentsFile: "/private/assignments", Description: "internal details", AssignmentsSHA256: "assignment-checksum", Projects: 1, Packages: 2}}}
	b, err := json.Marshal((&Engine{}).PublicResponse(&PublicProject{Packages: []PublicPackage{}, VerifiedRepositories: []string{}}, source, ""))
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
		response := (&Engine{}).PublicResponse(&PublicProject{Packages: []PublicPackage{p}, VerifiedRepositories: []string{}, Evidence: "package"}, nil, "")
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
