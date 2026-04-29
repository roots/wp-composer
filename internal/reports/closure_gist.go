// Package reports builds and publishes operational reports about package
// activity, such as closure summaries posted to GitHub Gists.
package reports

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ClosureGistVendorMin is the minimum number of plugin closures from a single
// vendor (author) in a status check run before a report gist is posted.
const ClosureGistVendorMin = 2

var htmlTagRE = regexp.MustCompile(`<[^>]*>`)

type closure struct {
	Slug   string
	Author string
}

type vendorGroup struct {
	Name  string
	Items []closure
}

// PostClosureReport posts a private gist summarising plugin closures from a
// status check run, grouping plugins under any vendor (author) that owns
// more than one closure. Returns the gist URL, or "" if no gist was posted
// (token missing, too few closures, or no vendor with >1 closure).
func PostClosureReport(ctx context.Context, db *sql.DB, runID int64, runStarted time.Time, token string) (string, error) {
	if token == "" {
		return "", nil
	}

	closures, err := loadClosures(ctx, db, runID)
	if err != nil {
		return "", err
	}

	multi := groupByVendor(closures)
	if len(multi) == 0 {
		return "", nil
	}

	md := renderMarkdown(runStarted, multi)
	return postGist(ctx, token, runStarted, md)
}

func loadClosures(ctx context.Context, db *sql.DB, runID int64) ([]closure, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT scc.package_name, COALESCE(p.author, '')
		FROM status_check_changes scc
		LEFT JOIN packages p ON p.type = scc.package_type AND p.name = scc.package_name
		WHERE scc.status_check_id = ?
		  AND scc.package_type = 'plugin'
		  AND scc.action IN ('deactivated','tombstoned')`, runID)
	if err != nil {
		return nil, fmt.Errorf("querying closures: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []closure
	for rows.Next() {
		var c closure
		if err := rows.Scan(&c.Slug, &c.Author); err != nil {
			return nil, err
		}
		c.Author = strings.TrimSpace(htmlTagRE.ReplaceAllString(c.Author, ""))
		out = append(out, c)
	}
	return out, rows.Err()
}

func groupByVendor(closures []closure) []vendorGroup {
	groups := map[string][]closure{}
	displayName := map[string]string{}
	for _, c := range closures {
		key := strings.ToLower(c.Author)
		groups[key] = append(groups[key], c)
		if _, ok := displayName[key]; !ok && c.Author != "" {
			displayName[key] = c.Author
		}
	}

	var multi []vendorGroup
	for key, items := range groups {
		if key != "" && len(items) >= ClosureGistVendorMin {
			multi = append(multi, vendorGroup{Name: displayName[key], Items: items})
		}
	}

	sort.Slice(multi, func(i, j int) bool {
		if len(multi[i].Items) != len(multi[j].Items) {
			return len(multi[i].Items) > len(multi[j].Items)
		}
		return multi[i].Name < multi[j].Name
	})
	for i := range multi {
		sort.Slice(multi[i].Items, func(a, b int) bool { return multi[i].Items[a].Slug < multi[i].Items[b].Slug })
	}
	return multi
}

func renderMarkdown(runStarted time.Time, multi []vendorGroup) string {
	var b strings.Builder
	fmt.Fprintf(&b, "WordPress.org plugins closed on %s\n\n", runStarted.UTC().Format(time.RFC3339))

	for _, v := range multi {
		fmt.Fprintf(&b, "## %s\n\n", v.Name)
		b.WriteString("| Plugin slug | WordPress.org URL | Notes |\n")
		b.WriteString("| --- | --- | --- |\n")
		for _, it := range v.Items {
			fmt.Fprintf(&b, "| `%s` | https://wordpress.org/plugins/%s/ | |\n", it.Slug, it.Slug)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func postGist(ctx context.Context, token string, runStarted time.Time, content string) (string, error) {
	filename := fmt.Sprintf("wp-plugins-closed-%s.md", runStarted.UTC().Format("2006-01-02"))
	payload, err := json.Marshal(map[string]any{
		"description": fmt.Sprintf("WordPress.org plugins closed on %s", runStarted.UTC().Format(time.RFC3339)),
		"public":      false,
		"files": map[string]any{
			filename: map[string]string{"content": content},
		},
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.github.com/gists", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "wp-packages/1.0 (+https://wp-packages.org)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("posting gist: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("gist API %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result struct {
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.HTMLURL, nil
}
