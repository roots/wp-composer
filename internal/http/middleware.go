package http

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/roots/wp-packages/internal/auth"
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
