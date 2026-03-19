package deploy

import (
	"bytes"
	"context"
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
	prefix := aws.ToString(input.Prefix)
	var contents []s3types.Object
	for key := range f.objects {
		if prefix == "" || strings.HasPrefix(key, prefix) {
			contents = append(contents, s3types.Object{Key: aws.String(key)})
		}
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

// rootJSONWithBuildID builds a root packages.json with a build-id field.
func rootJSONWithBuildID(buildID string) []byte {
	return []byte(fmt.Sprintf(`{"build-id":"%s","metadata-url":"/p2/%%package%%.json"}`, buildID))
}

// rootJSONNoBuildID builds a root packages.json without a build-id field.
func rootJSONNoBuildID() []byte {
	return []byte(`{"metadata-url":"/p2/%package%.json"}`)
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

func TestCacheControlForPaths(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"packages.json", "public, max-age=300"},
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

func TestFetchLiveBuildIDTransientError(t *testing.T) {
	fake := &fakeR2{
		objects: map[string][]byte{},
		getErr:  fmt.Errorf("connection reset by peer"),
	}

	_, err := fetchLiveBuildID(context.Background(), fake, "test-bucket")
	if err == nil {
		t.Fatal("expected fetchLiveBuildID to return error on transient GetObject failure")
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("error should contain original cause, got: %v", err)
	}
}

func TestCleanupRefusesWhenLiveReleaseUnknown(t *testing.T) {
	fake := &fakeR2{
		objects: map[string][]byte{
			"packages.json":                                      rootJSONNoBuildID(),
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

	if len(fake.deletedKeys) > 0 {
		t.Errorf("expected no deletions, got %d: %v", len(fake.deletedKeys), fake.deletedKeys)
	}
}

func TestCleanupIgnoresNonReleaseFiles(t *testing.T) {
	liveID := "20260314-150000"
	fake := &fakeR2{
		objects: map[string][]byte{
			"packages.json":                         rootJSONWithBuildID(liveID),
			"releases/" + liveID + "/packages.json": []byte(`{}`),
			"p2/wp-plugin/akismet.json":             []byte(`{}`),
		},
	}

	deleted, err := cleanupR2(context.Background(), fake, "test-bucket", 0, 1, slog.Default())
	if err != nil {
		t.Fatalf("cleanupR2: %v", err)
	}
	if deleted != 0 {
		t.Errorf("expected 0 deletions (only live release exists), got %d", deleted)
	}
}

func TestFetchLiveBuildID(t *testing.T) {
	tests := []struct {
		name   string
		root   []byte
		wantID string
	}{
		{
			name:   "with build-id",
			root:   rootJSONWithBuildID("20260314-150000"),
			wantID: "20260314-150000",
		},
		{
			name:   "without build-id",
			root:   rootJSONNoBuildID(),
			wantID: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeR2{objects: map[string][]byte{"packages.json": tt.root}}
			got, err := fetchLiveBuildID(context.Background(), fake, "test-bucket")
			if err != nil {
				t.Fatalf("fetchLiveBuildID: %v", err)
			}
			if got != tt.wantID {
				t.Errorf("buildID = %q, want %q", got, tt.wantID)
			}
		})
	}
}
