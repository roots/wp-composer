package packagist

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"
)

// DownloadsCache fetches and caches total download counts from packagist.org.
type DownloadsCache struct {
	value  atomic.Int64
	logger *slog.Logger
}

func NewDownloadsCache(logger *slog.Logger) *DownloadsCache {
	c := &DownloadsCache{logger: logger}
	c.value.Store(0)
	c.fetch()
	go c.loop()
	return c
}

// NewStubCache returns a DownloadsCache that never fetches, for use in tests.
func NewStubCache() *DownloadsCache {
	c := &DownloadsCache{logger: slog.Default()}
	c.value.Store(0)
	return c
}

func (c *DownloadsCache) Total() int64 {
	return c.value.Load()
}

func (c *DownloadsCache) loop() {
	ticker := time.NewTicker(1 * time.Hour)
	for range ticker.C {
		c.fetch()
	}
}

func (c *DownloadsCache) fetch() {
	total, err := fetchDownloads("roots/wordpress")
	if err != nil {
		c.logger.Warn("packagist downloads fetch failed", "error", err)
		return
	}
	c.value.Store(total)
	c.logger.Info("packagist downloads updated", "total", total)
}

func fetchDownloads(pkg string) (int64, error) {
	resp, err := http.Get(fmt.Sprintf("https://packagist.org/packages/%s/downloads.json", pkg))
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("packagist returned %d", resp.StatusCode)
	}

	var data struct {
		Package struct {
			Downloads struct {
				Total struct {
					Total int64 `json:"total"`
				} `json:"total"`
			} `json:"downloads"`
		} `json:"package"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return 0, err
	}
	return data.Package.Downloads.Total.Total, nil
}
