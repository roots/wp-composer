package version

import (
	"cmp"
	"regexp"
	"strconv"
	"strings"
)

// validVersion matches WordPress version strings: digits separated by dots (1-4 parts),
// optionally followed by a Composer-compatible pre-release suffix.
// alpha/beta/a/b/rc/p/patch allow an optional (dotted or bare) numeric qualifier: -beta1, -RC.2
// dev/stable only allow bare or dot-separated numeric qualifier: -dev, -dev.1 (not -dev1)
var validVersion = regexp.MustCompile(`(?i)^\d+(\.\d+){0,3}(-((alpha|beta|a|b|rc|p|patch)(\.?\d+)?|(dev|stable)(\.\d+)?))?$`)

// Normalize converts a WordPress version string to a Composer-compatible form.
// Returns empty string for invalid versions.
func Normalize(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	// Strip leading v/V to match Composer's VersionParser behavior.
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	if strings.EqualFold(v, "trunk") || v == "dev-trunk" {
		return "dev-trunk"
	}
	if !IsValid(v) {
		return ""
	}
	return v
}

// IsValid checks whether a version string is a valid WordPress version.
func IsValid(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	if strings.EqualFold(v, "trunk") || v == "dev-trunk" {
		return true
	}
	return validVersion.MatchString(v)
}

// NormalizeVersions filters and normalizes a version map (version -> download URL),
// returning only entries with valid versions.
func NormalizeVersions(versions map[string]string) map[string]string {
	result := make(map[string]string, len(versions))
	for v, url := range versions {
		normalized := Normalize(v)
		if normalized != "" {
			result[normalized] = url
		}
	}
	return result
}

// Compare compares two version strings numerically by segment.
// Returns -1, 0, or 1.
func Compare(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}
	for i := range maxLen {
		var av, bv int
		if i < len(aParts) {
			av, _ = strconv.Atoi(aParts[i])
		}
		if i < len(bParts) {
			bv, _ = strconv.Atoi(bParts[i])
		}
		if c := cmp.Compare(av, bv); c != 0 {
			return c
		}
	}
	return 0
}

// Latest returns the highest version from a map of version -> download URL,
// excluding dev-trunk. Returns empty string if no versions are present.
func Latest(versions map[string]string) string {
	var latest string
	for v := range versions {
		if v == "dev-trunk" {
			continue
		}
		if latest == "" || Compare(v, latest) > 0 {
			latest = v
		}
	}
	return latest
}
