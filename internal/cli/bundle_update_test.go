package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHostBundleActivation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("host updater requires Linux")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Fatal("jq required for bundle updater tests")
	}
	for _, mode := range []string{"success", "checksum", "unsafe-path", "symlink", "startup-failure", "restart-failure", "health-failure", "image-change"} {
		t.Run(mode, func(t *testing.T) {
			root, bin := t.TempDir(), t.TempDir()
			old := filepath.Join(root, "releases", "old")
			mustMkdir := func(path string) {
				t.Helper()
				if err := os.MkdirAll(path, 0755); err != nil {
					t.Fatal(err)
				}
			}
			write := func(path string, data []byte, perm os.FileMode) {
				t.Helper()
				if err := os.WriteFile(path, data, perm); err != nil {
					t.Fatal(err)
				}
			}
			mustMkdir(old)
			write(filepath.Join(old, "receipt.json"), []byte(`{"snapshot":{"version":"old"}}`), 0644)
			if err := os.Symlink("releases/old", filepath.Join(root, "current")); err != nil {
				t.Fatal(err)
			}
			version := strings.Repeat("c", 64)
			var buffer bytes.Buffer
			gz := gzip.NewWriter(&buffer)
			tw := tar.NewWriter(gz)
			manifest := []byte(fmt.Sprintf(`{"projects":2,"packages":3,"snapshot":{"version":%q}}`, version))
			header := &tar.Header{Name: "manifest.json", Mode: 0644, Size: int64(len(manifest))}
			if mode == "unsafe-path" {
				header.Name = "../escaped"
			}
			if mode == "symlink" {
				header.Typeflag = tar.TypeSymlink
				header.Linkname = "/etc/passwd"
				header.Size = 0
			}
			if err := tw.WriteHeader(header); err != nil {
				t.Fatal(err)
			}
			if mode != "symlink" {
				if _, err := tw.Write(manifest); err != nil {
					t.Fatal(err)
				}
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			archive := filepath.Join(root, "input.tar.gz")
			write(archive, buffer.Bytes(), 0644)
			sha := fmt.Sprintf("%x", sha256.Sum256(buffer.Bytes()))
			if mode == "checksum" {
				sha = strings.Repeat("a", 64)
			}
			mock := `#!/usr/bin/env bash
set -eu
case "$(basename "$0")" in
dokku)
  if [[ $1 == ps:inspect ]]; then
    echo inspect >> "$ROOT/inspects"
    image=$APP_IMAGE
    if [[ $MODE == image-change && $(wc -l < "$ROOT/inspects") -gt 1 ]]; then image=sha256:changed; fi
    printf '[{"State":{"Running":true},"Image":"%s"}]' "$image"
  else
    echo restart >> "$ROOT/restarts"
    [[ $MODE != restart-failure || $(wc -l < "$ROOT/restarts") -gt 1 ]]
  fi ;;
docker)
  case "$1" in
    run) printf 'candidate' ;;
    port) printf '127.0.0.1:12345' ;;
    rm) echo removed >> "$ROOT/removed" ;;
    logs) : ;;
    *) exit 1 ;;
  esac ;;
curl)
  while (($#)); do
    if [[ $1 == -o ]]; then cp "$ARCHIVE" "$2"; exit; fi
    if [[ $1 == http://127.0.0.1:* ]]; then preflight=1; fi
    shift
  done
  if [[ ${preflight:-0} == 1 ]]; then [[ $MODE != startup-failure ]] || exit 1
  else [[ $MODE != health-failure ]] || exit 1; fi
  printf '{"status":"ok","projects":2,"snapshot":{"version":"%s"}}' "$VERSION"
  ;;
sleep) : ;;
esac
`
			for _, name := range []string{"dokku", "docker", "curl", "sleep"} {
				write(filepath.Join(bin, name), []byte(mock), 0755)
			}
			cmd := exec.Command("bash", "../../scripts/update-snapshot", "--bundle", "search", root, "https://example.invalid/search.tar.gz", sha, "http://example.invalid/api/v1/health")
			cmd.Env = append(os.Environ(), "SEARCH_IMAGE=", "PATH="+bin+":"+os.Getenv("PATH"), "ROOT="+root, "MODE="+mode, "ARCHIVE="+archive, "VERSION="+version, "APP_IMAGE=sha256:"+strings.Repeat("b", 64))
			output, err := cmd.CombinedOutput()
			if (err == nil) != (mode == "success") {
				t.Fatalf("%v: %s", err, output)
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
				t.Fatalf("current=%s want=%s: %s", current, want, output)
			}
			if mode == "success" {
				previous, err := os.Readlink(filepath.Join(root, "previous"))
				if err != nil || previous != "releases/old" {
					t.Fatal("rollback snapshot lost", err)
				}
			}
			if mode == "restart-failure" || mode == "health-failure" {
				data, _ := os.ReadFile(filepath.Join(root, "restarts"))
				if strings.Count(string(data), "restart") != 2 {
					t.Fatal("rollback restart missing", string(output))
				}
			}
			if mode == "startup-failure" {
				if _, err := os.Stat(filepath.Join(root, "removed")); err != nil {
					t.Fatal("candidate leaked")
				}
			}
		})
	}
}
