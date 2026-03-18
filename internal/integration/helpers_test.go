//go:build integration || wporg_live

package integration

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.Default()
}

func httpGet(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET %s: status %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response from %s: %v", url, err)
	}
	return string(body)
}

func writeComposerJSON(t *testing.T, dir, repoURL string, require map[string]string) {
	t.Helper()
	data := map[string]any{
		"repositories": []map[string]any{
			{
				"type": "composer",
				"url":  repoURL,
			},
		},
		"require": require,
		"config": map[string]any{
			"allow-plugins": map[string]any{
				"composer/installers": true,
			},
			"secure-http": false,
		},
	}
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		t.Fatalf("marshaling composer.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), jsonData, 0644); err != nil {
		t.Fatalf("writing composer.json: %v", err)
	}
}

func runComposer(t *testing.T, composerPath, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(composerPath, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("composer %s failed: %v\noutput: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}
