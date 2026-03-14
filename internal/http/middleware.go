package http

import (
	"context"
	"database/sql"
	"log/slog"
	"net"
	"net/http"

	"github.com/roots/wp-composer/internal/auth"
)

type contextKey string

const userContextKey contextKey = "user"

func UserFromContext(ctx context.Context) *auth.User {
	u, _ := ctx.Value(userContextKey).(*auth.User)
	return u
}

func withUser(ctx context.Context, u *auth.User) context.Context {
	return context.WithValue(ctx, userContextKey, u)
}

func SessionAuth(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("session")
			if err != nil {
				http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
				return
			}

			user, err := auth.ValidateSession(r.Context(), db, cookie.Value)
			if err != nil {
				http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
				return
			}

			ctx := withUser(r.Context(), user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user == nil || !user.IsAdmin {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAllowedIP restricts access to requests from allowed CIDR ranges.
// If allowCIDRs is nil or empty, all requests are allowed (no network restriction).
// If CIDRs are configured but none parse successfully, access is denied (fail closed).
func RequireAllowedIP(allowCIDRs []string, logger *slog.Logger) func(http.Handler) http.Handler {
	configured := len(allowCIDRs) > 0
	var nets []*net.IPNet
	for _, cidr := range allowCIDRs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			logger.Error("invalid admin CIDR — admin access will be restricted", "cidr", cidr, "error", err)
			continue
		}
		nets = append(nets, ipNet)
	}

	// Fail closed: CIDRs were configured but none parsed
	failClosed := configured && len(nets) == 0
	if failClosed {
		logger.Error("all admin CIDRs are invalid — admin access is blocked")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if failClosed {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			if !configured {
				next.ServeHTTP(w, r)
				return
			}

			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				host = r.RemoteAddr
			}
			ip := net.ParseIP(host)
			if ip == nil {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			for _, n := range nets {
				if n.Contains(ip) {
					next.ServeHTTP(w, r)
					return
				}
			}

			logger.Warn("admin access denied", "ip", host)
			http.Error(w, "Forbidden", http.StatusForbidden)
		})
	}
}
