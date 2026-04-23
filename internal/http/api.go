package http

import "strings"

// parsePkgType normalizes the {type} path segment used by the public API.
// It accepts both the composer-style "wp-plugin"/"wp-theme" and the bare
// "plugin"/"theme" forms, returning the bare form. ok is false for anything
// else, which callers should treat as 404.
func parsePkgType(raw string) (string, bool) {
	t := strings.TrimPrefix(raw, "wp-")
	if t == "plugin" || t == "theme" {
		return t, true
	}
	return "", false
}
