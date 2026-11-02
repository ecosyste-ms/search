package project

import (
	"slices"
	"strings"
)

func dependentDistributions(rows []Package) map[string][]int64 {
	groups := map[string][]int64{}
	for _, p := range rows {
		if p.Signals.DependentRepos > 0 {
			ecosystem := strings.ToLower(p.Ecosystem)
			groups[ecosystem] = append(groups[ecosystem], p.Signals.DependentRepos)
		}
	}
	for _, counts := range groups {
		slices.Sort(counts)
	}
	return groups
}

func dependentPercentile(p Package, groups map[string][]int64) float64 {
	if p.Signals.DependentRepos <= 0 {
		return 0
	}
	counts := groups[strings.ToLower(p.Ecosystem)]
	// Use the first equal count so ties share a rank, excluding zero observations.
	position, _ := slices.BinarySearch(counts, p.Signals.DependentRepos)
	return float64(position+1) / float64(len(counts))
}
