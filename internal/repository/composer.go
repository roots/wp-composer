package repository

import (
	"github.com/roots/wp-packages/internal/composer"
)

// Re-export types and functions from the composer package for backwards compatibility
// with existing callers (builder.go, integration tests, etc.).
// These will be removed when builder.go is deleted in Phase 3.

type PackageMeta = composer.PackageMeta

var (
	ComposerVersion        = composer.ComposerVersion
	ComposerName           = composer.ComposerName
	DownloadURL            = composer.DownloadURL
	VendorFromComposerName = composer.VendorFromComposerName
	SlugFromComposerName   = composer.SlugFromComposerName
)
