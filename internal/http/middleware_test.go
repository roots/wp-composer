package http

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireAllowedIP_Allowed(t *testing.T) {
	mw := RequireAllowedIP([]string{"100.64.0.0/10"}, slog.Default())
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/admin", nil)
	req.RemoteAddr = "100.100.50.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Tailscale IP should be allowed, got %d", w.Code)
	}
}

func TestRequireAllowedIP_Denied(t *testing.T) {
	mw := RequireAllowedIP([]string{"100.64.0.0/10"}, slog.Default())
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/admin", nil)
	req.RemoteAddr = "203.0.113.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Errorf("public IP should be denied, got %d", w.Code)
	}
}

func TestRequireAllowedIP_EmptyCIDR(t *testing.T) {
	mw := RequireAllowedIP(nil, slog.Default())
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/admin", nil)
	req.RemoteAddr = "203.0.113.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("empty CIDR list should allow all, got %d", w.Code)
	}
}

func TestRequireAllowedIP_IPv6(t *testing.T) {
	mw := RequireAllowedIP([]string{"fd7a:115c:a1e0::/48"}, slog.Default())
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/admin", nil)
	req.RemoteAddr = "[fd7a:115c:a1e0::1]:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Tailscale IPv6 should be allowed, got %d", w.Code)
	}
}

func TestRequireAllowedIP_FailClosed(t *testing.T) {
	// All CIDRs invalid — should deny everything
	mw := RequireAllowedIP([]string{"not-a-cidr", "also-bad"}, slog.Default())
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/admin", nil)
	req.RemoteAddr = "100.100.50.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 403 {
		t.Errorf("invalid CIDRs should fail closed, got %d", w.Code)
	}
}

func TestRequireAllowedIP_MultipleCIDRs(t *testing.T) {
	mw := RequireAllowedIP([]string{"10.0.0.0/8", "100.64.0.0/10"}, slog.Default())
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Matches second CIDR
	req := httptest.NewRequest("GET", "/admin", nil)
	req.RemoteAddr = "100.100.50.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("should match second CIDR, got %d", w.Code)
	}
}
