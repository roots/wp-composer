# Development

## Prerequisites

- Go 1.26+
- [air](https://github.com/air-verse/air) — `go install github.com/air-verse/air@latest`
- [golangci-lint](https://github.com/golangci/golangci-lint) — `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest` (for `make lint`)

## Quick Start

The fastest way to get a working local environment:

```bash
make dev
```

This builds the binary, runs migrations, creates an admin user (`admin@localhost` / `admin`), discovers and fetches all seed packages, builds repository artifacts, and starts the HTTP server.

## Manual Setup

```bash
# Build
make build

# Run migrations
./wpcomposer migrate

# Create admin user
echo 'your-password' | ./wpcomposer admin create \
  --email admin@example.com \
  --name "Admin" \
  --password-stdin

# Start server
./wpcomposer serve
```

## Commands

| Command | Purpose |
|---------|---------|
| `wpcomposer dev` | Bootstrap and start dev server (all-in-one) |
| `wpcomposer serve` | Start HTTP server |
| `wpcomposer migrate` | Apply database migrations |
| `wpcomposer discover` | Discover package slugs from WordPress.org |
| `wpcomposer update` | Fetch/store package metadata |
| `wpcomposer build` | Generate Composer repository artifacts |
| `wpcomposer deploy` | Promote build (local symlink and/or R2 sync) |
| `wpcomposer pipeline` | Run full discover → update → build → deploy |
| `wpcomposer aggregate-installs` | Recompute install counters |
| `wpcomposer cleanup-sessions` | Remove expired admin sessions |
| `wpcomposer admin create` | Create admin user |
| `wpcomposer admin promote` | Grant admin role to existing user |
| `wpcomposer admin reset-password` | Reset user password |

All commands accept `--config`, `--db`, and `--log-level` flags. See [`operations.md`](operations.md) for usage details.

## Configuration

Env-first with optional YAML config file (env overrides YAML).

| Variable | Default | Purpose |
|----------|---------|---------|
| `APP_URL` | — | App domain for `notify-batch` URL |
| `DB_PATH` | `./storage/wpcomposer.db` | SQLite database path |
| `WP_COMPOSER_DEPLOY_R2` | `false` | Enable R2 deploy |
| `R2_ACCESS_KEY_ID` | — | R2 credentials |
| `R2_SECRET_ACCESS_KEY` | — | R2 credentials |
| `R2_BUCKET` | — | R2 bucket name |
| `R2_ENDPOINT` | — | R2 S3-compatible endpoint |
| `TRUST_PROXY` | `false` | Trust `X-Forwarded-For` for client IP (enable behind proxy) |
| `SESSION_LIFETIME_MINUTES` | `7200` | Admin session lifetime |

## Technical Decisions

| Area | Choice |
|------|--------|
| CLI | Cobra |
| HTTP router | `net/http` (stdlib) |
| Migrations | Goose (SQL-first) |
| Templates | `html/template` + Tailwind |
| Logging | `log/slog` |
| SQLite driver | `modernc.org/sqlite` |
| R2 | AWS SDK for Go v2 |
| Config | env-first + optional YAML |
| Admin access | In-app auth (email/password + session) |

## Make Targets

```bash
make dev    # Build + bootstrap + serve (live-reload via air)
make test   # Run all tests
make build  # Build binary
make clean  # Remove artifacts
```
