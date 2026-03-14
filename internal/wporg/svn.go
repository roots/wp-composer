package wporg

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

type SVNEntry struct {
	Slug          string
	LastCommitted *time.Time
}

// ParseSVNListing streams an SVN XML listing from the given URL and calls fn
// for each directory entry. This handles 100k+ entries with constant memory.
func (c *Client) ParseSVNListing(ctx context.Context, baseURL string, fn func(SVNEntry) error) error {
	client := &http.Client{Timeout: 600 * time.Second}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
	if err != nil {
		return fmt.Errorf("creating SVN request: %w", err)
	}
	req.Header.Set("Accept", "application/xml")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetching SVN listing: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SVN listing returned status %d", resp.StatusCode)
	}

	return parseSVNXML(ctx, resp.Body, fn, c.logger)
}

func parseSVNXML(ctx context.Context, r io.Reader, fn func(SVNEntry) error, logger *slog.Logger) error {
	decoder := xml.NewDecoder(r)

	var count int
	var inEntry bool
	var entry SVNEntry

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("parsing SVN XML: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "entry":
				inEntry = false
				entry = SVNEntry{}
				for _, attr := range t.Attr {
					if attr.Name.Local == "kind" && attr.Value == "dir" {
						inEntry = true
					}
				}
			case "name":
				if inEntry {
					var name string
					if err := decoder.DecodeElement(&name, &t); err == nil {
						entry.Slug = name
					}
				}
			case "date":
				if inEntry {
					var dateStr string
					if err := decoder.DecodeElement(&dateStr, &t); err == nil {
						if parsed, err := time.Parse(time.RFC3339Nano, dateStr); err == nil {
							parsed = parsed.UTC()
							entry.LastCommitted = &parsed
						}
					}
				}
			}
		case xml.EndElement:
			if t.Name.Local == "entry" && inEntry && entry.Slug != "" {
				if err := fn(entry); err != nil {
					return err
				}
				count++
				if count%5000 == 0 {
					logger.Info("SVN discovery progress", "entries", count)
				}
			}
		}
	}

	logger.Info("SVN discovery complete", "total_entries", count)
	return nil
}
