package wporg

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type SVNEntry struct {
	Slug          string
	LastCommitted *time.Time
}

// ParseSVNListing fetches the SVN HTML directory listing and extracts slugs.
func (c *Client) ParseSVNListing(ctx context.Context, baseURL string, fn func(SVNEntry) error) error {
	client := &http.Client{Timeout: 600 * time.Second}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return fmt.Errorf("creating SVN request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetching SVN listing: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SVN listing returned status %d", resp.StatusCode)
	}

	return parseSVNHTML(ctx, resp.Body, fn, c.logger)
}

func parseSVNHTML(ctx context.Context, r interface{ Read([]byte) (int, error) }, fn func(SVNEntry) error, logger *slog.Logger) error {
	scanner := bufio.NewScanner(r)
	// HTML lines are short; default buffer is fine.

	var count int
	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		line := scanner.Text()

		// Each entry is: <li><a href="slug-name/">slug-name/</a></li>
		idx := strings.Index(line, `<a href="`)
		if idx < 0 {
			continue
		}
		rest := line[idx+len(`<a href="`):]
		end := strings.IndexByte(rest, '"')
		if end < 0 {
			continue
		}
		href := rest[:end]

		slug := strings.TrimSuffix(href, "/")
		if slug == "" || slug == ".." || strings.HasPrefix(slug, "!svn") {
			continue
		}

		if err := fn(SVNEntry{Slug: slug}); err != nil {
			return err
		}

		count++
		if count%10000 == 0 {
			logger.Info("SVN discovery progress", "entries", count)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading SVN listing: %w", err)
	}

	logger.Info("SVN discovery complete", "total_entries", count)
	return nil
}
