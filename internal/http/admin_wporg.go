package http

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/roots/wp-packages/internal/app"
	"github.com/roots/wp-packages/internal/wporg"
)

// handleAdminWporgCheck queries the WordPress.org API for a single
// plugin/theme slug and returns the raw JSON response so admins can
// spot-check exactly what wp.org is reporting (including closure
// payloads, which the regular client converts to typed errors).
func handleAdminWporgCheck(a *app.App) http.HandlerFunc {
	httpClient := &http.Client{Timeout: 30 * time.Second}
	return func(w http.ResponseWriter, r *http.Request) {
		pkgType := r.URL.Query().Get("type")
		slug := strings.TrimSpace(r.URL.Query().Get("slug"))

		if pkgType != "plugin" && pkgType != "theme" {
			writeWporgCheckErr(w, http.StatusBadRequest, "type must be plugin or theme")
			return
		}
		if slug == "" {
			writeWporgCheckErr(w, http.StatusBadRequest, "slug is required")
			return
		}

		apiURL := buildWporgURL(pkgType, slug)
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, apiURL, nil)
		if err != nil {
			writeWporgCheckErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		req.Header.Set("User-Agent", wporg.UserAgent)

		apiResp, err := httpClient.Do(req)
		if err != nil {
			writeWporgCheckErr(w, http.StatusBadGateway, err.Error())
			return
		}
		defer func() { _ = apiResp.Body.Close() }()

		body, err := io.ReadAll(apiResp.Body)
		if err != nil {
			writeWporgCheckErr(w, http.StatusBadGateway, err.Error())
			return
		}

		resp := map[string]any{
			"type":        pkgType,
			"slug":        slug,
			"url":         apiURL,
			"http_status": apiResp.StatusCode,
		}

		var parsed any
		if jsonErr := json.Unmarshal(body, &parsed); jsonErr == nil {
			resp["data"] = parsed
			resp["status"] = wporgCheckStatus(apiResp.StatusCode, parsed)
		} else {
			resp["raw"] = string(body)
			resp["status"] = "error"
			resp["error"] = "response was not JSON: " + jsonErr.Error()
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func buildWporgURL(pkgType, slug string) string {
	if pkgType == "plugin" {
		return "https://api.wordpress.org/plugins/info/1.2/?action=plugin_information" +
			"&request%5Bslug%5D=" + url.QueryEscape(slug)
	}
	return "https://api.wordpress.org/themes/info/1.2/?action=theme_information" +
		"&request%5Bslug%5D=" + url.QueryEscape(slug)
}

func wporgCheckStatus(httpStatus int, parsed any) string {
	obj, ok := parsed.(map[string]any)
	if !ok {
		if httpStatus == http.StatusNotFound {
			return "not_found"
		}
		return "open"
	}
	if errMsg, hasErr := obj["error"]; hasErr {
		if errMsg == "closed" {
			if desc, _ := obj["description"].(string); strings.Contains(desc, "This closure is permanent.") {
				return "closed_permanent"
			}
			return "closed"
		}
		return "error"
	}
	if httpStatus == http.StatusNotFound {
		return "not_found"
	}
	return "open"
}

func writeWporgCheckErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "error", "error": msg})
}
