package search

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"

	"github.com/ecosyste-ms/search/internal/storage"
)

func IndexFileHashes(root string) (map[string]string, error) {
	hashes := map[string]string{}
	for _, name := range []string{"index_meta.json", "store"} {
		err := filepath.WalkDir(filepath.Join(root, name), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if !entry.Type().IsRegular() {
				return fmt.Errorf("non-regular index file: %s", path)
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			hashes[filepath.ToSlash(rel)], err = storage.FileSHA(path)
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	return hashes, nil
}

func verifySnapshotIndex(indexPath, dir string, manifest compactExport) error {
	const name = "index-checksums.json"
	expected := manifest.Files[name].SHA256
	if expected == "" {
		return fmt.Errorf("snapshot manifest has no index checksum binding; re-export the snapshot")
	}
	path := filepath.Join(dir, name)
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if fmt.Sprintf("%x", sha256.Sum256(body)) != expected {
		return fmt.Errorf("index checksum manifest mismatch")
	}
	var hashes map[string]string
	if err := json.Unmarshal(body, &hashes); err != nil {
		return err
	}
	actual, err := IndexFileHashes(indexPath)
	if err != nil {
		return err
	}
	if !maps.Equal(actual, hashes) {
		return fmt.Errorf("index snapshot checksum mismatch")
	}
	return nil
}
