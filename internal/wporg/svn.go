package wporg

import (
	"bufio"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type SVNEntry struct {
	Slug          string
	LastCommitted *time.Time
}

// SVNListingResult holds the parsed SVN listing along with the repository revision.
type SVNListingResult struct {
	Revision int64
}

// ParseSVNListing fetches the SVN HTML directory listing and extracts slugs.
// It also returns the current SVN revision from the page header.
func (c *Client) ParseSVNListing(ctx context.Context, baseURL string, fn func(SVNEntry) error) (*SVNListingResult, error) {
	client := &http.Client{Timeout: 600 * time.Second}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating SVN request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching SVN listing: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SVN listing returned status %d", resp.StatusCode)
	}

	return parseSVNHTML(ctx, resp.Body, fn, c.logger)
}

func parseSVNHTML(ctx context.Context, r interface{ Read([]byte) (int, error) }, fn func(SVNEntry) error, logger *slog.Logger) (*SVNListingResult, error) {
	scanner := bufio.NewScanner(r)
	result := &SVNListingResult{}

	var count int
	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		line := scanner.Text()

		// Extract revision from title: " - Revision 3483213: /"
		if result.Revision == 0 {
			if rev := parseSVNRevision(line); rev > 0 {
				result.Revision = rev
			}
		}

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
			return result, err
		}

		count++
		if count%10000 == 0 {
			logger.Info("SVN discovery progress", "entries", count)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading SVN listing: %w", err)
	}

	logger.Info("SVN discovery complete", "total_entries", count, "revision", result.Revision)
	return result, nil
}

// parseSVNRevision extracts a revision number from lines like:
//
//	<title> - Revision 3483213: /</title>
//	<h2> - Revision 3483213: /</h2>
func parseSVNRevision(line string) int64 {
	const marker = "Revision "
	idx := strings.Index(line, marker)
	if idx < 0 {
		return 0
	}
	rest := line[idx+len(marker):]
	end := strings.IndexAny(rest, ":< ")
	if end < 0 {
		return 0
	}
	rev, err := strconv.ParseInt(rest[:end], 10, 64)
	if err != nil {
		return 0
	}
	return rev
}

// svnLogReport is the XML structure for an SVN DAV log-report response.
type svnLogReport struct {
	Items []svnLogItem `xml:"log-item"`
}

type svnLogItem struct {
	Revision     int64    `xml:"version-name"`
	Date         string   `xml:"date"`
	AddedPaths   []string `xml:"added-path"`
	ModifiedPaths []string `xml:"modified-path"`
	DeletedPaths []string `xml:"deleted-path"`
}

// FetchSVNChangedSlugs queries the SVN DAV log between two revisions and returns
// the set of unique top-level slugs (plugin/theme names) that were modified.
func (c *Client) FetchSVNChangedSlugs(ctx context.Context, baseURL string, fromRev, toRev int64) ([]string, error) {
	body := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>`+
		`<S:log-report xmlns:S="svn:" xmlns:D="DAV:">`+
		`<S:start-revision>%d</S:start-revision>`+
		`<S:end-revision>%d</S:end-revision>`+
		`<S:discover-changed-paths/>`+
		`<S:limit>50000</S:limit>`+
		`</S:log-report>`, toRev, fromRev)

	reqURL := strings.TrimSuffix(baseURL, "/") + "/!svn/bc/0/"
	req, err := http.NewRequestWithContext(ctx, "REPORT", reqURL, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating SVN log request: %w", err)
	}
	req.Header.Set("Content-Type", "text/xml")

	// Use a generous timeout — catch-up runs after downtime can span many
	// revisions and produce large responses.
	davClient := &http.Client{Timeout: 600 * time.Second}
	resp, err := davClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching SVN log: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("SVN log returned status %d: %s", resp.StatusCode, string(respBody))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading SVN log response: %w", err)
	}

	return parseSVNLogSlugs(data)
}

// parseSVNLogSlugs extracts unique top-level slugs from SVN log XML.
// Paths look like "/plugin-name/trunk/file.php" — we extract "plugin-name".
func parseSVNLogSlugs(data []byte) ([]string, error) {
	var report svnLogReport
	if err := xml.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("parsing SVN log XML: %w", err)
	}

	seen := make(map[string]struct{})
	for _, item := range report.Items {
		for _, p := range item.AddedPaths {
			if slug := slugFromPath(p); slug != "" {
				seen[slug] = struct{}{}
			}
		}
		for _, p := range item.ModifiedPaths {
			if slug := slugFromPath(p); slug != "" {
				seen[slug] = struct{}{}
			}
		}
		for _, p := range item.DeletedPaths {
			if slug := slugFromPath(p); slug != "" {
				seen[slug] = struct{}{}
			}
		}
	}

	slugs := make([]string, 0, len(seen))
	for slug := range seen {
		slugs = append(slugs, slug)
	}
	return slugs, nil
}

// slugFromPath extracts the top-level directory (slug) from an SVN path.
// e.g. "/akismet/trunk/akismet.php" → "akismet"
func slugFromPath(path string) string {
	path = strings.TrimPrefix(path, "/")
	if idx := strings.IndexByte(path, '/'); idx > 0 {
		return path[:idx]
	}
	// bare directory entry like "akismet"
	if path != "" && path != ".." {
		return path
	}
	return ""
}
