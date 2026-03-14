package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/roots/wp-composer/internal/config"
)

const (
	r2MaxRetries   = 3
	r2RetryBaseMs  = 1000
	r2IndexFile    = "packages.json"
	r2ManifestFile = "manifest.json"
)

// r2API is the subset of the S3 client used by cleanup and live-release detection.
// The real *s3.Client satisfies this; tests provide a fake.
type r2API interface {
	GetObject(ctx context.Context, input *s3.GetObjectInput, opts ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	ListObjectsV2(ctx context.Context, input *s3.ListObjectsV2Input, opts ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	DeleteObjects(ctx context.Context, input *s3.DeleteObjectsInput, opts ...func(*s3.Options)) (*s3.DeleteObjectsOutput, error)
}

// buildFile holds a file collected from the build directory for ordered upload.
type buildFile struct {
	relPath string
	data    []byte
}

// SyncToR2 uploads all files in a build directory to R2 under a versioned
// release prefix (releases/<buildID>/). After all release files are uploaded,
// it rewrites the root packages.json to point at the new prefix — the atomic
// pointer swap that makes the new release live.
func SyncToR2(ctx context.Context, cfg config.R2Config, buildDir, buildID string, logger *slog.Logger) error {
	client := newS3Client(cfg)
	releasePrefix := "releases/" + buildID + "/"

	// Collect all files, then sort: index files last.
	var files []buildFile
	err := WalkBuildFiles(buildDir, func(relPath string, data []byte) error {
		files = append(files, buildFile{relPath: relPath, data: data})
		return nil
	})
	if err != nil {
		return fmt.Errorf("collecting build files: %w", err)
	}

	sortBuildFiles(files)

	// Upload all files under the release prefix.
	var uploaded int
	var packagesData []byte
	for _, f := range files {
		key := releasePrefix + f.relPath
		if err := putObjectWithRetry(ctx, client, cfg.Bucket, key, f.data, logger); err != nil {
			return fmt.Errorf("R2 sync: %w", err)
		}
		if f.relPath == r2IndexFile {
			packagesData = f.data
		}
		uploaded++
		if uploaded%100 == 0 {
			logger.Info("R2 upload progress", "files", uploaded)
		}
	}

	// Rewrite and upload root packages.json — the atomic switch.
	if packagesData == nil {
		return fmt.Errorf("R2 sync: packages.json not found in build")
	}
	rewritten, err := RewritePackagesJSON(packagesData, releasePrefix)
	if err != nil {
		return fmt.Errorf("rewriting packages.json: %w", err)
	}
	if err := putObjectWithRetry(ctx, client, cfg.Bucket, r2IndexFile, rewritten, logger); err != nil {
		return fmt.Errorf("R2 sync (root packages.json): %w", err)
	}

	logger.Info("R2 sync complete", "files", uploaded, "release", releasePrefix)
	return nil
}

// RewritePackagesJSON prefixes URL templates and provider-includes keys with
// the given release prefix so all paths point into the versioned release dir.
func RewritePackagesJSON(data []byte, releasePrefix string) ([]byte, error) {
	var pkg map[string]any
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("parsing packages.json: %w", err)
	}

	// Prefix metadata-url and providers-url.
	for _, key := range []string{"metadata-url", "providers-url"} {
		if v, ok := pkg[key].(string); ok {
			pkg[key] = "/" + releasePrefix + strings.TrimPrefix(v, "/")
		}
	}

	// Prefix provider-includes keys.
	if pi, ok := pkg["provider-includes"].(map[string]any); ok {
		rewritten := make(map[string]any, len(pi))
		for k, v := range pi {
			rewritten[releasePrefix+k] = v
		}
		pkg["provider-includes"] = rewritten
	}

	return deterministicJSON(pkg)
}

// sortBuildFiles sorts files so index files (packages.json, manifest.json) come last.
func sortBuildFiles(files []buildFile) {
	sort.SliceStable(files, func(i, j int) bool {
		return uploadPriority(files[i].relPath) < uploadPriority(files[j].relPath)
	})
}

// uploadPriority returns a sort key: lower numbers upload first.
// Index files get the highest priority number so they upload last.
func uploadPriority(relPath string) int {
	base := relPath
	if idx := strings.LastIndex(relPath, "/"); idx >= 0 {
		base = relPath[idx+1:]
	}
	switch base {
	case r2ManifestFile:
		return 2
	case r2IndexFile:
		return 3
	default:
		return 0
	}
}

// putObjectWithRetry uploads a single file to R2 with exponential backoff retry.
func putObjectWithRetry(ctx context.Context, client *s3.Client, bucket, key string, data []byte, logger *slog.Logger) error {
	contentType := "application/json"
	cacheControl := CacheControlForPath(key)

	var lastErr error
	for attempt := range r2MaxRetries {
		if attempt > 0 {
			delay := time.Duration(float64(r2RetryBaseMs)*math.Pow(2, float64(attempt-1))) * time.Millisecond
			logger.Warn("retrying R2 upload", "key", key, "attempt", attempt+1, "delay", delay)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		_, lastErr = client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:       aws.String(bucket),
			Key:          aws.String(key),
			Body:         bytes.NewReader(data),
			ContentType:  aws.String(contentType),
			CacheControl: aws.String(cacheControl),
		})
		if lastErr == nil {
			return nil
		}
	}
	return fmt.Errorf("uploading %s after %d attempts: %w", key, r2MaxRetries, lastErr)
}

// CleanupR2 removes old release prefixes from R2, keeping the live release,
// releases within the grace period, and the top N most recent releases.
// It no longer depends on the local filesystem — all state is read from R2.
func CleanupR2(ctx context.Context, cfg config.R2Config, graceHours, retainCount int, logger *slog.Logger) (int, error) {
	return cleanupR2(ctx, newS3Client(cfg), cfg.Bucket, graceHours, retainCount, logger)
}

func cleanupR2(ctx context.Context, client r2API, bucket string, graceHours, retainCount int, logger *slog.Logger) (int, error) {
	if retainCount < 1 {
		retainCount = 3
	}
	if graceHours < 0 {
		graceHours = 24
	}

	// Fetch root packages.json to identify the live release.
	// Hard-fail on transient errors — we must know the live release before deleting anything.
	live, err := fetchLiveRelease(ctx, client, bucket)
	if err != nil {
		return 0, fmt.Errorf("identifying live release: %w", err)
	}

	// If release prefixes exist but we couldn't identify which one is live,
	// refuse to proceed — we might delete the active release.
	// (This is checked after listing, but we capture the result now.)

	// List all objects and group by release prefix.
	releaseObjects := make(map[string][]string) // buildID -> list of keys
	var legacyKeys []string
	var continuationToken *string
	for {
		input := &s3.ListObjectsV2Input{
			Bucket:            aws.String(bucket),
			ContinuationToken: continuationToken,
		}
		resp, err := client.ListObjectsV2(ctx, input)
		if err != nil {
			return 0, fmt.Errorf("listing R2 objects: %w", err)
		}

		for _, obj := range resp.Contents {
			key := aws.ToString(obj.Key)
			if strings.HasPrefix(key, "releases/") {
				parts := strings.SplitN(strings.TrimPrefix(key, "releases/"), "/", 2)
				if len(parts) >= 1 && parts[0] != "" {
					releaseObjects[parts[0]] = append(releaseObjects[parts[0]], key)
				}
			} else if key != r2IndexFile {
				legacyKeys = append(legacyKeys, key)
			}
		}

		if !aws.ToBool(resp.IsTruncated) {
			break
		}
		continuationToken = resp.NextContinuationToken
	}

	if len(releaseObjects) == 0 && len(legacyKeys) == 0 {
		logger.Info("R2 cleanup: nothing to clean")
		return 0, nil
	}

	// Safety: if there are release prefixes on R2 but we couldn't identify the
	// live one, refuse to delete anything — a transient read failure could cause
	// us to delete the active release.
	if len(releaseObjects) > 0 && live.buildID == "" {
		return 0, fmt.Errorf("release prefixes exist on R2 but live release could not be identified — refusing to clean")
	}

	// Build the keep set: live release + within grace period + top N recent.
	keep := make(map[string]bool)
	if live.buildID != "" {
		keep[live.buildID] = true
	}

	graceCutoff := time.Now().Add(-time.Duration(graceHours) * time.Hour)

	// Sort release IDs to find the most recent ones.
	var releaseIDs []string
	for id := range releaseObjects {
		releaseIDs = append(releaseIDs, id)
	}
	sort.Strings(releaseIDs)

	// Keep releases within grace period.
	for _, id := range releaseIDs {
		if t, err := time.Parse("20060102-150405", id); err == nil && t.After(graceCutoff) {
			keep[id] = true
		}
	}

	// Keep top N most recent (beyond those already kept).
	kept := 0
	for i := len(releaseIDs) - 1; i >= 0 && kept < retainCount; i-- {
		if !keep[releaseIDs[i]] {
			keep[releaseIDs[i]] = true
			kept++
		}
	}

	// Collect keys to delete: stale releases + legacy flat files.
	var toDelete []s3types.ObjectIdentifier
	for id, keys := range releaseObjects {
		if keep[id] {
			continue
		}
		for _, key := range keys {
			toDelete = append(toDelete, s3types.ObjectIdentifier{Key: aws.String(key)})
		}
	}

	// Only delete legacy flat files if the root packages.json already points at a
	// versioned release prefix. If the bucket is still using flat-path layout
	// (pre-versioned deploy), these files are still live — don't touch them.
	if live.isVersioned && len(legacyKeys) > 0 {
		logger.Info("R2 cleanup: deleting legacy flat files", "count", len(legacyKeys))
		for _, key := range legacyKeys {
			toDelete = append(toDelete, s3types.ObjectIdentifier{Key: aws.String(key)})
		}
	} else if len(legacyKeys) > 0 {
		logger.Info("R2 cleanup: skipping legacy flat files (root not yet versioned)", "count", len(legacyKeys))
	}

	if len(toDelete) == 0 {
		logger.Info("R2 cleanup: nothing to delete", "retained_releases", len(keep))
		return 0, nil
	}

	// Batch delete (S3 API supports up to 1000 per request).
	var deleted int
	for i := 0; i < len(toDelete); i += 1000 {
		end := i + 1000
		if end > len(toDelete) {
			end = len(toDelete)
		}
		batch := toDelete[i:end]
		_, err := client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(bucket),
			Delete: &s3types.Delete{
				Objects: batch,
				Quiet:   aws.Bool(true),
			},
		})
		if err != nil {
			return deleted, fmt.Errorf("deleting R2 objects: %w", err)
		}
		deleted += len(batch)
		logger.Info("R2 cleanup progress", "deleted_batch", len(batch), "deleted_total", deleted)
	}

	logger.Info("R2 cleanup complete", "deleted", deleted, "retained_releases", len(keep))
	return deleted, nil
}

// liveReleaseResult holds the result of reading the root packages.json from R2.
type liveReleaseResult struct {
	// buildID is the live release build ID, empty if root doesn't point at a release prefix.
	buildID string
	// isVersioned is true when the root packages.json uses releases/ URLs.
	// False means the bucket still uses flat-path layout (pre-versioned deploy).
	isVersioned bool
	// exists is true when a root packages.json was found on R2.
	exists bool
}

// fetchLiveRelease reads the root packages.json from R2 and extracts the
// build ID from the metadata-url prefix. Returns an error on transient failures
// so callers can distinguish "no root exists" from "couldn't read it".
func fetchLiveRelease(ctx context.Context, client r2API, bucket string) (liveReleaseResult, error) {
	resp, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(r2IndexFile),
	})
	if err != nil {
		// Check for NoSuchKey — means bucket has no root packages.json yet.
		var nsk *s3types.NoSuchKey
		if errors.As(err, &nsk) {
			return liveReleaseResult{}, nil
		}
		// Any other error is transient — callers must not assume "no live release".
		return liveReleaseResult{}, fmt.Errorf("fetching root packages.json: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var pkg map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&pkg); err != nil {
		return liveReleaseResult{}, fmt.Errorf("parsing root packages.json: %w", err)
	}

	result := liveReleaseResult{exists: true}

	// Extract build ID from metadata-url like "/releases/20260314-150405/p2/%package%.json"
	if mu, ok := pkg["metadata-url"].(string); ok {
		mu = strings.TrimPrefix(mu, "/")
		if strings.HasPrefix(mu, "releases/") {
			parts := strings.SplitN(strings.TrimPrefix(mu, "releases/"), "/", 2)
			if len(parts) >= 1 && parts[0] != "" {
				result.buildID = parts[0]
				result.isVersioned = true
			}
		}
	}

	return result, nil
}

// CacheControlForPath returns the appropriate Cache-Control header for a given file path.
func CacheControlForPath(path string) string {
	switch {
	case path == "packages.json":
		// Root packages.json — the only mutable object.
		return "public, max-age=300"
	case strings.HasPrefix(path, "releases/"):
		// Everything under a release prefix is immutable.
		return "public, max-age=31536000, immutable"
	// Legacy flat-path cases (backward compat during transition).
	case path == "manifest.json":
		return "public, max-age=300"
	case strings.HasPrefix(path, "p/") && strings.Contains(path, "$"):
		return "public, max-age=31536000, immutable"
	case strings.HasPrefix(path, "p2/"):
		return "public, max-age=300"
	default:
		return "public, max-age=300"
	}
}

func newS3Client(cfg config.R2Config) *s3.Client {
	return s3.New(s3.Options{
		Region: "auto",
		Credentials: credentials.NewStaticCredentialsProvider(
			cfg.AccessKeyID,
			cfg.SecretAccessKey,
			"",
		),
		BaseEndpoint: aws.String(cfg.Endpoint),
	})
}

// deterministicJSON produces JSON with recursively sorted keys.
// Local copy to avoid cross-package dependency on internal/repository.
func deterministicJSON(v any) ([]byte, error) {
	return json.Marshal(sortKeys(v))
}

func sortKeys(v any) any {
	switch val := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		sorted := make(map[string]any, len(val))
		for _, k := range keys {
			sorted[k] = sortKeys(val[k])
		}
		return sorted
	case []any:
		result := make([]any, len(val))
		for i, item := range val {
			result[i] = sortKeys(item)
		}
		return result
	default:
		return v
	}
}
