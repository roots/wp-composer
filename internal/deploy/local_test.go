package deploy

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func createTestBuild(t *testing.T, repoDir, buildID string) {
	t.Helper()
	buildDir := filepath.Join(repoDir, "builds", buildID)
	_ = os.MkdirAll(buildDir, 0755)
	_ = os.WriteFile(filepath.Join(buildDir, "packages.json"), []byte(`{}`), 0644)
	_ = os.WriteFile(filepath.Join(buildDir, "manifest.json"), []byte(`{}`), 0644)
}

func TestPromoteAndCurrent(t *testing.T) {
	repoDir := t.TempDir()
	createTestBuild(t, repoDir, "20260313-140000")

	err := Promote(repoDir, "20260313-140000", slog.Default())
	if err != nil {
		t.Fatalf("promote: %v", err)
	}

	id, err := CurrentBuildID(repoDir)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if id != "20260313-140000" {
		t.Errorf("current = %s, want 20260313-140000", id)
	}

	// Verify symlink resolves correctly (packages.json readable through current/)
	currentPkgs := filepath.Join(repoDir, "current", "packages.json")
	if _, err := os.Stat(currentPkgs); err != nil {
		t.Errorf("current/packages.json not resolvable through symlink: %v", err)
	}
}

func TestPromoteInvalidBuild(t *testing.T) {
	repoDir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(repoDir, "builds", "bad-build"), 0755)

	err := Promote(repoDir, "bad-build", slog.Default())
	if err == nil {
		t.Fatal("expected error promoting invalid build")
	}
}

func TestRollback(t *testing.T) {
	repoDir := t.TempDir()
	createTestBuild(t, repoDir, "20260313-130000")
	createTestBuild(t, repoDir, "20260313-140000")

	_ = Promote(repoDir, "20260313-140000", slog.Default())

	targetID, err := Rollback(repoDir, "", slog.Default())
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if targetID != "20260313-130000" {
		t.Errorf("rolled back to %s, want 20260313-130000", targetID)
	}

	id, _ := CurrentBuildID(repoDir)
	if id != "20260313-130000" {
		t.Errorf("current after rollback = %s", id)
	}
}

func TestRollbackToSpecific(t *testing.T) {
	repoDir := t.TempDir()
	createTestBuild(t, repoDir, "20260313-120000")
	createTestBuild(t, repoDir, "20260313-130000")
	createTestBuild(t, repoDir, "20260313-140000")

	_ = Promote(repoDir, "20260313-140000", slog.Default())

	targetID, err := Rollback(repoDir, "20260313-120000", slog.Default())
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if targetID != "20260313-120000" {
		t.Errorf("rolled back to %s, want 20260313-120000", targetID)
	}
}

func TestCleanup(t *testing.T) {
	repoDir := t.TempDir()
	createTestBuild(t, repoDir, "20260313-080000")
	createTestBuild(t, repoDir, "20260313-090000")
	createTestBuild(t, repoDir, "20260313-100000")
	createTestBuild(t, repoDir, "20260313-110000")
	createTestBuild(t, repoDir, "20260313-120000")
	createTestBuild(t, repoDir, "20260313-130000")
	createTestBuild(t, repoDir, "20260313-140000")
	createTestBuild(t, repoDir, "20260313-150000")

	_ = Promote(repoDir, "20260313-150000", slog.Default())

	// retainCount is clamped to a minimum of 5
	removed, err := Cleanup(repoDir, 5, slog.Default())
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if removed != 2 {
		t.Errorf("removed %d, want 2", removed)
	}

	builds, _ := ListBuilds(repoDir)
	if len(builds) != 6 {
		t.Errorf("remaining builds = %d, want 6 (current + 5 retained)", len(builds))
	}
}

func TestListBuilds(t *testing.T) {
	repoDir := t.TempDir()
	createTestBuild(t, repoDir, "20260313-130000")
	createTestBuild(t, repoDir, "20260313-140000")

	builds, err := ListBuilds(repoDir)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(builds) != 2 {
		t.Fatalf("expected 2 builds, got %d", len(builds))
	}
	if builds[0] != "20260313-130000" || builds[1] != "20260313-140000" {
		t.Errorf("builds not sorted: %v", builds)
	}
}

func TestCacheControlForPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"packages.json", "public, max-age=300"},
		{"manifest.json", "public, max-age=300"},
		{"p/providers-this-week$abc123.json", "public, max-age=31536000, immutable"},
		{"p/wp-plugin/akismet$def456.json", "public, max-age=31536000, immutable"},
		{"p2/wp-plugin/akismet.json", "public, max-age=300"},
		{"p2/wp-theme/astra.json", "public, max-age=300"},
		// Release-prefixed paths are all immutable.
		{"releases/20260314-150405/packages.json", "public, max-age=31536000, immutable"},
		{"releases/20260314-150405/manifest.json", "public, max-age=31536000, immutable"},
		{"releases/20260314-150405/p2/wp-plugin/akismet.json", "public, max-age=31536000, immutable"},
		{"releases/20260314-150405/p/wp-plugin/akismet$abc.json", "public, max-age=31536000, immutable"},
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
