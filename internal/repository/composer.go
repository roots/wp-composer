package repository

import (
	"fmt"
	"hash/crc32"
	"strings"
)

// ComposerVersion builds a single Composer version entry for a package.
func ComposerVersion(pkgType, slug, ver, downloadURL string, meta PackageMeta) map[string]any {
	composerName := ComposerName(pkgType, slug)
	composerType := "wordpress-plugin"
	if pkgType == "theme" {
		composerType = "wordpress-theme"
	}

	svnBase := fmt.Sprintf("https://plugins.svn.wordpress.org/%s", slug)
	supportIssues := fmt.Sprintf("https://wordpress.org/support/plugin/%s", slug)
	supportChangelog := fmt.Sprintf("https://wordpress.org/plugins/%s/#developers", slug)
	if pkgType == "theme" {
		svnBase = fmt.Sprintf("https://themes.svn.wordpress.org/%s", slug)
		supportIssues = fmt.Sprintf("https://wordpress.org/support/theme/%s", slug)
		supportChangelog = fmt.Sprintf("https://wordpress.org/themes/%s/#developers", slug)
	}

	ref := fmt.Sprintf("tags/%s", ver)
	if pkgType == "theme" {
		ref = ver
	}
	if ver == "dev-trunk" {
		ref = "trunk"
	}

	entry := map[string]any{
		"name":    composerName,
		"version": ver,
		"type":    composerType,
		"source": map[string]any{
			"type":      "svn",
			"url":       svnBase + "/",
			"reference": ref,
		},
		"require": map[string]any{
			"composer/installers": "~1.0|~2.0",
		},
		"support": map[string]any{
			"source":    svnBase,
			"issues":    supportIssues,
			"changelog": supportChangelog,
		},
		"uid": crc32.ChecksumIEEE([]byte(fmt.Sprintf("%s/%s", composerName, ver))),
	}

	if ver == "dev-trunk" {
		// Trunk zip is unversioned (e.g. /plugin/akismet.zip)
		trunkURL := fmt.Sprintf("https://downloads.wordpress.org/plugin/%s.zip", slug)
		if pkgType == "theme" {
			trunkURL = fmt.Sprintf("https://downloads.wordpress.org/theme/%s.zip", slug)
		}
		entry["dist"] = map[string]any{
			"type": "zip",
			"url":  trunkURL,
		}
	} else if downloadURL != "" {
		entry["dist"] = map[string]any{
			"type": "zip",
			"url":  downloadURL,
		}
	}

	if meta.Description != "" {
		entry["description"] = meta.Description
	}
	if meta.Homepage != "" {
		entry["homepage"] = meta.Homepage
	}
	if meta.Author != "" {
		entry["authors"] = []map[string]any{{"name": meta.Author}}
	}
	if meta.RequiresPHP != "" {
		req := entry["require"].(map[string]any)
		req["php"] = ">=" + meta.RequiresPHP
	}
	if meta.LastUpdated != "" {
		entry["time"] = meta.LastUpdated
	}

	return entry
}

// ComposerName returns the Composer package name for a WordPress package.
func ComposerName(pkgType, slug string) string {
	if pkgType == "theme" {
		return "wp-theme/" + slug
	}
	return "wp-plugin/" + slug
}

// DownloadURL returns the WordPress.org download URL for a specific version.
func DownloadURL(pkgType, slug, version string) string {
	if pkgType == "theme" {
		return fmt.Sprintf("https://downloads.wordpress.org/theme/%s.%s.zip", slug, version)
	}
	return fmt.Sprintf("https://downloads.wordpress.org/plugin/%s.%s.zip", slug, version)
}

// PackageMeta holds optional metadata for Composer version entries.
type PackageMeta struct {
	Description string
	Homepage    string
	Author      string
	RequiresPHP string
	LastUpdated string
}

// VendorFromComposerName extracts the path portion for filesystem layout.
// "wp-plugin/akismet" → "wp-plugin/akismet"
func VendorFromComposerName(name string) string {
	return name
}

// SlugFromComposerName extracts just the slug: "wp-plugin/akismet" → "akismet"
func SlugFromComposerName(name string) string {
	parts := strings.SplitN(name, "/", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return name
}
