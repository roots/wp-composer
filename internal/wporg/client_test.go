package wporg

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/roots/wp-composer/internal/config"
)

func testClient(handler http.Handler) (*Client, *httptest.Server) {
	srv := httptest.NewServer(handler)
	cfg := config.DiscoveryConfig{
		APITimeoutS:  5,
		MaxRetries:   3,
		RetryDelayMs: 10, // fast retries for tests
	}
	c := NewClient(cfg, slog.Default())
	c.http = srv.Client()
	return c, srv
}

func TestFetchJSON_Success(t *testing.T) {
	c, srv := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"slug":"akismet","name":"Akismet","version":"5.0"}`)
	}))
	defer srv.Close()

	data, err := c.fetchJSON(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data["slug"] != "akismet" {
		t.Errorf("got slug=%v, want akismet", data["slug"])
	}
}

func TestFetchJSON_404(t *testing.T) {
	c, srv := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := c.fetchJSON(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error for 404")
	}
}

func TestFetchJSON_APIError(t *testing.T) {
	c, srv := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"error":"Plugin not found.","slug":"nonexistent"}`)
	}))
	defer srv.Close()

	_, err := c.fetchJSON(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error for API error response")
	}
}

func TestFetchJSON_RetryOnServerError(t *testing.T) {
	var attempts atomic.Int32
	c, srv := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"slug":"test"}`)
	}))
	defer srv.Close()

	data, err := c.fetchJSON(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error after retries: %v", err)
	}
	if data["slug"] != "test" {
		t.Errorf("got slug=%v, want test", data["slug"])
	}
	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts.Load())
	}
}

func TestFetchJSON_AllRetriesFail(t *testing.T) {
	c, srv := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := c.fetchJSON(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error when all retries fail")
	}
}

func TestFetchJSON_ContextCancelled(t *testing.T) {
	c, srv := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := c.fetchJSON(ctx, srv.URL)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestFetchJSON_InvalidJSON(t *testing.T) {
	c, srv := testClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `not json`)
	}))
	defer srv.Close()

	_, err := c.fetchJSON(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
