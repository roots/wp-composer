package http

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/roots/wp-packages/internal/app"
)

type changesResponse struct {
	Timestamp int64          `json:"timestamp"`
	Actions   []changeAction `json:"actions,omitempty"`
	Error     string         `json:"error,omitempty"`
}

type changeAction struct {
	Type    string `json:"type"`
	Package string `json:"package"`
	Time    int64  `json:"time"`
}

const changesRetention = 24 * time.Hour

func handleMetadataChanges(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=60")

		now := time.Now()
		nowMs := now.UnixMilli()

		sinceStr := r.URL.Query().Get("since")
		if sinceStr == "" {
			_ = json.NewEncoder(w).Encode(changesResponse{
				Timestamp: nowMs,
				Error:     `Missing or invalid "since" query parameter. Use the timestamp from a previous response.`,
			})
			return
		}

		since, err := strconv.ParseInt(sinceStr, 10, 64)
		if err != nil || since < 0 {
			_ = json.NewEncoder(w).Encode(changesResponse{
				Timestamp: nowMs,
				Error:     `Missing or invalid "since" query parameter. Use the timestamp from a previous response.`,
			})
			return
		}

		// If since is older than retention window, tell client to resync
		retentionFloor := now.Add(-changesRetention).UnixMilli()
		if since < retentionFloor {
			_ = json.NewEncoder(w).Encode(changesResponse{
				Timestamp: nowMs,
				Actions: []changeAction{
					{Type: "resync", Package: "*", Time: now.Unix()},
				},
			})
			return
		}

		// Capture checkpoint from existing data to avoid race with concurrent inserts
		var checkpoint int64
		err = a.DB.QueryRowContext(r.Context(),
			`SELECT COALESCE(MAX(timestamp), 0) FROM metadata_changes`).Scan(&checkpoint)
		if err != nil {
			captureError(r, err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if checkpoint == 0 {
			checkpoint = nowMs
		}

		rows, err := a.DB.QueryContext(r.Context(),
			`SELECT id, package_name, action, timestamp
			 FROM metadata_changes
			 WHERE timestamp > ? AND timestamp <= ?
			 ORDER BY timestamp ASC, id ASC`, since, checkpoint)
		if err != nil {
			captureError(r, err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		// Deduplicate: keep only the latest action per package (highest id wins
		// because rows are ordered by id ASC, so later overwrites earlier)
		seen := map[string]changeAction{}
		for rows.Next() {
			var id int64
			var name, action string
			var ts int64
			if err := rows.Scan(&id, &name, &action, &ts); err != nil {
				captureError(r, err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			seen[name] = changeAction{Type: action, Package: name, Time: ts / 1000}
		}
		if err := rows.Err(); err != nil {
			captureError(r, err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		actions := make([]changeAction, 0, len(seen))
		for _, a := range seen {
			actions = append(actions, a)
		}
		sort.Slice(actions, func(i, j int) bool {
			return actions[i].Package < actions[j].Package
		})

		_ = json.NewEncoder(w).Encode(changesResponse{
			Timestamp: checkpoint,
			Actions:   actions,
		})
	}
}
