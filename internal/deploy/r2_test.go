package deploy

import (
	"os"
	"path/filepath"
	"testing"
)

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
