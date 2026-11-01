package cli

import (
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/ecosyste-ms/search/internal/storage"
)

func indexSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	hashes := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		hashes[path], err = storage.FileSHA(path)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return hashes
}
