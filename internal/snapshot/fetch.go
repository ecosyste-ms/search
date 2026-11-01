package snapshot

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/ecosyste-ms/search/internal/search"
	"github.com/ecosyste-ms/search/internal/storage"
)

type releaseOptions struct {
	URL, SHA, Output string
}

func Command(args []string, out io.Writer) error {
	f := flag.NewFlagSet("snapshot", flag.ContinueOnError)
	var o releaseOptions
	f.StringVar(&o.URL, "url", "", "published HTTP(S) corpus archive URL")
	f.StringVar(&o.SHA, "sha256", "", "required archive SHA256 from the publisher")
	f.StringVar(&o.Output, "output", "", "new release directory; existing directories are never replaced")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 || o.URL == "" || o.Output == "" {
		return fmt.Errorf("snapshot requires -url, -sha256 and -output")
	}
	if !validSHA(o.SHA) {
		return fmt.Errorf("archive SHA256 required")
	}
	return buildRelease(context.Background(), o, out)
}

func validatePublishedURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("snapshot URLs must use HTTP(S), without userinfo or fragments")
	}
	return nil
}

func download(ctx context.Context, raw string, dst io.Writer, limit int64) (int64, error) {
	if err := validatePublishedURL(raw); err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return 0, fmt.Errorf("invalid download request")
	}
	req.Header.Set("Accept-Encoding", "identity")
	client := &http.Client{Timeout: 30 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		return validatePublishedURL(req.URL.String())
	}}
	resp, err := client.Do(req)
	if err != nil {
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return 0, fmt.Errorf("snapshot download failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("snapshot download returned HTTP %d", resp.StatusCode)
	}
	n, err := io.Copy(dst, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return n, err
	}
	if n > limit {
		return n, fmt.Errorf("download exceeds declared size limit")
	}
	return n, nil
}

func buildRelease(ctx context.Context, o releaseOptions, out io.Writer) error {
	if _, err := os.Lstat(o.Output); !os.IsNotExist(err) {
		return fmt.Errorf("release output already exists or cannot be inspected")
	}
	parent := filepath.Dir(o.Output)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".search-snapshot-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	m, err := fetchCorpus(ctx, o, stage)
	if err != nil {
		return err
	}
	input := filepath.Join(stage, "source-projects.ndjson.gz")
	if err := validatePublished(input, m); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "Validated %d projects and %d package memberships\n", m.Projects, m.Packages)
	snapshot := filepath.Join(stage, "snapshot")
	if err := buildPublished(input, snapshot, m); err != nil {
		return err
	}
	e, err := search.Open(filepath.Join(snapshot, "index"), snapshot)
	if err != nil {
		return err
	}
	if err := e.Close(); err != nil {
		return err
	}
	if err := storage.ArchiveSnapshot(snapshot, filepath.Join(stage, "search.tar.gz")); err != nil {
		return err
	}
	if err := os.Remove(input); err != nil {
		return err
	}
	bundleSHA, err := storage.FileSHA(filepath.Join(stage, "search.tar.gz"))
	if err != nil {
		return err
	}
	result := map[string]any{"snapshot": m.Snapshot, "projects": m.Projects, "packages": m.Packages, "bundle_sha256": bundleSHA}
	if err := storage.WriteJSON(filepath.Join(stage, "receipt.json"), result); err != nil {
		return err
	}
	if _, err := os.Lstat(o.Output); !os.IsNotExist(err) {
		return fmt.Errorf("release output appeared during build; refusing replacement")
	}
	if err := os.Chmod(stage, 0755); err != nil {
		return err
	}
	if err := os.Rename(stage, o.Output); err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(result)
}
