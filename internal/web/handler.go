package web

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/ecosyste-ms/search/internal/search"
)

//go:embed assets/*
var discoveryAssets embed.FS

//go:embed openapi.yml
var openAPISpec []byte

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
	mux.HandleFunc("GET /{$}", pages.searchPage)
	mux.HandleFunc("GET /project/{id}", pages.projectPage)
	mux.HandleFunc("GET /api/v1/search", pages.searchJSON)
	return mux
}
