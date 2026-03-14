package version

import (
	"regexp"
	"strings"
)

// validVersion matches WordPress version strings: digits separated by dots (1-4 parts),
// optionally followed by a pre-release suffix like -beta1, -RC2, -alpha.
var validVersion = regexp.MustCompile(`^\d+(\.\d+){0,3}(-[a-zA-Z0-9._]+)?$`)

// Normalize converts a WordPress version string to a Composer-compatible form.
// Returns empty string for invalid versions.
func Normalize(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if strings.EqualFold(v, "trunk") {
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
	if strings.EqualFold(v, "trunk") {
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
