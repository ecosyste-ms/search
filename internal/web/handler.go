package web

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/ecosyste-ms/search/internal/search"
	"github.com/ecosyste-ms/search/internal/telemetry"
)

//go:embed assets/*
var discoveryAssets embed.FS

//go:embed openapi.yml
var openAPISpec []byte

const searchCacheControl = "public, max-age=3600, s-maxage=3600"

func Handler(e *search.Engine) http.Handler {
	mux := http.NewServeMux()
	pages := &webServer{e}
	mux.HandleFunc("GET /api/v1/health", pages.health)
	mux.HandleFunc("GET /openapi.yml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(openAPISpec)
	})
	assets, _ := fs.Sub(discoveryAssets, "assets")
	mux.Handle("GET /", http.FileServer(http.FS(assets)))
	mux.Handle("GET /{$}", telemetry.HTTPHandler("GET /{$}", http.HandlerFunc(pages.searchPage)))
	mux.Handle("GET /project/{id}", telemetry.HTTPHandler("GET /project/{id}", http.HandlerFunc(pages.projectPage)))
	mux.Handle("GET /api/v1/search", telemetry.HTTPHandler("GET /api/v1/search", http.HandlerFunc(pages.searchJSON)))
	return publicCORS(mux)
}
