package http

import (
	"encoding/json"
	"net/http"

	"github.com/roots/wp-packages/internal/app"
)

// handleAPIClosedPackages returns slugs of closed packages. When permanentOnly
// is true, the result is restricted to packages flagged permanently_closed = 1
// (a stable subset of is_active = 0). Otherwise it returns every is_active = 0
// row, which includes both temporary and permanent closures.
func handleAPIClosedPackages(a *app.App, permanentOnly bool) http.HandlerFunc {
	where := "is_active = 0"
	if permanentOnly {
		where = "permanently_closed = 1"
	}
	query := `SELECT name FROM packages WHERE type = ? AND ` + where + ` ORDER BY name`

	return func(w http.ResponseWriter, r *http.Request) {
		pkgType, ok := parsePkgType(r.PathValue("type"))
		if !ok {
			http.Error(w, "unknown package type", http.StatusNotFound)
			return
		}

		rows, err := a.DB.QueryContext(r.Context(), query, pkgType)
		if err != nil {
			a.Logger.Error("querying closed packages", "error", err, "type", pkgType, "permanent", permanentOnly)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		defer func() { _ = rows.Close() }()

		slugs := []string{}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				a.Logger.Error("scanning closed packages", "error", err)
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			slugs = append(slugs, name)
		}
		if err := rows.Err(); err != nil {
			a.Logger.Error("iterating closed packages", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_ = json.NewEncoder(w).Encode(slugs)
	}
}
