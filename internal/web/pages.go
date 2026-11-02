package web

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ecosyste-ms/search/internal/search"
	"github.com/ecosyste-ms/search/internal/telemetry"
)

var discoveryTemplates = template.Must(template.New("").Funcs(template.FuncMap{"join": strings.Join}).ParseFS(discoveryAssets, "assets/*.html"))

type webServer struct{ *search.Engine }

type htmlHit struct {
	search.Hit
	URL, DetailsURL, IdentitiesURL string
}

type htmlPage struct {
	Query, Status, Name, Description          string
	Hits                                      []htmlHit
	Suggestions                               map[string][]string
	Public                                    map[string]any
	Title, MetaDescription, Canonical, Robots string
}

func renderDiscovery(w http.ResponseWriter, r *http.Request, name string, data htmlPage) {
	var body bytes.Buffer
	if err := discoveryTemplates.ExecuteTemplate(&body, name, data); err != nil {
		telemetry.RecordError(r.Context(), err)
		http.Error(w, "Unable to render page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", searchCacheControl)
	_, _ = w.Write(body.Bytes())
}

func (e *webServer) searchPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	page := htmlPage{Query: strings.TrimSpace(r.URL.Query().Get("q")), Status: "Enter a query and press Search."}
	page.Title = "Search popular packages · ecosyste.ms"
	page.MetaDescription = "Search popular open source packages and projects by name or description, with package identities and links across registries."
	page.Canonical = "https://search.ecosyste.ms/"
	if len(page.Query) > 500 {
		http.Error(w, "query must contain at most 500 bytes", http.StatusBadRequest)
		return
	}
	if page.Query != "" {
		page.Title = metadataText(page.Query, 100) + " · ecosyste.ms search"
		page.MetaDescription = "Search results for " + metadataText(page.Query, 100) + ". Explore packages, projects and their public identifiers across registries."
		page.Canonical += "?" + url.Values{"q": {page.Query}}.Encode()
		page.Robots = "noindex, follow"
		start := time.Now()
		found, err := e.Search(r.Context(), page.Query, "", true, 100)
		if err != nil {
			telemetry.RecordError(r.Context(), err)
			http.Error(w, "Unable to search", http.StatusInternalServerError)
			return
		}
		e.AttachIdentities(found.Hits)
		for _, h := range found.Hits {
			details := "/project/" + url.PathEscape(h.ID) + "?" + url.Values{"q": {page.Query}}.Encode()
			link := details
			if h.Identity != nil && h.Identity.URL != "" {
				link = h.Identity.URL
			}
			page.Hits = append(page.Hits, htmlHit{h, link, details, details + "&fragment=identities"})
		}
		page.Suggestions = found.Suggestions
		page.Status = fmt.Sprintf("Showing %d results · %d ms", len(page.Hits), time.Since(start).Milliseconds())
		if len(page.Hits) == 0 {
			page.Status = "No matching projects."
		}
	}
	renderDiscovery(w, r, "index.html", page)
}

func (e *webServer) projectPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(query) > 500 {
		http.Error(w, "query must contain at most 500 bytes", http.StatusBadRequest)
		return
	}
	ordinal, err := e.Ordinal(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	description, err := e.Description(r.Context(), ordinal)
	if err != nil {
		telemetry.RecordError(r.Context(), err)
		http.Error(w, "Unable to read description", http.StatusInternalServerError)
		return
	}
	p, err := e.PublicProject(ordinal)
	if err != nil {
		telemetry.RecordError(r.Context(), err)
		http.Error(w, "Unable to read identifiers", http.StatusInternalServerError)
		return
	}
	page := htmlPage{Name: e.DisplayName(ordinal), Description: description}
	page.Title = metadataText(page.Name, 100) + " · ecosyste.ms search"
	page.MetaDescription = metadataText(description, 160)
	if page.MetaDescription == "" {
		page.MetaDescription = "Package identities and repository links for " + metadataText(page.Name, 100) + "."
	}
	page.Canonical = "https://search.ecosyste.ms/project/" + url.PathEscape(r.PathValue("id"))
	page.Robots = "noindex, follow"
	if p != nil {
		page.Public = e.PublicResponse(p, nil, query)
	}
	name := "project.html"
	if r.URL.Query().Get("fragment") == "identities" {
		name = "identities"
	}
	renderDiscovery(w, r, name, page)
}

func metadataText(text string, limit int) string {
	runes := []rune(strings.Join(strings.Fields(text), " "))
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return string(runes)
}
