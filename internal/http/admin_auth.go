package http

import (
	"html"
	"net/http"
	"time"

	"github.com/roots/wp-packages/internal/app"
	"github.com/roots/wp-packages/internal/auth"
)

func handleLoginPage(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// If already authenticated, redirect to admin
		if cookie, err := r.Cookie("session"); err == nil {
			if _, err := auth.ValidateSession(r.Context(), a.DB, cookie.Value); err == nil {
				http.Redirect(w, r, "/admin", http.StatusSeeOther)
				return
			}
		}

		errMsg := r.URL.Query().Get("error")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(loginHTML(errMsg)))
	}
}

func handleLogin(a *app.App) http.HandlerFunc {
	limiter := newLoginRateLimiter()

	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		now := time.Now()
		if !limiter.allow(ip, now) {
			http.Redirect(w, r, "/admin/login?error=too+many+attempts", http.StatusSeeOther)
			return
		}

		if err := r.ParseForm(); err != nil {
			http.Redirect(w, r, "/admin/login?error=invalid+request", http.StatusSeeOther)
			return
		}

		email := r.FormValue("email")
		password := r.FormValue("password")

		if email == "" || password == "" {
			limiter.recordFailure(ip, now)
			http.Redirect(w, r, "/admin/login?error=email+and+password+required", http.StatusSeeOther)
			return
		}

		user, err := auth.GetUserByEmail(r.Context(), a.DB, email)
		if err != nil {
			limiter.recordFailure(ip, now)
			http.Redirect(w, r, "/admin/login?error=invalid+credentials", http.StatusSeeOther)
			return
		}

		if err := auth.CheckPassword(user.PasswordHash, password); err != nil {
			limiter.recordFailure(ip, now)
			http.Redirect(w, r, "/admin/login?error=invalid+credentials", http.StatusSeeOther)
			return
		}

		if !user.IsAdmin {
			limiter.recordFailure(ip, now)
			http.Redirect(w, r, "/admin/login?error=not+authorized", http.StatusSeeOther)
			return
		}

		sessionID, err := auth.CreateSession(r.Context(), a.DB, user.ID, a.Config.Session.LifetimeMinutes)
		if err != nil {
			a.Logger.Error("failed to create session", "error", err)
			captureError(r, err)
			http.Redirect(w, r, "/admin/login?error=internal+error", http.StatusSeeOther)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Value:    sessionID,
			Path:     "/",
			HttpOnly: true,
			Secure:   a.Config.Env == "production",
			SameSite: http.SameSiteLaxMode,
			MaxAge:   a.Config.Session.LifetimeMinutes * 60,
		})
		limiter.recordSuccess(ip)

		http.Redirect(w, r, "/admin", http.StatusSeeOther)
	}
}

func handleLogout(a *app.App) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie("session"); err == nil {
			_ = auth.DeleteSession(r.Context(), a.DB, cookie.Value)
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			MaxAge:   -1,
			Expires:  time.Unix(0, 0),
		})

		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
	}
}

func loginHTML(errMsg string) string {
	errorBlock := ""
	if errMsg != "" {
		errorBlock = `<p style="color:#dc2626;margin-bottom:1rem">` + html.EscapeString(errMsg) + `</p>`
	}
	return `<!DOCTYPE html>
<html><head><title>Admin Login — WP Composer</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
body{font-family:system-ui,sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0;background:#f8fafc}
form{background:#fff;padding:2rem;border-radius:8px;box-shadow:0 1px 3px rgba(0,0,0,.1);width:100%;max-width:360px}
h1{font-size:1.25rem;margin:0 0 1.5rem}
label{display:block;font-size:.875rem;font-weight:500;margin-bottom:.25rem}
input[type=email],input[type=password]{width:100%;padding:.5rem;border:1px solid #d1d5db;border-radius:4px;font-size:1rem;margin-bottom:1rem;box-sizing:border-box}
button{width:100%;padding:.625rem;background:#1e40af;color:#fff;border:none;border-radius:4px;font-size:1rem;cursor:pointer}
button:hover{background:#1e3a8a}
</style></head><body>
<form method="POST" action="/admin/login">
<h1>Admin Login</h1>
` + errorBlock + `
<label for="email">Email</label>
<input type="email" id="email" name="email" required autofocus>
<label for="password">Password</label>
<input type="password" id="password" name="password" required>
<button type="submit">Sign in</button>
</form></body></html>`
}
