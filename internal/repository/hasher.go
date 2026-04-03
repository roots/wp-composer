package repository

import (
	"github.com/roots/wp-packages/internal/composer"
)

// Re-export hashing functions from the composer package for backwards compatibility
// with builder.go. These will be removed when builder.go is deleted in Phase 3.

var (
	HashJSON = composer.HashJSON
)
