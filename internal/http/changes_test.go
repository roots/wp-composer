package http

import (
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/roots/wp-packages/internal/app"
)

func setupChangesTestApp(t *testing.T) *app.App {
	t.Helper()
	a := setupTestApp(t)

	_, _ = a.DB.Exec(`
		CREATE TABLE metadata_changes (
			id INTEGER PRIMARY KEY,
			package_name TEXT NOT NULL,
			action TEXT NOT NULL CHECK(action IN ('update', 'delete')),
			timestamp INTEGER NOT NULL,
			build_id TEXT NOT NULL
		);
		CREATE INDEX idx_metadata_changes_timestamp ON metadata_changes(timestamp);
	`)

	return a
}

func TestMetadataChanges_NoSince(t *testing.T) {
	a := setupChangesTestApp(t)
	handler := handleMetadataChanges(a)

	req := httptest.NewRequest("GET", "/metadata/changes.json", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp changesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == "" {
		t.Error("expected error message for missing since")
	}
	if resp.Timestamp == 0 {
		t.Error("expected non-zero timestamp")
	}
	if len(resp.Actions) != 0 {
		t.Errorf("expected no actions, got %v", resp.Actions)
	}
	// Verify actions field is omitted from JSON (not "actions":null)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err == nil {
		if _, exists := raw["actions"]; exists {
			t.Error("expected actions field to be omitted from error response")
		}
	}
}

func TestMetadataChanges_InvalidSince(t *testing.T) {
	a := setupChangesTestApp(t)
	handler := handleMetadataChanges(a)

	for _, since := range []string{"abc", "-1", "12.5"} {
		req := httptest.NewRequest("GET", "/metadata/changes.json?since="+since, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != 200 {
			t.Errorf("since=%s: status = %d, want 200", since, w.Code)
		}

		var resp changesResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("since=%s: decode: %v", since, err)
		}
		if resp.Error == "" {
			t.Errorf("since=%s: expected error message", since)
		}
	}
}

func TestMetadataChanges_WithSinceNoChanges(t *testing.T) {
	a := setupChangesTestApp(t)
	handler := handleMetadataChanges(a)

	since := time.Now().UnixMilli() - 1000
	req := httptest.NewRequest("GET", "/metadata/changes.json?since="+itoa(since), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp changesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error != "" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
	if len(resp.Actions) != 0 {
		t.Errorf("expected 0 actions, got %d", len(resp.Actions))
	}
}

func TestMetadataChanges_WithChanges(t *testing.T) {
	a := setupChangesTestApp(t)
	handler := handleMetadataChanges(a)

	now := time.Now().UnixMilli()
	_, _ = a.DB.Exec(`INSERT INTO metadata_changes (package_name, action, timestamp, build_id)
		VALUES ('wp-plugin/akismet', 'update', ?, 'build-1')`, now)
	_, _ = a.DB.Exec(`INSERT INTO metadata_changes (package_name, action, timestamp, build_id)
		VALUES ('wp-theme/twentytwentyfour', 'update', ?, 'build-1')`, now)

	since := now - 1000
	req := httptest.NewRequest("GET", "/metadata/changes.json?since="+itoa(since), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp changesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(resp.Actions))
	}

	// Verify action time is in seconds (not milliseconds)
	for _, action := range resp.Actions {
		if action.Time > now/1000+1 {
			t.Errorf("action time %d looks like milliseconds, expected seconds", action.Time)
		}
	}
}

func TestMetadataChanges_Deduplication(t *testing.T) {
	a := setupChangesTestApp(t)
	handler := handleMetadataChanges(a)

	now := time.Now().UnixMilli()
	// Same package updated in two builds — should return only the latest action
	_, _ = a.DB.Exec(`INSERT INTO metadata_changes (package_name, action, timestamp, build_id)
		VALUES ('wp-plugin/akismet', 'update', ?, 'build-1')`, now-500)
	_, _ = a.DB.Exec(`INSERT INTO metadata_changes (package_name, action, timestamp, build_id)
		VALUES ('wp-plugin/akismet', 'delete', ?, 'build-2')`, now)

	since := now - 1000
	req := httptest.NewRequest("GET", "/metadata/changes.json?since="+itoa(since), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp changesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Actions) != 1 {
		t.Fatalf("expected 1 deduplicated action, got %d", len(resp.Actions))
	}
	if resp.Actions[0].Type != "delete" {
		t.Errorf("expected latest action 'delete', got '%s'", resp.Actions[0].Type)
	}
}

func TestMetadataChanges_ResyncForOldSince(t *testing.T) {
	a := setupChangesTestApp(t)
	handler := handleMetadataChanges(a)

	// since=0 is definitely older than 24h retention
	req := httptest.NewRequest("GET", "/metadata/changes.json?since=0", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp changesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Actions) != 1 {
		t.Fatalf("expected 1 resync action, got %d", len(resp.Actions))
	}
	if resp.Actions[0].Type != "resync" {
		t.Errorf("expected 'resync' action, got '%s'", resp.Actions[0].Type)
	}
	if resp.Actions[0].Package != "*" {
		t.Errorf("expected package '*', got '%s'", resp.Actions[0].Package)
	}
}

func itoa(i int64) string {
	return strconv.FormatInt(i, 10)
}
