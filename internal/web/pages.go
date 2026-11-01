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
	URL, DetailsURL string
}

type htmlPage struct {
	Query, Status, Name, Description string
	Hits                             []htmlHit
	Suggestions                      map[string][]string
	Public                           map[string]any
}

func renderDiscovery(w http.ResponseWriter, r *http.Request, name string, data htmlPage) {
	var body bytes.Buffer
	if err := discoveryTemplates.ExecuteTemplate(&body, name, data); err != nil {
		telemetry.RecordError(r.Context(), err)
		http.Error(w, "Unable to render page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(body.Bytes())
}

func (e *webServer) searchPage(w http.ResponseWriter, r *http.Request) {
	page := htmlPage{Query: strings.TrimSpace(r.URL.Query().Get("q")), Status: "Enter a query and press Search."}
	if len(page.Query) > 500 {
		http.Error(w, "query must contain at most 500 bytes", http.StatusBadRequest)
		return
	}
	if page.Query != "" {
		start := time.Now()
		found, err := e.Search(r.Context(), page.Query, "", true, 100)
		if err != nil {
			telemetry.RecordError(r.Context(), err)
			http.Error(w, "Unable to search", http.StatusInternalServerError)
			return
		}
		e.AttachIdentities(found.Hits)
		for _, h := range found.Hits {
			details := "/project/" + url.PathEscape(h.ID)
			link := details
			if h.Identity != nil && h.Identity.URL != "" {
				link = h.Identity.URL
			}
			page.Hits = append(page.Hits, htmlHit{h, link, details})
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
	if p != nil {
		page.Public = search.PublicResponse(p, nil)
	}
	name := "project.html"
	if r.URL.Query().Get("fragment") == "identities" {
		name = "identities"
	}
	renderDiscovery(w, r, name, page)
}
