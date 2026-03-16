# Admin Access

## Security Model

Admin access is protected by in-app authentication. Email/password login and admin authorization are required for all protected `/admin/*` routes.

## Proxy Trust

When behind a reverse proxy (Caddy, nginx), enable proxy header trust so the app sees the real client IP (used for login rate limiting and telemetry dedupe):

```env
TRUST_PROXY=true
```

This reads `X-Real-IP` / `X-Forwarded-For` headers to determine the client IP. **Only enable this when the app is behind a trusted proxy** — otherwise clients can spoof their IP.

## Admin Bootstrap

### Create initial admin user

```bash
echo 'secure-password' | wpcomposer admin create --email admin@example.com --name "Admin" --password-stdin
```

### Promote existing user to admin

```bash
wpcomposer admin promote --email user@example.com
```

### Reset admin password

```bash
echo 'new-password' | wpcomposer admin reset-password --email admin@example.com --password-stdin
```

## Login/Logout

- **Login:** `GET /admin/login` renders a login form. `POST /admin/login` authenticates with email/password and creates a server-side session.
- **Logout:** `POST /admin/logout` destroys the session and clears the cookie.
- **Session cookie:** `session`, HttpOnly, Secure (in production), SameSite=Lax.
- **Session lifetime:** configurable via `SESSION_LIFETIME_MINUTES` (default 7200 minutes / 5 days).

## Session Cleanup

Expired sessions accumulate in the `sessions` table. Clean them periodically:

```bash
wpcomposer cleanup-sessions
```

Run via systemd timer or cron (daily recommended).

## Emergency Password Reset

If locked out of the admin panel:

```bash
# SSH to the server
ssh deploy@your-server

# Reset the password
echo 'new-password' | wpcomposer admin reset-password --email admin@example.com --password-stdin
```

No database access or application restart required.
