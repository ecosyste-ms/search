package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ecosyste-ms/search/internal/search"
	"github.com/ecosyste-ms/search/internal/web"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestSearchHTTPErrorMonitoring(t *testing.T) {
	output := filepath.Join(t.TempDir(), "release")
	if err := Run(snapshotArgs(t, packFixture(t, archiveFixture(t), nil), output), io.Discard); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(output, "snapshot")
	e, err := search.Open(filepath.Join(dir, "index"), dir)
	if err != nil {
		t.Fatal(err)
	}
	closed := false
	t.Cleanup(func() {
		if !closed {
			_ = e.Close()
		}
	})
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	server := httptest.NewServer(web.Handler(e))
	defer server.Close()
	get := func(path string, status int, content string) {
		t.Helper()
		resp, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != status || !strings.Contains(string(body), content) {
			t.Fatalf("%s: %d %s", path, resp.StatusCode, body)
		}
	}
	get("/api/v1/search?q=curl&ecosystem=nixpkgs", 200, "pkg:nix/curl")
	get("/?q=curl", 200, "curl")
	get("/project/project:curl?fragment=identities", 200, "pkg:nix/curl")
	get("/api/v1/search?q=private-query&limit=invalid", 400, "limit must")
	get("/api/v1/health", 200, "ok")
	get("/search.js", 200, "")
	closed = true
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	get("/api/v1/search?q=private-query", 500, "")
	get("/?q=private-query", 500, "Unable to search")
	spans := exporter.GetSpans()
	if len(spans) != 6 {
		t.Fatalf("got %d spans, want six application requests", len(spans))
	}
	for i, span := range spans {
		wantError := i >= 4
		if (span.Status.Code == codes.Error) != wantError || (len(span.Events) > 0) != wantError {
			t.Errorf("request %d: status=%v events=%v", i, span.Status, span.Events)
		}
		for _, attr := range span.Attributes {
			if attr.Key == "url.query" || strings.Contains(attr.Value.AsString(), "private-query") {
				t.Errorf("query leaked to monitoring: %v", attr)
			}
		}
		if strings.Contains(span.Name, "project:curl") {
			t.Errorf("span name contains snapshot ID: %s", span.Name)
		}
	}
}
