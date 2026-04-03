package composer

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/roots/wp-packages/internal/version"
)

// HashVersions computes a content hash over the normalized versions_json and
// trunk_revision. trunk_revision is included because it affects the serialized
// dev-trunk output (source.reference includes trunk@<rev>) even though it's
// not part of versions_json.
//
// versionsJSON is already deterministic — json.Marshal sorts map keys, and
// NormalizeAndStoreVersions always produces the same output for the same input.
// So we hash the string directly rather than round-tripping through parse/sort.
func HashVersions(versionsJSON string, trunkRevision *int64) string {
	h := sha256.New()
	h.Write([]byte(versionsJSON))
	if trunkRevision != nil {
		h.Write([]byte(strconv.FormatInt(*trunkRevision, 10)))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// SerializePackage produces the Composer p2 JSON for a single package file.
//
// The name parameter determines which versions to include:
//   - "akismet"     → tagged versions (all non-dev-* versions)
//   - "akismet~dev" → dev versions only (dev-trunk)
//
// Plugins with zero tagged versions get dev-trunk in the main (non-~dev) file.
// Themes never produce dev versions.
//
// Returns nil with no error when there are no versions to serialize (e.g.
// theme ~dev request, or theme with no tagged versions).
func SerializePackage(pkgType, name string, versionsJSON string, meta PackageMeta) ([]byte, error) {
	isDev := strings.HasSuffix(name, "~dev")
	slug := strings.TrimSuffix(name, "~dev")

	// Themes never produce dev files
	if isDev && pkgType == "theme" {
		return nil, nil
	}

	var versions map[string]string
	if err := json.Unmarshal([]byte(versionsJSON), &versions); err != nil {
		return nil, fmt.Errorf("parsing versions_json for %s/%s: %w", pkgType, slug, err)
	}
	versions = version.NormalizeVersions(versions)

	composerName := ComposerName(pkgType, slug)

	var entries map[string]VersionEntry
	if isDev {
		entries = map[string]VersionEntry{
			"dev-trunk": ComposerVersion(pkgType, slug, "dev-trunk", "", meta),
		}
	} else {
		entries = make(map[string]VersionEntry)
		for ver, dlURL := range versions {
			if !strings.HasPrefix(ver, "dev-") {
				entries[ver] = ComposerVersion(pkgType, slug, ver, dlURL, meta)
			}
		}
		// Trunk-only plugins: put dev-trunk in the main file
		if len(entries) == 0 && pkgType == "plugin" {
			entries["dev-trunk"] = ComposerVersion(pkgType, slug, "dev-trunk", "", meta)
		}
	}

	if len(entries) == 0 {
		return nil, nil
	}

	payload := map[string]any{
		"packages": map[string]any{
			composerName: entries,
		},
	}
	return json.Marshal(payload)
}
