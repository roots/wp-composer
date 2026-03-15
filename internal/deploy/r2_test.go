package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// fakeR2 implements r2API for testing cleanup logic.
type fakeR2 struct {
	objects     map[string][]byte // key -> content
	getErr      error             // injected error for GetObject
	deletedKeys []string
}

func (f *fakeR2) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	key := aws.ToString(input.Key)
	data, ok := f.objects[key]
	if !ok {
		return nil, &s3types.NoSuchKey{}
	}
	return &s3.GetObjectOutput{
		Body: io.NopCloser(bytes.NewReader(data)),
	}, nil
}

func (f *fakeR2) ListObjectsV2(_ context.Context, input *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	var contents []s3types.Object
	for key := range f.objects {
		contents = append(contents, s3types.Object{Key: aws.String(key)})
	}
	return &s3.ListObjectsV2Output{
		Contents:    contents,
		IsTruncated: aws.Bool(false),
	}, nil
}

func (f *fakeR2) DeleteObjects(_ context.Context, input *s3.DeleteObjectsInput, _ ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error) {
	for _, obj := range input.Delete.Objects {
		key := aws.ToString(obj.Key)
		f.deletedKeys = append(f.deletedKeys, key)
		delete(f.objects, key)
	}
	return &s3.DeleteObjectsOutput{}, nil
}

// versionedRootJSON builds a root packages.json pointing at the given release.
// metadata-url is prefixed (per-release p2/); providers-url is unprefixed (shared p/).
func versionedRootJSON(buildID string) []byte {
	data, _ := json.Marshal(map[string]any{
		"metadata-url":  "/releases/" + buildID + "/p2/%package%.json",
		"providers-url": "/p/%package%$%hash%.json",
	})
	return data
}

// flatRootJSON builds a pre-versioned root packages.json (flat-path layout).
func flatRootJSON() []byte {
	data, _ := json.Marshal(map[string]any{
		"metadata-url":  "/p2/%package%.json",
		"providers-url": "/p/%package%$%hash%.json",
	})
	return data
}

// oldLayoutRootJSON builds a root packages.json using the old release-prefixed
// layout where both metadata-url and providers-url point into releases/.
func oldLayoutRootJSON(buildID string) []byte {
	data, _ := json.Marshal(map[string]any{
		"metadata-url":  "/releases/" + buildID + "/p2/%package%.json",
		"providers-url": "/releases/" + buildID + "/p/%package%$%hash%.json",
	})
	return data
}

func TestUploadPriority(t *testing.T) {
	tests := []struct {
		path string
		want int
	}{
		{"p/wp-plugin/akismet$abc.json", 0},
		{"p2/wp-plugin/akismet.json", 0},
		{"p/providers-this-week$abc.json", 0},
		{"manifest.json", 2},
		{"packages.json", 3},
		// Release-prefixed paths should also sort correctly.
		{"releases/20260314-150405/p2/wp-plugin/akismet.json", 0},
		{"releases/20260314-150405/manifest.json", 2},
		{"releases/20260314-150405/packages.json", 3},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := uploadPriority(tt.path)
			if got != tt.want {
				t.Errorf("uploadPriority(%q) = %d, want %d", tt.path, got, tt.want)
			}
		})
	}
}

func TestUploadOrderPackagesJsonLast(t *testing.T) {
	buildDir := t.TempDir()

	_ = os.MkdirAll(filepath.Join(buildDir, "p", "wp-plugin"), 0755)
	_ = os.MkdirAll(filepath.Join(buildDir, "p2", "wp-plugin"), 0755)

	files := map[string]string{
		"packages.json":                `{"packages":{}}`,
		"manifest.json":                `{}`,
		"p/providers-2026-03$abc.json": `{}`,
		"p/wp-plugin/akismet$def.json": `{}`,
		"p2/wp-plugin/akismet.json":    `{}`,
	}
	for name, content := range files {
		path := filepath.Join(buildDir, filepath.FromSlash(name))
		_ = os.WriteFile(path, []byte(content), 0644)
	}

	var collected []buildFile
	err := WalkBuildFiles(buildDir, func(relPath string, data []byte) error {
		collected = append(collected, buildFile{relPath: relPath, data: data})
		return nil
	})
	if err != nil {
		t.Fatalf("WalkBuildFiles: %v", err)
	}

	sortBuildFiles(collected)

	n := len(collected)
	if n < 2 {
		t.Fatalf("expected at least 2 files, got %d", n)
	}
	if collected[n-1].relPath != "packages.json" {
		t.Errorf("last file = %q, want packages.json", collected[n-1].relPath)
	}
	if collected[n-2].relPath != "manifest.json" {
		t.Errorf("second-to-last file = %q, want manifest.json", collected[n-2].relPath)
	}

	for i := 0; i < n-2; i++ {
		if collected[i].relPath == "packages.json" || collected[i].relPath == "manifest.json" {
			t.Errorf("index file %q found at position %d, expected after all data files", collected[i].relPath, i)
		}
	}
}

func TestValidateBuildRejectsInvalid(t *testing.T) {
	// Build with no packages.json should fail validation
	buildDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(buildDir, "manifest.json"), []byte(`{}`), 0644)

	err := ValidateBuild(buildDir)
	if err == nil {
		t.Fatal("expected ValidateBuild to reject build missing packages.json")
	}

	// Build with no manifest.json should fail
	buildDir2 := t.TempDir()
	_ = os.WriteFile(filepath.Join(buildDir2, "packages.json"), []byte(`{}`), 0644)

	err = ValidateBuild(buildDir2)
	if err == nil {
		t.Fatal("expected ValidateBuild to reject build missing manifest.json")
	}

	// Valid build should pass
	buildDir3 := t.TempDir()
	_ = os.WriteFile(filepath.Join(buildDir3, "packages.json"), []byte(`{}`), 0644)
	_ = os.WriteFile(filepath.Join(buildDir3, "manifest.json"), []byte(`{}`), 0644)

	err = ValidateBuild(buildDir3)
	if err != nil {
		t.Fatalf("expected ValidateBuild to accept valid build, got: %v", err)
	}
}

func TestRewritePackagesJSON(t *testing.T) {
	input := map[string]any{
		"metadata-url":  "/p2/%package%.json",
		"providers-url": "/p/%package%$%hash%.json",
		"provider-includes": map[string]any{
			"p/providers-this-week$abc123.json": map[string]any{
				"sha256": "deadbeef",
			},
			"p/providers-last-week$def456.json": map[string]any{
				"sha256": "cafebabe",
			},
		},
		"notify-batch": "https://app.example.com/notify",
	}

	inputJSON, _ := json.Marshal(input)
	prefix := "releases/20260314-150405/"

	result, err := RewritePackagesJSON(inputJSON, prefix)
	if err != nil {
		t.Fatalf("RewritePackagesJSON: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	// Check metadata-url rewritten (per-release).
	wantMeta := "/releases/20260314-150405/p2/%package%.json"
	if got["metadata-url"] != wantMeta {
		t.Errorf("metadata-url = %q, want %q", got["metadata-url"], wantMeta)
	}

	// Check providers-url NOT rewritten (shared content-addressed store).
	wantProv := "/p/%package%$%hash%.json"
	if got["providers-url"] != wantProv {
		t.Errorf("providers-url = %q, want %q", got["providers-url"], wantProv)
	}

	// Check provider-includes keys NOT rewritten (shared content-addressed store).
	pi, ok := got["provider-includes"].(map[string]any)
	if !ok {
		t.Fatal("provider-includes not a map")
	}
	for key := range pi {
		if strings.HasPrefix(key, "releases/") {
			t.Errorf("provider-includes key %q should not be prefixed", key)
		}
	}
	if len(pi) != 2 {
		t.Errorf("provider-includes has %d keys, want 2", len(pi))
	}

	// Check notify-batch NOT rewritten.
	if got["notify-batch"] != "https://app.example.com/notify" {
		t.Errorf("notify-batch was modified: %v", got["notify-batch"])
	}
}

func TestCacheControlForReleasePaths(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		// Root packages.json — mutable pointer.
		{"packages.json", "public, max-age=300"},
		// Everything under releases/ is immutable.
		{"releases/20260314-150405/packages.json", "public, max-age=31536000, immutable"},
		{"releases/20260314-150405/manifest.json", "public, max-age=31536000, immutable"},
		{"releases/20260314-150405/p2/wp-plugin/akismet.json", "public, max-age=31536000, immutable"},
		{"releases/20260314-150405/p/wp-plugin/akismet$abc.json", "public, max-age=31536000, immutable"},
		// Legacy flat paths still work.
		{"manifest.json", "public, max-age=300"},
		{"p/wp-plugin/akismet$abc.json", "public, max-age=31536000, immutable"},
		{"p2/wp-plugin/akismet.json", "public, max-age=300"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := CacheControlForPath(tt.path)
			if got != tt.want {
				t.Errorf("CacheControlForPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// --- Safety-critical cleanup tests using fakeR2 ---

func TestFetchLiveReleaseTransientError(t *testing.T) {
	// A non-NoSuchKey error from GetObject must propagate as an error,
	// not be silently treated as "no live release".
	fake := &fakeR2{
		objects: map[string][]byte{},
		getErr:  fmt.Errorf("connection reset by peer"),
	}

	_, err := fetchLiveRelease(context.Background(), fake, "test-bucket")
	if err == nil {
		t.Fatal("expected fetchLiveRelease to return error on transient GetObject failure")
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("error should contain original cause, got: %v", err)
	}
}

func TestCleanupRefusesWhenLiveReleaseUnknown(t *testing.T) {
	// Bucket has release prefixes but root packages.json uses flat-path layout
	// (no releases/ in metadata-url) — cleanup must refuse to delete releases
	// because it can't identify which one is live.
	fake := &fakeR2{
		objects: map[string][]byte{
			"packages.json":                                      flatRootJSON(),
			"releases/20260314-100000/packages.json":             []byte(`{}`),
			"releases/20260314-100000/p2/wp-plugin/akismet.json": []byte(`{}`),
			"releases/20260314-110000/packages.json":             []byte(`{}`),
		},
	}

	_, err := cleanupR2(context.Background(), fake, "test-bucket", 0, 1, slog.Default())
	if err == nil {
		t.Fatal("expected cleanupR2 to refuse when live release is unknown but release prefixes exist")
	}
	if !strings.Contains(err.Error(), "refusing to clean") {
		t.Errorf("error should mention refusing to clean, got: %v", err)
	}

	// Nothing should have been deleted.
	if len(fake.deletedKeys) > 0 {
		t.Errorf("expected no deletions, got %d: %v", len(fake.deletedKeys), fake.deletedKeys)
	}
}

func TestCleanupSkipsLegacyFilesWhenRootNotVersioned(t *testing.T) {
	// Bucket has legacy flat files and no release prefixes.
	// Root packages.json uses flat-path layout — legacy files must NOT be deleted.
	fake := &fakeR2{
		objects: map[string][]byte{
			"packages.json":                flatRootJSON(),
			"p2/wp-plugin/akismet.json":    []byte(`{}`),
			"p/wp-plugin/akismet$abc.json": []byte(`{}`),
			"manifest.json":                []byte(`{}`),
		},
	}

	deleted, err := cleanupR2(context.Background(), fake, "test-bucket", 0, 1, slog.Default())
	if err != nil {
		t.Fatalf("cleanupR2: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected 0 deletions when root is not versioned, got %d", deleted)
	}
	if len(fake.deletedKeys) > 0 {
		t.Errorf("legacy files should not be deleted: %v", fake.deletedKeys)
	}
}

func TestCleanupDeletesLegacyFilesWhenRootIsVersioned(t *testing.T) {
	// Root packages.json points at a release prefix — legacy flat files should be cleaned up,
	// but shared content-addressed p/ files should NOT be deleted (they're part of the new layout).
	liveID := "20260314-150000"
	fake := &fakeR2{
		objects: map[string][]byte{
			"packages.json":                               versionedRootJSON(liveID),
			"releases/" + liveID + "/packages.json":       []byte(`{}`),
			"releases/" + liveID + "/p2/wp-plugin/a.json": []byte(`{}`),
			// Legacy flat files — should be deleted.
			"p2/wp-plugin/akismet.json": []byte(`{}`),
			"manifest.json":            []byte(`{}`),
			// Shared content-addressed p/ file — should NOT be deleted.
			"p/wp-plugin/akismet$abc.json": []byte(`{}`),
		},
	}

	deleted, err := cleanupR2(context.Background(), fake, "test-bucket", 0, 1, slog.Default())
	if err != nil {
		t.Fatalf("cleanupR2: %v", err)
	}
	if deleted != 2 {
		t.Errorf("expected 2 legacy deletions (not shared p/ files), got %d", deleted)
	}

	// Live release files must NOT be deleted.
	for _, key := range fake.deletedKeys {
		if strings.HasPrefix(key, "releases/"+liveID+"/") {
			t.Errorf("live release file deleted: %s", key)
		}
		if strings.HasPrefix(key, "p/") && strings.Contains(key, "$") {
			t.Errorf("shared content-addressed file deleted: %s", key)
		}
	}
}

func TestFetchLiveReleaseDetectsSharedPrefix(t *testing.T) {
	tests := []struct {
		name            string
		root            []byte
		wantShared      bool
		wantIsVersioned bool
	}{
		{
			name:            "new layout (shared p/)",
			root:            versionedRootJSON("20260314-150000"),
			wantShared:      true,
			wantIsVersioned: true,
		},
		{
			name:            "old layout (release-prefixed p/)",
			root:            oldLayoutRootJSON("20260314-150000"),
			wantShared:      false,
			wantIsVersioned: true,
		},
		{
			name:            "flat layout (pre-versioned)",
			root:            flatRootJSON(),
			wantShared:      true, // providers-url = /p/... (not releases/), so shared
			wantIsVersioned: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeR2{objects: map[string][]byte{"packages.json": tt.root}}
			result, err := fetchLiveRelease(context.Background(), fake, "test-bucket")
			if err != nil {
				t.Fatalf("fetchLiveRelease: %v", err)
			}
			if result.hasSharedPrefix != tt.wantShared {
				t.Errorf("hasSharedPrefix = %v, want %v", result.hasSharedPrefix, tt.wantShared)
			}
			if result.isVersioned != tt.wantIsVersioned {
				t.Errorf("isVersioned = %v, want %v", result.isVersioned, tt.wantIsVersioned)
			}
		})
	}
}

func TestCollectSharedFilesSkipsNonShared(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "p", "wp-plugin"), 0755)
	_ = os.MkdirAll(filepath.Join(dir, "p2", "wp-plugin"), 0755)

	_ = os.WriteFile(filepath.Join(dir, "p", "wp-plugin", "akismet$abc.json"), []byte(`{}`), 0644)
	_ = os.WriteFile(filepath.Join(dir, "p2", "wp-plugin", "akismet.json"), []byte(`{}`), 0644)
	_ = os.WriteFile(filepath.Join(dir, "packages.json"), []byte(`{}`), 0644)

	shared := collectSharedFiles(dir)
	if len(shared) != 1 {
		t.Fatalf("expected 1 shared file, got %d: %v", len(shared), shared)
	}
	if !shared["p/wp-plugin/akismet$abc.json"] {
		t.Errorf("expected p/wp-plugin/akismet$abc.json in shared set")
	}
}

func TestCollectSharedFilesEmptyDir(t *testing.T) {
	shared := collectSharedFiles("")
	if shared != nil {
		t.Errorf("expected nil for empty dir, got %v", shared)
	}
}
