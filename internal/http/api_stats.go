package http

import (
	"encoding/json"
	"net/http"

	"github.com/roots/wp-packages/internal/app"
)

type statsResponse struct {
	TotalInstalls          int64  `json:"total_installs"`
	TotalInstallsFormatted string `json:"total_installs_formatted"`
	Installs30d            int64  `json:"installs_30d"`
	Installs30dFormatted   string `json:"installs_30d_formatted"`
	ActivePlugins          int64  `json:"active_plugins"`
	ActiveThemes           int64  `json:"active_themes"`
	TotalPackages          int64  `json:"total_packages"`
}

func handleAPIStats(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var s statsResponse
		err := a.DB.QueryRowContext(r.Context(),
			`SELECT active_plugins, active_themes, active_plugins + active_themes,
				plugin_installs + theme_installs, installs_30d
			 FROM package_stats WHERE id = 1`,
		).Scan(&s.ActivePlugins, &s.ActiveThemes, &s.TotalPackages, &s.TotalInstalls, &s.Installs30d)
		if err != nil {
			http.Error(w, "stats unavailable", http.StatusInternalServerError)
			return
		}
		s.TotalInstallsFormatted = formatNumber(s.TotalInstalls)
		s.Installs30dFormatted = formatNumber(s.Installs30d)

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_ = json.NewEncoder(w).Encode(s)
	}
}
