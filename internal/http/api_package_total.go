package http

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/roots/wp-packages/internal/app"
)

func handleAPIPackageTotal(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pkgType := strings.TrimPrefix(r.PathValue("type"), "wp-")
		name := r.PathValue("name")

		var totalInstalls, installs30d int64
		err := a.DB.QueryRowContext(r.Context(),
			`SELECT wp_packages_installs_total, wp_packages_installs_30d
			 FROM packages WHERE type = ? AND name = ? AND is_active = 1`,
			pkgType, name,
		).Scan(&totalInstalls, &installs30d)
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "package not found", http.StatusNotFound)
			return
		}
		if err != nil {
			a.Logger.Error("looking up package", "error", err, "type", pkgType, "name", name)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_installs":           totalInstalls,
			"total_installs_formatted": formatNumber(totalInstalls),
			"installs_30d":             installs30d,
			"installs_30d_formatted":   formatNumber(installs30d),
		})
	}
}
