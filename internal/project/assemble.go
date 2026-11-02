package project

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

func activity(m Metadata, asOf time.Time) string {
	if m.Archived {
		return "archived"
	}
	pushed, err := time.Parse(time.RFC3339, m.Pushed)
	if err != nil {
		return "unknown"
	}
	if asOf.Sub(pushed) <= 365*24*time.Hour {
		return "active"
	}
	return "inactive"
}

func Assemble(rows []Package, asOf time.Time, assignments *AssignmentSet) (Corpus, error) {
	c := Corpus{Metadata: map[string]Metadata{}, AsOf: asOf}
	projects := map[string]*Project{}
	seen := map[int]bool{}
	distributions := dependentDistributions(rows)
	// Prefer the most recently synced cached copy when packages share a repository.
	repoCopies := map[int]Repository{}
	for _, p := range rows {
		if p.Repo.ID > 0 && p.Repo.Synced >= repoCopies[p.Repo.ID].Synced {
			repoCopies[p.Repo.ID] = p.Repo
		}
	}
	for _, p := range rows {
		if p.ID <= 0 {
			return c, fmt.Errorf("package without positive ID: %q", p.Name)
		}
		if p.Signals.DependentRepos < 0 {
			return c, fmt.Errorf("negative dependent repository count for package %d", p.ID)
		}
		if seen[p.ID] {
			return c, fmt.Errorf("duplicate package %d", p.ID)
		}
		seen[p.ID] = true
		assignment, exists := assignments.Rows[p.ID]
		if !exists {
			return c, fmt.Errorf("missing assignment for package %d", p.ID)
		}
		id, evidence := assignment.ProjectID, assignment.Reason
		project := projects[id]
		if project == nil {
			project = &Project{ID: id, Name: p.Name, Evidence: evidence}
			projects[id] = project
		}
		project.Names = append(project.Names, p.Name)
		project.Descriptions = append(project.Descriptions, p.Description)
		project.Aliases = append(project.Aliases, p.Keywords...)
		project.Packages = append(project.Packages, PackageRef{ID: p.ID, Name: p.Name, PURL: p.PURL})
		project.Ecosystems = append(project.Ecosystems, p.Ecosystem)
		project.Languages = append(project.Languages, p.Language)
		project.Licenses = append(project.Licenses, p.Licenses...)
		m := c.Metadata[id]
		m.DependentReposPercentile = max(m.DependentReposPercentile, dependentPercentile(p, distributions))
		root, ok := RepositoryRoot(p.RepositoryURL)
		cached, cachedOK := RepositoryRoot(p.Repo.URL)
		if ok && cachedOK && root == cached {
			project.Repositories = append(project.Repositories, root)
			if collectionRepository(root) {
				c.Metadata[id] = m
				continue
			}
			r := repoCopies[p.Repo.ID]
			if r.ID == 0 {
				r = p.Repo
			}
			project.Names = append(project.Names, r.FullName, strings.TrimPrefix(root, "github.com/"))
			project.Descriptions = append(project.Descriptions, r.Description)
			project.Aliases = append(project.Aliases, r.Topics...)
			project.Aliases = append(project.Aliases, r.PreviousNames...)
			project.Languages = append(project.Languages, r.Language)
			project.Licenses = append(project.Licenses, r.License)
			if r.Pushed > m.Pushed {
				m.Pushed = r.Pushed
			}
			archived := r.Archived != nil && *r.Archived
			if len(project.Repositories) == 1 {
				m.Archived = archived
			} else {
				m.Archived = m.Archived && archived
			}
		}
		c.Metadata[id] = m
	}
	for id, p := range projects {
		p.Names = Unique(p.Names)
		p.Descriptions = Unique(p.Descriptions)
		p.Aliases = Unique(p.Aliases)
		p.Repositories = Unique(p.Repositories)
		p.Ecosystems = Lower(p.Ecosystems)
		p.Languages = Lower(p.Languages)
		p.Licenses = Lower(p.Licenses)
		if len(p.Languages) == 0 {
			p.Languages = []string{"unknown"}
		}
		if len(p.Licenses) == 0 {
			p.Licenses = []string{"unknown"}
		}
		slices.SortFunc(p.Packages, func(a, b PackageRef) int { return a.ID - b.ID })
		p.Name = assignments.Projects[id].DisplayName
		m := c.Metadata[id]
		m.Activity = activity(m, asOf)
		c.Metadata[id] = m
		c.Projects = append(c.Projects, *p)
	}
	slices.SortFunc(c.Projects, func(a, b Project) int { return strings.Compare(a.ID, b.ID) })
	return c, nil
}
