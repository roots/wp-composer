package http

import (
	"encoding/json"
	"net/http"

	"github.com/roots/wp-packages/internal/app"
)

func handleAPIClosedPackages(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pkgType, ok := parsePkgType(r.PathValue("type"))
		if !ok {
			http.Error(w, "unknown package type", http.StatusNotFound)
			return
		}

		rows, err := a.DB.QueryContext(r.Context(),
			`SELECT name FROM packages
			 WHERE type = ? AND permanently_closed = 1
			 ORDER BY name`,
			pkgType,
		)
		if err != nil {
			a.Logger.Error("querying closed packages", "error", err, "type", pkgType)
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
