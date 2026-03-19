package og

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/roots/wp-packages/internal/config"
)

// Uploader handles uploading OG images to R2 CDN or local disk.
type Uploader struct {
	client    *s3.Client
	bucket    string
	publicURL string
	localDir  string
}

// NewUploader creates an Uploader. If R2 CDN is configured, it uploads to R2.
// Otherwise, it writes to localDir on disk.
func NewUploader(cfg config.R2Config) *Uploader {
	u := &Uploader{
		publicURL: cfg.CDNPublicURL,
	}

	if cfg.CDNBucket != "" && cfg.Endpoint != "" && cfg.AccessKeyID != "" {
		u.client = s3.New(s3.Options{
			Region: "auto",
			Credentials: credentials.NewStaticCredentialsProvider(
				cfg.AccessKeyID,
				cfg.SecretAccessKey,
				"",
			),
			BaseEndpoint: aws.String(cfg.Endpoint),
		})
		u.bucket = cfg.CDNBucket
	} else {
		u.localDir = filepath.Join("storage", "og")
	}

	return u
}

// Upload stores the PNG bytes at the given key (e.g. "social/plugin/akismet.png").
func (u *Uploader) Upload(ctx context.Context, key string, data []byte) error {
	if u.client != nil {
		_, err := u.client.PutObject(ctx, &s3.PutObjectInput{
			Bucket:       aws.String(u.bucket),
			Key:          aws.String(key),
			Body:         bytes.NewReader(data),
			ContentType:  aws.String("image/png"),
			CacheControl: aws.String("public, max-age=86400"),
		})
		if err != nil {
			return fmt.Errorf("uploading %s to R2: %w", key, err)
		}
		return nil
	}

	// Local disk fallback
	path := filepath.Join(u.localDir, key)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating directory for %s: %w", key, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", key, err)
	}
	return nil
}

// PublicURL returns the full public URL for an OG image key.
func (u *Uploader) PublicURL(key string) string {
	if u.publicURL != "" {
		return u.publicURL + "/" + key
	}
	return ""
}

// IsR2 returns true if the uploader is configured for R2.
func (u *Uploader) IsR2() bool {
	return u.client != nil
}
