package packages

import (
	"fmt"
	"html"
	"regexp"
	"time"
)

var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

// PackageFromAPIData maps a WordPress.org API response to a Package struct.
func PackageFromAPIData(data map[string]any, pkgType string) *Package {
	pkg := &Package{
		Type:     pkgType,
		Name:     getString(data, "slug"),
		IsActive: true,
	}

	if v := getString(data, "name"); v != "" {
		s := html.UnescapeString(v)
		pkg.DisplayName = &s
	}
	if v := getString(data, "short_description"); v != "" {
		s := html.UnescapeString(v)
		pkg.Description = &s
	} else if sections, ok := data["sections"].(map[string]any); ok {
		if v := getString(sections, "description"); v != "" {
			s := html.UnescapeString(htmlTagRe.ReplaceAllString(v, ""))
			pkg.Description = &s
		}
	}
	if v := getString(data, "author"); v != "" {
		s := html.UnescapeString(htmlTagRe.ReplaceAllString(v, ""))
		pkg.Author = &s
	}
	if v := getString(data, "homepage"); v != "" {
		pkg.Homepage = &v
	}
	if v := getString(data, "version"); v != "" {
		pkg.CurrentVersion = &v
		pkg.WporgVersion = &v
	}

	pkg.Downloads = getInt64(data, "downloaded")
	pkg.ActiveInstalls = getInt64(data, "active_installs")
	pkg.NumRatings = int(getInt64(data, "num_ratings"))

	if v := getFloat64(data, "rating"); v > 0 {
		pkg.Rating = &v
	}

	if v := getString(data, "last_updated"); v != "" {
		if t, err := parseWordPressDate(v); err == nil {
			pkg.LastCommitted = &t
		}
	}

	// Extract versions map (version string -> download URL)
	if versionsRaw, ok := data["versions"]; ok {
		if vm, ok := versionsRaw.(map[string]any); ok {
			versions := make(map[string]string, len(vm))
			for ver, dl := range vm {
				if dlStr, ok := dl.(string); ok {
					versions[ver] = dlStr
				}
			}
			pkg.RawVersions = versions
		}
	}

	return pkg
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getInt64(m map[string]any, key string) int64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return int64(n)
		case int64:
			return n
		}
	}
	return 0
}

func getFloat64(m map[string]any, key string) float64 {
	if v, ok := m[key]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return 0
}

// parseWordPressDate tries common date formats from the WordPress.org API.
func parseWordPressDate(s string) (time.Time, error) {
	formats := []string{
		"2006-01-02 3:04pm MST",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		time.RFC3339,
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable date: %s", s)
}
