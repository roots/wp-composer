package wporg

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
)

// NewMockServer returns an httptest.Server that serves fixtures from fixtureDir.
// Routes:
//   - GET /plugins/info/1.2/?...&request[slug]=X → testdata/plugins/X.json
//   - GET /themes/info/1.2/?...&request[slug]=X  → testdata/themes/X.json
//
// Returns 404 for unknown slugs. Handles both full info and last_updated-only requests.
func NewMockServer(fixtureDir string) *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/plugins/info/1.2/", func(w http.ResponseWriter, r *http.Request) {
		serveFixture(w, r, filepath.Join(fixtureDir, "plugins"))
	})

	mux.HandleFunc("/themes/info/1.2/", func(w http.ResponseWriter, r *http.Request) {
		serveFixture(w, r, filepath.Join(fixtureDir, "themes"))
	})

	return httptest.NewServer(mux)
}

func serveFixture(w http.ResponseWriter, r *http.Request, dir string) {
	slug := extractSlug(r)
	if slug == "" {
		http.Error(w, `{"error":"missing slug"}`, http.StatusBadRequest)
		return
	}

	data, err := os.ReadFile(filepath.Join(dir, slug+".json"))
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"Plugin not found.","slug":"` + slug + `"}`))
		return
	}

	// If the request only asks for last_updated, return a minimal response
	if isLastUpdatedOnly(r) {
		var full map[string]any
		if err := json.Unmarshal(data, &full); err == nil {
			minimal := map[string]any{
				"last_updated": full["last_updated"],
				"slug":         full["slug"],
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(minimal)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func extractSlug(r *http.Request) string {
	// Check query string (GET-style URLs used by our client)
	q := r.URL.RawQuery
	if slug := extractParam(q, "request%5Bslug%5D"); slug != "" {
		return slug
	}
	if slug := extractParam(q, "request[slug]"); slug != "" {
		return slug
	}
	return ""
}

func extractParam(query, key string) string {
	idx := strings.Index(query, key+"=")
	if idx < 0 {
		return ""
	}
	val := query[idx+len(key)+1:]
	if end := strings.IndexByte(val, '&'); end >= 0 {
		val = val[:end]
	}
	return val
}

func isLastUpdatedOnly(r *http.Request) bool {
	q := r.URL.RawQuery
	// Check if versions=false is in the query (indicates a minimal last_updated-only request)
	return strings.Contains(q, "versions%5D=false") || strings.Contains(q, "versions]=false")
}
