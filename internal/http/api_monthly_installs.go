package http

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/roots/wp-packages/internal/app"
	"github.com/roots/wp-packages/internal/telemetry"
)

func handleAPIMonthlyInstalls(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pkgType := strings.TrimPrefix(r.PathValue("type"), "wp-")
		name := r.PathValue("name")

		var packageID int64
		err := a.DB.QueryRowContext(r.Context(),
			`SELECT id FROM packages WHERE type = ? AND name = ? AND is_active = 1`,
			pkgType, name,
		).Scan(&packageID)
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "package not found", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		installs, err := telemetry.GetMonthlyInstalls(r.Context(), a.DB, packageID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if installs == nil {
			installs = []telemetry.MonthlyInstall{}
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_ = json.NewEncoder(w).Encode(installs)
	}
}
