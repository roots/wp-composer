# Telemetry

Install telemetry records installs via Composer's `notify-batch` mechanism.

## Goal

- Count installs initiated through WP Packages package metadata.
- No impact on download latency (notification is sent after install).
- Avoid counting rapid duplicate retries from the same client.

## How It Works

Composer has built-in support for install notifications. When `packages.json` includes a `notify-batch` URL, the Composer client POSTs install data after downloading packages.

### packages.json

```json
{
  "notify-batch": "https://app.example.com/downloads",
  "packages": [],
  ...
}
```

The `notify-batch` URL must be absolute and point to the app domain (not the R2/CDN domain).

### Notification Flow

1. Composer resolves and downloads packages directly from WordPress.org (dist URLs point to `downloads.wordpress.org`).
2. After install, Composer POSTs to `/downloads` with the list of installed packages.
3. The app ingests events inline with `INSERT OR IGNORE` (no background queue).
4. If the server is unreachable, the install still succeeds — notifications are best-effort.

### POST Format

Composer sends:

```json
{
  "downloads": [
    {"name": "wp-plugin/akismet", "version": "5.0.0"},
    {"name": "wp-theme/astra", "version": "4.0.0"}
  ]
}
```

### Response

```json
{"status": "ok"}
```

Always returns `200` — best-effort, non-blocking for client workflows.

## Event Ingestion

Data model:

- Table: `install_events`
- Columns: `package_id`, `version`, `ip_hash`, `user_agent_hash`, `dedupe_bucket`, `dedupe_hash`, `created_at`

### Dedupe Strategy (SQLite)

Uses a rolling time bucket and unique constraint instead of advisory locks:

```
dedupe_bucket = floor(unix_timestamp / dedupe_window_seconds)
dedupe_hash   = sha256(ip_hash + package_id + version + user_agent_hash)
```

- Unique constraint on `(dedupe_hash, dedupe_bucket)`.
- Ingest via `INSERT OR IGNORE` — duplicates within the same bucket are silently dropped.
- Default `dedupe_window_seconds`: `3600` (1 hour).

### Tradeoffs

- Dedupe at bucket granularity (close to the Laravel app's rolling-window behavior).
- Simpler and lock-free under SQLite's write serialization.
- No background queue needed — inline `INSERT OR IGNORE` is fast enough for expected traffic.

## Aggregation

Periodic command (run hourly via systemd timer or cron):

```bash
wppackages aggregate-installs
```

Rollups written to `packages`:

| Column | Description |
|--------|-------------|
| `wp_packages_installs_total` | All-time install count |
| `wp_packages_installs_30d` | Installs in the last 30 days |
| `last_installed_at` | Timestamp of most recent install |

## UI Integration

Public browser and detail pages expose:

- WordPress.org download count (from package metadata).
- WP Packages install counters (when non-zero).
- Sort by Composer installs.

## Operational Notes

- The `/downloads` endpoint is lightweight — validates the POST and inserts inline.
- If the endpoint is down, Composer installs are unaffected (users still download directly from WordPress.org).
- Stale counters indicate `aggregate-installs` hasn't run — check the timer/cron.
