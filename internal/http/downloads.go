package http

import (
	"encoding/json"
	"net"
	"net/http"

	"github.com/roots/wp-packages/internal/app"
	"github.com/roots/wp-packages/internal/telemetry"
)

type notifyBatchRequest struct {
	Downloads []downloadEntry `json:"downloads"`
}

type downloadEntry struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

const maxDownloadsRequestBodyBytes = 1 << 20 // 1 MiB

func handleDownloads(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req notifyBatchRequest
		r.Body = http.MaxBytesReader(w, r.Body, maxDownloadsRequestBodyBytes)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
			return
		}

		// Cap batch size
		downloads := req.Downloads
		if len(downloads) > 100 {
			downloads = downloads[:100]
		}

		ip := clientIP(r)
		ipHash := telemetry.HashIP(ip)
		uaHash := telemetry.HashUserAgent(r.Header.Get("User-Agent"))
		dedupeWindow := a.Config.Telemetry.DedupeWindowSeconds

		for _, dl := range downloads {
			if dl.Name == "" || dl.Version == "" {
				continue
			}

			pkgID, err := telemetry.LookupPackageID(r.Context(), a.DB, dl.Name)
			if err != nil || pkgID == 0 {
				continue
			}

			_, _ = telemetry.RecordInstall(r.Context(), a.DB, telemetry.InstallParams{
				PackageID:     pkgID,
				Version:       dl.Version,
				IPHash:        ipHash,
				UserAgentHash: uaHash,
			}, dedupeWindow)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

// clientIP extracts the IP address from RemoteAddr, stripping the port.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr // fallback if no port
	}
	return host
}
