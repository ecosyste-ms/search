package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHostSnapshotUpdate(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("host updater uses Linux flock and mv -T")
	}
	for _, mode := range []string{"success", "download-failure", "restart-failure", "health-failure", "existing"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			old := filepath.Join(root, "releases/old")
			if err := os.MkdirAll(old, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(old, "receipt.json"), []byte(`{"snapshot":{"version":"old"}}`), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("releases/old", filepath.Join(root, "current")); err != nil {
				t.Fatal(err)
			}
			sha := strings.Repeat("a", 64)
			if mode == "existing" {
				if err := os.Mkdir(filepath.Join(root, "releases", sha), 0755); err != nil {
					t.Fatal(err)
				}
			}
			bin := t.TempDir()
			mock := `#!/usr/bin/env bash
set -eu
case "$(basename "$0")" in
docker)
  [[ $MODE != download-failure ]] || exit 1
  for arg in "$@"; do
    if [[ $arg == type=bind,src=*,dst=/output ]]; then
      dest=${arg#type=bind,src=}; dest=${dest%,dst=/output}
    fi
  done
  mkdir "$dest/release"
  printf '{"snapshot":{"version":"%s"}}' "$NEW_SHA" > "$dest/release/receipt.json"
  ;;
dokku)
  echo restart >> "$ROOT/restarts"
  [[ $MODE != restart-failure || $(wc -l < "$ROOT/restarts") -gt 1 ]]
  ;;
curl)
  [[ $MODE != health-failure ]] || exit 1
  printf '{"status":"ok","snapshot":{"version":"%s"}}' "$NEW_SHA"
  ;;
jq)
  if [[ $1 == -r ]]; then
    sed -n 's/.*"version":"\([^"]*\)".*/\1/p' "$3"
  else
    grep -q "$NEW_SHA"
  fi
  ;;
sleep) : ;;
esac
`
			for _, name := range []string{"docker", "dokku", "curl", "jq", "sleep"} {
				if err := os.WriteFile(filepath.Join(bin, name), []byte(mock), 0755); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("bash", "../../scripts/update-snapshot", "search", root, "https://example.invalid/corpus.tar.gz", sha, "http://example.invalid/api/v1/health")
			cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "MODE="+mode, "ROOT="+root, "NEW_SHA="+sha, "SEARCH_IMAGE=sha256:"+strings.Repeat("b", 64))
			out, err := cmd.CombinedOutput()
			if (err == nil) != (mode == "success") {
				t.Fatalf("%s: %v %s", mode, err, out)
			}
			current, err := os.Readlink(filepath.Join(root, "current"))
			if err != nil {
				t.Fatal(err)
			}
			want := "releases/old"
			if mode == "success" {
				want = "releases/" + sha
			}
			if current != want {
				t.Fatalf("current=%s want=%s: %s", current, want, out)
			}
			if mode == "success" {
				previous, err := os.Readlink(filepath.Join(root, "previous"))
				if err != nil || previous != "releases/old" {
					t.Fatal("previous snapshot lost")
				}
			}
			if mode == "restart-failure" || mode == "health-failure" {
				b, _ := os.ReadFile(filepath.Join(root, "restarts"))
				if strings.Count(string(b), "restart") != 2 {
					t.Fatal("rollback did not restart", string(out))
				}
			}
			left, _ := filepath.Glob(filepath.Join(root, ".update.*"))
			for _, path := range left {
				if filepath.Base(path) != ".update.lock" {
					t.Fatal("staging left behind", path)
				}
			}
		})
	}
}
