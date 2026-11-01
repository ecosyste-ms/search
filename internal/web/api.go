package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ecosyste-ms/search/internal/telemetry"
)

func (e *webServer) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "projects": e.Count, "snapshot": e.Snapshot})
}
func (e *webServer) searchJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	values := r.URL.Query()
	q := strings.TrimSpace(values.Get("q"))
	if q == "" || len(q) > 500 {
		http.Error(w, "query must contain 1..500 bytes", http.StatusBadRequest)
		return
	}
	for name := range values {
		if name != "q" && name != "suggestions" && name != "limit" && name != "ecosystem" {
			http.Error(w, "unsupported search option: "+name, http.StatusBadRequest)
			return
		}
	}
	ecosystem := strings.TrimSpace(values.Get("ecosystem"))
	if len(values["ecosystem"]) > 1 || len(ecosystem) > 100 {
		http.Error(w, "ecosystem must be a single value of at most 100 bytes", http.StatusBadRequest)
		return
	}
	suggestions := true
	switch values.Get("suggestions") {
	case "", "true":
	case "false":
		suggestions = false
	default:
		http.Error(w, "suggestions must be true or false", http.StatusBadRequest)
		return
	}
	display := 10
	switch values.Get("limit") {
	case "", "10":
	case "100":
		display = 100
	default:
		http.Error(w, "limit must be 10 or 100", http.StatusBadRequest)
		return
	}
	result, err := e.Search(r.Context(), q, ecosystem, suggestions, display)
	if err != nil {
		telemetry.RecordError(r.Context(), err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := e.AttachPublicDetails(result.Hits); err != nil {
		telemetry.RecordError(r.Context(), err)
		http.Error(w, "Unable to read project details", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", searchCacheControl)
	_ = json.NewEncoder(w).Encode(result)
}
