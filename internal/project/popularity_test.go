package project

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestAssembleDependentPercentiles(t *testing.T) {
	const ecosystem = "pypi"
	const cross = "cross"
	const tied = "tied"
	const zero = "zero"
	rows := []Package{}
	assignments := &AssignmentSet{Rows: map[int]ProjectAssignment{}, Projects: map[string]ProjectAssignment{}}
	for i, input := range []struct {
		ecosystem, group string
		count            int64
	}{
		{"PyPI", tied, 10}, {ecosystem, tied, 10}, {ecosystem, "high", 100},
		{"npm", cross, 50}, {ecosystem, cross, 1}, {ecosystem, zero, 0},
		{"cargo", zero, 0}, {"npm", "npm-tie", 50},
	} {
		id := i + 1
		rows = append(rows, Package{ID: id, Name: fmt.Sprintf("package-%d", id), Ecosystem: input.ecosystem, Signals: PackageSignals{DependentRepos: input.count}})
		a := ProjectAssignment{PackageID: id, ProjectID: input.group, DisplayName: input.group, Reason: "package"}
		assignments.Rows[id], assignments.Projects[input.group] = a, a
	}
	c, err := Assemble(rows, time.Now(), assignments)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]float64{tied: .5, "high": 1, cross: .5, zero: 0, "npm-tie": .5} {
		if got := c.Metadata[id].DependentReposPercentile; got != want {
			t.Errorf("%s: got %v want %v", id, got, want)
		}
	}
	rows[0].Signals.DependentRepos = -1
	if _, err := Assemble(rows, time.Now(), assignments); err == nil {
		t.Fatal("negative count accepted")
	}
}

func TestRecordDependentSignals(t *testing.T) {
	for _, signal := range []string{``, `,"signals":null`, `,"signals":{}`, `,"signals":{"dependent_repos_count":null}`, `,"signals":{"dependent_repos_count":0}`} {
		var r Record
		if err := json.Unmarshal([]byte(`{"package_id":1,"repository":null`+signal+`}`), &r); err != nil {
			t.Fatal(err)
		}
		p, err := r.PackageRow()
		if err != nil || p.Signals.DependentRepos != 0 {
			t.Fatalf("missing count: %+v %v", p, err)
		}
	}
	for _, count := range []string{`"10"`, `1.5`, `9223372036854775808`} {
		var r Record
		if err := json.Unmarshal([]byte(`{"signals":{"dependent_repos_count":`+count+`}}`), &r); err == nil {
			t.Fatal("invalid count accepted", count)
		}
	}
}
