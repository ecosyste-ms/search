package project

import (
	"net/url"
	"slices"
	"strings"
)

// Only GitHub root URLs have automatic identity semantics here.
func RepositoryRoot(raw string) (string, bool) {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "git+")
	if strings.HasPrefix(raw, "git@github.com:") {
		raw = "https://github.com/" + strings.TrimPrefix(raw, "git@github.com:")
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Hostname(), "github.com") || u.Port() != "" {
		return "", false
	}
	if u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "git" && u.Scheme != "ssh" {
		return "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	repo := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(parts[1], ".git"), ".GIT"))
	if repo == "" || parts[0] == "." || parts[0] == ".." || repo == "." || repo == ".." {
		return "", false
	}
	return "github.com/" + strings.ToLower(parts[0]) + "/" + repo, true
}

func collectionRepository(root string) bool {
	return slices.Contains([]string{"github.com/definitelytyped/definitelytyped", "github.com/nixos/nixpkgs", "github.com/homebrew/homebrew-core", "github.com/conda-forge/staged-recipes", "github.com/spack/spack", "github.com/conan-io/conan-center-index", "github.com/docker-library/official-images", "github.com/flutter/packages", "github.com/dart-lang/tools", "github.com/csstools/postcss-plugins"}, root)
}

func Unique(values []string) []string {
	result := make([]string, 0, len(values))
	for _, s := range values {
		s = strings.TrimSpace(s)
		if s != "" {
			result = append(result, s)
		}
	}
	slices.Sort(result)
	return slices.Compact(result)
}

func Lower(values []string) []string {
	out := make([]string, len(values))
	for i, s := range values {
		out[i] = strings.ToLower(s)
	}
	return Unique(out)
}
