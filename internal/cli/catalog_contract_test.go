package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ecosyste-ms/search/internal/search"
)

func TestSnapshotRequiresAssignmentDate(t *testing.T) {
	for _, value := range []any{nil, "", "not-a-date"} {
		t.Run(fmt.Sprint(value), func(t *testing.T) {
			files := archiveFixture(t)
			data := packFixture(t, files, func(manifest map[string]any) {
				var assignments map[string]any
				if err := json.Unmarshal(files["assignment-manifest.json"], &assignments); err != nil {
					t.Fatal(err)
				}
				assignments["as_of"] = value
				if value == nil {
					delete(assignments, "as_of")
				}
				body := fixtureJSON(t, assignments)
				files["assignment-manifest.json"] = body
				manifest["files"].(map[string]any)["assignment-manifest.json"] = map[string]any{"bytes": len(body), "sha256": fmt.Sprintf("%x", sha256.Sum256(body))}
			})
			output := filepath.Join(t.TempDir(), "release")
			if err := Run(snapshotArgs(t, data, output), io.Discard); err == nil || !strings.Contains(err.Error(), "as_of") {
				t.Fatalf("invalid assignment date accepted: %v", err)
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatalf("failed import left a release: %v", err)
			}
		})
	}
}

func TestOpenRequiresCurrentCatalogue(t *testing.T) {
	for _, problem := range []string{"missing-popularity", "null-popularity", "missing-public", "null-public", "missing-description", "compressed-catalogue", "missing-snapshot"} {
		t.Run(problem, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "release")
			if err := Run(snapshotArgs(t, packFixture(t, archiveFixture(t), nil), output), io.Discard); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(output, "snapshot")
			catalogue := filepath.Join(dir, "serving-catalog.ndjson")
			body, err := os.ReadFile(catalogue)
			if err != nil {
				t.Fatal(err)
			}
			var rows []any
			decoder := json.NewDecoder(bytes.NewReader(body))
			for decoder.More() {
				var row map[string]any
				if err := decoder.Decode(&row); err != nil {
					t.Fatal(err)
				}
				if value, ok := row["dependent_repos_percentile"]; !ok || value != float64(0) {
					t.Fatal("writer must preserve explicit zero popularity")
				}
				switch problem {
				case "missing-popularity":
					delete(row, "dependent_repos_percentile")
				case "null-popularity":
					row["dependent_repos_percentile"] = nil
				case "missing-public":
					delete(row, "public")
				case "null-public":
					row["public"] = nil
				case "missing-description":
					delete(row, "description")
				}
				rows = append(rows, row)
			}
			var changed bytes.Buffer
			for _, row := range rows {
				if err := json.NewEncoder(&changed).Encode(row); err != nil {
					t.Fatal(err)
				}
			}
			body = changed.Bytes()
			name := "serving-catalog.ndjson"
			if problem == "compressed-catalogue" {
				name += ".gz"
				body = fixtureGzip(t, rows...)
			}
			if err := os.Remove(catalogue); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, name), body, 0444); err != nil {
				t.Fatal(err)
			}
			manifestPath := filepath.Join(dir, "manifest.json")
			manifestBody, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			var manifest map[string]any
			if err := json.Unmarshal(manifestBody, &manifest); err != nil {
				t.Fatal(err)
			}
			files := manifest["files"].(map[string]any)
			delete(files, "serving-catalog.ndjson")
			files[name] = map[string]any{"sha256": fmt.Sprintf("%x", sha256.Sum256(body))}
			if problem == "missing-snapshot" {
				delete(manifest, "snapshot")
			}
			if err := os.Chmod(manifestPath, 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(manifestPath, fixtureJSON(t, manifest), 0444); err != nil {
				t.Fatal(err)
			}
			engine, err := search.Open(filepath.Join(dir, "index"), dir)
			if err == nil {
				_ = engine.Close()
				t.Fatal("unsupported catalogue accepted")
			}
			want := map[string]string{
				"missing-popularity":   "dependent repository percentile",
				"null-popularity":      "dependent repository percentile",
				"missing-public":       "public record missing",
				"null-public":          "public record missing",
				"missing-description":  "description missing",
				"compressed-catalogue": "serving-catalog.ndjson required",
				"missing-snapshot":     "invalid compact export manifest",
			}[problem]
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("wrong rejection: %v", err)
			}
		})
	}
}
