# Admin Access

## Security Model

Admin access uses defense in depth with two independent layers:

1. **Network layer** — Tailscale restricts access to `/admin/*` routes. Only devices on the tailnet can reach admin endpoints.
2. **Application layer** — in-app authentication (email/password) and admin authorization required for all protected `/admin/*` routes.

Both layers must pass. A valid Tailscale connection without app auth (or vice versa) is denied.

## Network Restriction

The Go app enforces IP-based access control on all `/admin/*` routes via the `ADMIN_ALLOW_CIDR` config.

Default allowed ranges (Tailscale):
- `100.64.0.0/10` (Tailscale IPv4)
- `fd7a:115c:a1e0::/48` (Tailscale IPv6)

Override via environment:

```env
ADMIN_ALLOW_CIDR=100.64.0.0/10,fd7a:115c:a1e0::/48,10.0.0.0/8
```

Set to empty to disable IP restriction (development only):

```env
ADMIN_ALLOW_CIDR=
```

If all configured CIDRs are invalid, the middleware fails closed — all admin access is blocked until the configuration is corrected.

### Proxy trust

By default, the app uses the raw `RemoteAddr` for IP checks. If behind a reverse proxy (Caddy, nginx), enable proxy header trust:

```env
TRUST_PROXY=true
```

This enables Chi's `RealIP` middleware, which reads `X-Forwarded-For` / `X-Real-IP` headers to determine the client IP. **Only enable this when the app is behind a trusted proxy** — otherwise clients can spoof their IP via forwarding headers and bypass admin IP restrictions.

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

## Exposure Verification

To verify admin is not publicly accessible:

```bash
# From outside the tailnet — should return 403
curl -s -o /dev/null -w "%{http_code}" https://app.example.com/admin/login

# From inside the tailnet — should return 200
curl -s -o /dev/null -w "%{http_code}" https://app.example.com/admin/login
```

## Emergency Password Reset

If locked out of the admin panel:

```bash
# SSH to the server
ssh deploy@your-server

# Reset the password
echo 'new-password' | wpcomposer admin reset-password --email admin@example.com --password-stdin
```

No database access or application restart required.
