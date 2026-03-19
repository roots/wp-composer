package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

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

// SyncToR2 uploads build files to R2. Only p2/ files and packages.json are uploaded.
// p2/ files are skipped if unchanged from the previous build (byte-compared locally).
// packages.json is uploaded last.
func SyncToR2(ctx context.Context, cfg config.R2Config, buildDir, buildID, previousBuildDir string, logger *slog.Logger) error {
	client := newS3Client(cfg)

	// Collect file paths only (not data) to avoid loading everything into memory.
	var filePaths []string
	err := filepath.Walk(buildDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, err := filepath.Rel(buildDir, path)
		if err != nil {
			return err
		}
		relPath := strings.ReplaceAll(rel, string(os.PathSeparator), "/")
		// Only upload p2/ files and packages.json
		if strings.HasPrefix(relPath, "p2/") || relPath == r2IndexFile {
			filePaths = append(filePaths, relPath)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("walking build files: %w", err)
	}

	total := len(filePaths)

	// Upload p2/ files in parallel, packages.json last.
	var uploaded, skipped atomic.Int64
	g, gCtx := errgroup.WithContext(ctx)
	g.SetLimit(50)

	for _, relPath := range filePaths {
		relPath := relPath
		if relPath == r2IndexFile {
			continue // upload last
		}
		g.Go(func() error {
			// Skip unchanged p2/ files
			if previousBuildDir != "" && fileUnchanged(previousBuildDir, buildDir, relPath) {
				skipped.Add(1)
				return nil
			}
			data, err := os.ReadFile(filepath.Join(buildDir, relPath))
			if err != nil {
				return fmt.Errorf("reading %s: %w", relPath, err)
			}
			if err := putObjectWithRetry(gCtx, client, cfg.Bucket, relPath, data, logger); err != nil {
				return fmt.Errorf("R2 sync: %w", err)
			}
			n := uploaded.Add(1)
			if (n+skipped.Load())%500 == 0 {
				logger.Info("R2 upload progress", "uploaded", n, "skipped", skipped.Load(), "total", total)
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	// Upload packages.json last.
	packagesData, err := os.ReadFile(filepath.Join(buildDir, r2IndexFile))
	if err != nil {
		return fmt.Errorf("R2 sync: reading packages.json: %w", err)
	}
	if err := putObjectWithRetry(ctx, client, cfg.Bucket, r2IndexFile, packagesData, logger); err != nil {
		return fmt.Errorf("R2 sync (root packages.json): %w", err)
	}

	logger.Info("R2 sync complete", "uploaded", uploaded.Load(), "skipped", skipped.Load())
	return nil
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

// fileUnchanged returns true if relPath exists in both directories with identical content.
func fileUnchanged(prevDir, curDir, relPath string) bool {
	if prevDir == "" {
		return false
	}
	prevPath := filepath.Join(prevDir, filepath.FromSlash(relPath))
	curPath := filepath.Join(curDir, filepath.FromSlash(relPath))

	prevData, err := os.ReadFile(prevPath)
	if err != nil {
		return false
	}
	curData, err := os.ReadFile(curPath)
	if err != nil {
		return false
	}
	return bytes.Equal(prevData, curData)
}

// CleanupR2 removes old release prefixes from R2, keeping the live release,
// releases within the grace period, and the top N most recent releases.
func CleanupR2(ctx context.Context, cfg config.R2Config, graceHours, retainCount int, logger *slog.Logger) (int, error) {
	return cleanupR2(ctx, newS3Client(cfg), cfg.Bucket, graceHours, retainCount, logger)
}

func cleanupR2(ctx context.Context, client r2API, bucket string, graceHours, retainCount int, logger *slog.Logger) (int, error) {
	if retainCount < 5 {
		logger.Warn("retain count below minimum, clamping to 5", "requested", retainCount)
		retainCount = 5
	}
	if graceHours < 0 {
		graceHours = 24
	}

	// Fetch root packages.json to identify the live release via build-id field.
	live, err := fetchLiveBuildID(ctx, client, bucket)
	if err != nil {
		return 0, fmt.Errorf("identifying live release: %w", err)
	}

	// List only objects under releases/ prefix.
	releaseObjects := make(map[string][]string) // buildID -> list of keys
	var continuationToken *string
	for {
		input := &s3.ListObjectsV2Input{
			Bucket:            aws.String(bucket),
			Prefix:            aws.String("releases/"),
			ContinuationToken: continuationToken,
		}
		resp, err := client.ListObjectsV2(ctx, input)
		if err != nil {
			return 0, fmt.Errorf("listing R2 objects: %w", err)
		}

		for _, obj := range resp.Contents {
			key := aws.ToString(obj.Key)
			parts := strings.SplitN(strings.TrimPrefix(key, "releases/"), "/", 2)
			if len(parts) >= 1 && parts[0] != "" {
				releaseObjects[parts[0]] = append(releaseObjects[parts[0]], key)
			}
		}

		if !aws.ToBool(resp.IsTruncated) {
			break
		}
		continuationToken = resp.NextContinuationToken
	}

	if len(releaseObjects) == 0 {
		logger.Info("R2 cleanup: nothing to clean")
		return 0, nil
	}

	// Safety: if there are release prefixes on R2 but we couldn't identify the
	// live one, refuse to delete anything.
	if live == "" {
		return 0, fmt.Errorf("release prefixes exist on R2 but live release could not be identified — refusing to clean")
	}

	// Build the keep set: live release + within grace period + top N recent.
	keep := make(map[string]bool)
	keep[live] = true

	graceCutoff := time.Now().Add(-time.Duration(graceHours) * time.Hour)

	var releaseIDs []string
	for id := range releaseObjects {
		releaseIDs = append(releaseIDs, id)
	}
	sort.Strings(releaseIDs)

	for _, id := range releaseIDs {
		if t, err := time.Parse("20060102-150405", id); err == nil && t.After(graceCutoff) {
			keep[id] = true
		}
	}

	kept := 0
	for i := len(releaseIDs) - 1; i >= 0 && kept < retainCount; i-- {
		if !keep[releaseIDs[i]] {
			keep[releaseIDs[i]] = true
			kept++
		}
	}

	var toDelete []s3types.ObjectIdentifier
	for id, keys := range releaseObjects {
		if keep[id] {
			continue
		}
		for _, key := range keys {
			toDelete = append(toDelete, s3types.ObjectIdentifier{Key: aws.String(key)})
		}
	}

	if len(toDelete) == 0 {
		logger.Info("R2 cleanup: nothing to delete", "retained_releases", len(keep))
		return 0, nil
	}

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

// fetchLiveBuildID reads the root packages.json from R2 and extracts the
// build-id field. Returns "" if no root exists or no build-id is set.
// Returns an error on transient failures.
func fetchLiveBuildID(ctx context.Context, client r2API, bucket string) (string, error) {
	resp, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(r2IndexFile),
	})
	if err != nil {
		var nsk *s3types.NoSuchKey
		if errors.As(err, &nsk) {
			return "", nil
		}
		return "", fmt.Errorf("fetching root packages.json: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var pkg map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&pkg); err != nil {
		return "", fmt.Errorf("parsing root packages.json: %w", err)
	}

	if bid, ok := pkg["build-id"].(string); ok && bid != "" {
		return bid, nil
	}
	return "", nil
}

// CacheControlForPath returns the appropriate Cache-Control header for a given file path.
func CacheControlForPath(path string) string {
	if path == "packages.json" {
		return "public, max-age=300"
	}
	// All p2/ files are mutable
	return "public, max-age=300"
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
		UsePathStyle: true,
	})
}
