# Architecture

WP Packages has two primary runtime concerns:

1. Build and serve a static Composer repository.
2. Provide web/admin interfaces for package browsing and operations.

## System Components

- **Single binary** (`wppackages`) provides CLI commands and HTTP server.
- **SQLite** (WAL mode) as the sole runtime data store.
- **R2/CDN** serves Composer metadata artifacts (`packages.json`, `p2/`).
- **Caddy** reverse proxies app routes to the Go HTTP server.
- **systemd** manages the `serve` process and periodic timers.

### Build Pipeline Commands

- `wppackages discover` — discovers package slugs (config list or SVN).
- `wppackages update` — fetches and stores package metadata from WordPress.org.
- `wppackages build` — generates static Composer JSON artifacts.
- `wppackages deploy` — promotes a completed build, supports rollback/cleanup.
- `wppackages pipeline` — orchestrates discover → update → build → deploy.

### Static Repository Storage

- Immutable build directories under `storage/repository/builds/<build-id>/`.
- Atomic `current` symlink points to the promoted build.

### Web UI

- Public package browser/detail pages via server-rendered Go templates + Tailwind.
- Admin panel at `/admin` with in-app auth (email/password + session).

## Module Layout

```
cmd/wppackages/         CLI entrypoint (Cobra)
internal/
├── config/             env-first loading + optional YAML config
├── db/                 SQLite connection, pragmas, Goose migrations
├── wporg/              WordPress.org API + SVN clients
├── packages/           package normalization/storage logic
├── repository/         artifact generation, hashing, integrity validation
├── deploy/             local promote/rollback/cleanup + R2 sync
├── telemetry/          event ingestion, dedupe, rollups
└── http/               stdlib router, handlers, templates, static assets
```

## Data Flow

1. **Discovery** creates/updates shell package records (`type`, `name`, `last_committed`).
2. **Update** fetches full package payloads, normalizes versions, stores to `packages.versions_json`.
3. **Build** generates:
   - `packages.json` (with absolute `notify-batch` URL to app domain)
   - Composer v2 metadata files under `p2/`
   - `manifest.json` with build metrics and snapshot metadata
4. **Deploy** promotes a complete build by switching the `current` symlink and optionally syncing to R2.
5. **R2/CDN** serves static JSON directly; Caddy proxies dynamic routes to the Go app.

## Snapshot Consistency

- `wppackages update` stamps updated rows with `last_sync_run_id`.
- `wppackages build` snapshots `max(last_sync_run_id)` and only includes rows at or below that value.
- This prevents mixed-state builds when updates are running concurrently.

## Static Repository Layout

```
storage/repository/
├── current -> builds/20260313-140000
└── builds/
    ├── 20260313-140000/
    │   ├── packages.json
    │   ├── manifest.json
    │   └── p2/
    └── 20260313-130000/
```

## Public vs Admin Surface

### Public

- `GET /` — package browser with search/filter/sort/pagination
- `GET /packages/{type}/{name}` — package detail
- `POST /downloads` — Composer notify-batch endpoint (install telemetry)
- `GET /health` — status + package totals + last build metadata

### Admin

- `GET /admin` — dashboard
- `GET /admin/packages` — package management
- `GET /admin/builds` — build history/status
- Admin-triggered sync/build/deploy actions
- Access: in-app auth/authorization required

## Static Repository Deployment

Two deployment targets:

- **R2/CDN (production)** — `wppackages deploy --to-r2` syncs the build to Cloudflare R2 with appropriate `Cache-Control` headers. R2 custom domain + Cloudflare CDN serves the static files. See `docs/r2-deployment.md`.
- **Local (development)** — `wppackages deploy` updates the `current` symlink only.

## Scheduling

Periodic tasks run via systemd timers or cron (no in-process scheduler required):

- `wppackages pipeline` — every 5 minutes
- `wppackages aggregate-installs` — hourly
- `wppackages cleanup-sessions` — daily

Optional: `wppackages serve --with-scheduler` for in-process scheduling.