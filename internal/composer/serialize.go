package composer

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/roots/wp-packages/internal/version"
)

// FileOutput holds serialized Composer JSON and its R2 object key.
type FileOutput struct {
	Data []byte
	Key  string // R2 object key, e.g. "p2/wp-plugin/akismet.json"
}

// PackageFiles holds the output files for a single package.
type PackageFiles struct {
	Tagged FileOutput // p2/wp-plugin/akismet.json (always present for active packages)
	Dev    FileOutput // p2/wp-plugin/akismet~dev.json (empty if no dev versions)
}

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

// SerializePackage splits versions_json into tagged and dev files, computes a
// content hash over the full deterministic versions_json, and returns the hash
// plus the serialized Composer JSON for each output file.
//
// The hash is computed over the full normalized versions_json (all versions),
// not the output files. This means any version change triggers re-upload of
// both files, which is simpler than tracking separate hashes.
//
// Plugins produce both tagged and dev files. Themes produce only tagged.
// Plugins with zero tagged versions get dev-trunk in the main file.
func SerializePackage(pkgType, name string, versionsJSON string, meta PackageMeta) (hash string, files PackageFiles, err error) {
	// Parse and re-normalize versions
	var versions map[string]string
	if err := json.Unmarshal([]byte(versionsJSON), &versions); err != nil {
		return "", PackageFiles{}, fmt.Errorf("parsing versions_json for %s/%s: %w", pkgType, name, err)
	}
	versions = version.NormalizeVersions(versions)

	// Compute content hash over versions + trunk_revision.
	// We hash the re-normalized JSON (not the raw input) so the hash matches
	// what HashVersions would produce on the same DB row.
	normalized, err := json.Marshal(versions)
	if err != nil {
		return "", PackageFiles{}, fmt.Errorf("marshaling normalized versions for %s/%s: %w", pkgType, name, err)
	}
	hash = HashVersions(string(normalized), meta.TrunkRevision)

	composerName := ComposerName(pkgType, name)

	// Split versions into tagged and dev
	taggedVersions := make(map[string]any)
	for ver, dlURL := range versions {
		if !strings.HasPrefix(ver, "dev-") {
			taggedVersions[ver] = ComposerVersion(pkgType, name, ver, dlURL, meta)
		}
	}

	var devVersions map[string]any
	if pkgType == "plugin" {
		devVersions = map[string]any{
			"dev-trunk": ComposerVersion(pkgType, name, "dev-trunk", "", meta),
		}
	}

	// Main file: tagged versions, or dev-trunk for trunk-only plugins
	mainVersions := taggedVersions
	if len(mainVersions) == 0 && devVersions != nil {
		mainVersions = devVersions
	}
	if len(mainVersions) == 0 {
		// Theme with no tagged versions — nothing to serialize
		return hash, PackageFiles{}, nil
	}

	taggedPayload := map[string]any{
		"packages": map[string]any{
			composerName: mainVersions,
		},
	}
	taggedData, err := DeterministicJSON(taggedPayload)
	if err != nil {
		return "", PackageFiles{}, fmt.Errorf("serializing tagged %s: %w", composerName, err)
	}

	files.Tagged = FileOutput{
		Data: taggedData,
		Key:  fmt.Sprintf("p2/%s.json", composerName),
	}

	// Dev file: plugins only
	if devVersions != nil {
		devPayload := map[string]any{
			"packages": map[string]any{
				composerName: devVersions,
			},
		}
		devData, err := DeterministicJSON(devPayload)
		if err != nil {
			return "", PackageFiles{}, fmt.Errorf("serializing dev %s: %w", composerName, err)
		}
		files.Dev = FileOutput{
			Data: devData,
			Key:  fmt.Sprintf("p2/%s~dev.json", composerName),
		}
	}

	return hash, files, nil
}
