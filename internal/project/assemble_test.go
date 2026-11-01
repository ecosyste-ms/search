package project

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestExportRecord(t *testing.T) {
	raw, err := os.ReadFile("testdata/package.json")
	if err != nil {
		t.Fatal(err)
	}
	var r Record
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	p, err := r.PackageRow()
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != r.PackageID || p.Name == "" {
		t.Fatal("package identity lost")
	}
	var original map[string]any
	if err := json.Unmarshal(raw, &original); err != nil {
		t.Fatal(err)
	}
	if p.PURL != original["purl"] && original["purl"] != nil {
		t.Fatal("PURL changed")
	}
}
func TestSuppliedAssignmentsPreserveLineages(t *testing.T) {
	rows := []Package{{ID: 1, Name: "curl", PURL: "pkg:nix/curl?channel=stable", Ecosystem: "nixpkgs"}, {ID: 2, Name: "curl", PURL: "pkg:nix/curl?channel=unstable", Ecosystem: "nixpkgs"}, {ID: 3, Name: "curl", PURL: "pkg:gem/curl", Ecosystem: "rubygems"}}
	assignments := &AssignmentSet{Rows: map[int]ProjectAssignment{}, Projects: map[string]ProjectAssignment{}}
	for _, p := range rows {
		id := "nix:curl"
		if p.ID == 3 {
			id = "gem:curl"
		}
		a := ProjectAssignment{PackageID: p.ID, ProjectID: id, DisplayName: "curl", Reason: "distribution_release"}
		assignments.Rows[p.ID] = a
		assignments.Projects[id] = a
	}
	c, err := Assemble(rows, time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC), assignments)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Projects) != 2 {
		t.Fatal("supplied groups changed")
	}
	got := map[string][]string{}
	for _, p := range c.Projects {
		for _, pkg := range p.Packages {
			got[p.ID] = append(got[p.ID], pkg.PURL)
		}
	}
	want := map[string][]string{"nix:curl": {"pkg:nix/curl?channel=stable", "pkg:nix/curl?channel=unstable"}, "gem:curl": {"pkg:gem/curl"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	delete(assignments.Rows, 3)
	if _, err := Assemble(rows, time.Now(), assignments); err == nil {
		t.Fatal("missing assignment accepted")
	}
}
