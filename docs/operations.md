# Operations

Day-to-day operation of WP Composer in production-like environments.

## Prerequisites

- Ubuntu 24.04 LTS server
- Go binary (`wpcomposer`) deployed to the server
- SQLite database at configured path (default `./storage/wpcomposer.db`)
- Caddy configured as reverse proxy for app routes
- systemd service for `wpcomposer serve`
- systemd timers or cron for periodic commands

All prerequisites are automated via Ansible. See [Server Provisioning](#server-provisioning) below.

## Core Commands

### Migrate

```bash
wpcomposer migrate
```

Apply pending database migrations. Run this before first use and after upgrades.

### Discover

```bash
wpcomposer discover --source=svn
wpcomposer discover --source=config --type=plugin --limit=100
```

- `--source=svn` for full WordPress.org directory discovery.
- `--source=config` for curated seed lists.
- `--type plugin|theme|all` to filter by package type.
- `--concurrency` to control parallel fetches.

### Update

```bash
wpcomposer update
wpcomposer update --type=plugin --name=akismet --force
wpcomposer update --include-inactive
```

- Fetches full package payloads from WordPress.org and normalizes versions.
- Stamps `last_sync_run_id` for snapshot-consistent builds.
- `--force` re-fetches even if package hasn't changed.
- `--concurrency` to control parallel fetches.

### Build

```bash
wpcomposer build
wpcomposer build --force
wpcomposer build --package=wp-plugin/akismet
```

- Generates immutable build artifacts and `manifest.json`.
- Validates all hash references before marking build as successful.
- `--output` to specify a custom build output directory.

### Deploy

```bash
wpcomposer deploy
wpcomposer deploy 20260313-140000
wpcomposer deploy --rollback
wpcomposer deploy --rollback=20260313-130000
wpcomposer deploy --to-r2
wpcomposer deploy --cleanup
wpcomposer deploy --cleanup --r2-cleanup
wpcomposer deploy --cleanup --r2-cleanup --retain 5
wpcomposer deploy --cleanup --r2-cleanup --grace-hours 6
```

- Validates build artifacts before any sync or promotion.
- `--to-r2` uploads to a versioned release prefix (`releases/<build-id>/`), rewrites root `packages.json` as the atomic pointer swap, then promotes locally. If R2 sync fails, the local symlink is not updated.
- Rollback validates target build, syncs to R2 if enabled, then promotes.
- `--cleanup` removes old local builds beyond retention (default: 5 beyond current).
- `--r2-cleanup` removes stale release prefixes from R2 (must be combined with `--cleanup`). Reads R2 state directly — no local filesystem dependency.
- `--retain N` controls how many releases to keep beyond current (minimum: 5).
- `--grace-hours N` keeps releases younger than N hours (default: 24).

### Full Pipeline

```bash
wpcomposer pipeline
wpcomposer pipeline --skip-discover
wpcomposer pipeline --skip-deploy
wpcomposer pipeline --discover-source=config
```

Runs discover → update → build → deploy sequentially, stopping on failure. After a successful deploy, automatically cleans up old local builds and stale R2 releases (keeps live + 5 most recent + 24h grace window). Cleanup is best-effort — failures are logged as warnings but do not fail the pipeline.

### Aggregate Installs

```bash
wpcomposer aggregate-installs
```

Recomputes `wp_composer_installs_total`, `wp_composer_installs_30d`, and `last_installed_at` on all packages.

### Cleanup Sessions

```bash
wpcomposer cleanup-sessions
```

Deletes expired rows from the `sessions` table.

## Scheduled Tasks

Configure via systemd timers or cron:

| Command | Schedule | Notes |
|---------|----------|-------|
| `wpcomposer pipeline` | Every 5 minutes | Main data refresh cycle |
| `wpcomposer aggregate-installs` | Hourly | Telemetry counter rollups |
| `wpcomposer cleanup-sessions` | Daily | Expired session cleanup |
| `wpcomposer generate-og` | Daily | Regenerate OG images where install counts changed |

Alternatively, run `wpcomposer serve --with-scheduler` to handle scheduling in-process.

## Global Flags

All commands accept:

- `--config <path>` — optional config file path
- `--db <path>` — database path (default `./storage/wpcomposer.db`)
- `--log-level info|debug|warn|error` — log verbosity

## Key Environment Variables

| Variable | Purpose |
|----------|---------|
| `APP_URL` | Used in `notify-batch` URL in `packages.json` |
| `DB_PATH` | SQLite database file path |
| `WP_COMPOSER_DEPLOY_R2` | Enable R2 sync on deploy |
| `R2_ACCESS_KEY_ID` | R2 API credentials |
| `R2_SECRET_ACCESS_KEY` | R2 API credentials |
| `R2_BUCKET` | R2 bucket name |
| `R2_ENDPOINT` | R2 S3-compatible endpoint URL |
| `LITESTREAM_BUCKET` | R2 bucket for Litestream DB backup |

## Admin Bootstrap

### Create initial admin user

```bash
echo 'secure-password' | wpcomposer admin create --email admin@example.com --name "Admin" --password-stdin
```

`--password-stdin` is required. Password must be piped via stdin to avoid shell history exposure.

### Promote existing user to admin

```bash
wpcomposer admin promote --email user@example.com
```

### Reset admin password

```bash
echo 'new-password' | wpcomposer admin reset-password --email admin@example.com --password-stdin
```

## Admin Access Model

Admin access is protected by in-app authentication (email/password) and admin authorization for all `/admin/*` routes.

## Server Provisioning

Ansible playbooks handle server setup and application deployment. Playbooks are in `deploy/ansible/`.

### Setup

```bash
cd deploy/ansible
source .venv/bin/activate
```

### Provision (full server setup)

```bash
ansible-playbook provision.yml
```

Provisions: Go binary, SQLite, Caddy (reverse proxy + TLS), systemd service, systemd timers.

### Deploy (code only)

```bash
ansible-playbook deploy.yml
```

### Inventory

The production inventory defines the target server. It is gitignored.

```bash
cp inventory/hosts/production.example.yml inventory/hosts/production.yml
```

Edit `production.yml` and fill in `ansible_host` (server IP or hostname) and `ansible_user`.

### Ansible Vault

Secrets used by Ansible are stored in an encrypted vault file. The vault password file (`.vault_pass`) and decrypted vault (`vault.yml`) are gitignored.

All `ansible-vault` commands require the venv:

```bash
cd deploy/ansible
source .venv/bin/activate
```

**Decrypt** (to view or edit as plain YAML):

```bash
ansible-vault decrypt group_vars/production/vault.yml
```

**Encrypt** (after editing):

```bash
ansible-vault encrypt group_vars/production/vault.yml
```

**Edit in-place** (decrypts, opens in `$EDITOR`, re-encrypts on save):

```bash
ansible-vault edit group_vars/production/vault.yml
```

**View** (prints decrypted contents without modifying the file):

```bash
ansible-vault view group_vars/production/vault.yml
```

See `vault.example.yml` for the expected keys.

### GitHub Secrets

The deploy workflow materializes secrets at runtime. These are stored as environment secrets under the `production` environment:

| Secret | Source |
|--------|--------|
| `ANSIBLE_VAULT_PASSWORD` | Contents of `.vault_pass` |
| `PROD_INVENTORY_YML_B64` | Base64-encoded `inventory/hosts/production.yml` (contains server host/IP) |
| `PROD_SSH_PRIVATE_KEY` | SSH private key for the deploy user |
| `PROD_VAULT_YML_B64` | Base64-encoded encrypted `group_vars/production/vault.yml` |

**Updating secrets after a vault change:**

```bash
# Re-encrypt vault first if decrypted
ansible-vault encrypt group_vars/production/vault.yml

# Update the base64-encoded vault secret
base64 < group_vars/production/vault.yml | gh secret set PROD_VAULT_YML_B64 --env production
```

**Updating other secrets:**

```bash
# Vault password
gh secret set ANSIBLE_VAULT_PASSWORD --env production < .vault_pass

# Inventory (e.g. after server IP change)
base64 < inventory/hosts/production.yml | gh secret set PROD_INVENTORY_YML_B64 --env production

# SSH key
gh secret set PROD_SSH_PRIVATE_KEY --env production < /path/to/private/key
```

## Litestream (SQLite Backup to R2)

[Litestream](https://litestream.io/) continuously replicates the SQLite database to R2 by streaming WAL changes. In production it wraps `wpcomposer serve` as a sidecar process — if the child dies, Litestream exits and systemd restarts both.

### How it works

The systemd service runs:

```
litestream replicate -config /srv/wp-composer/shared/litestream.yml -exec "wpcomposer serve ..."
```

WAL segments are uploaded to R2 continuously. A full snapshot is taken every 24 hours (Litestream default).

### Restore locally

Pull a production snapshot to bootstrap your local database:

```bash
make db-restore
```

Or directly:

```bash
wpcomposer db restore --force
```

Requires `litestream` in PATH (`brew install litestream` on macOS) and the following env vars set:
- `LITESTREAM_BUCKET`
- `R2_ENDPOINT`
- `R2_ACCESS_KEY_ID`
- `R2_SECRET_ACCESS_KEY`

The `--output`/`-o` flag overrides the restore path. The `--force` flag is required if the target DB already exists. Migrations run automatically after restore.

### Check snapshots

```bash
litestream snapshots -config litestream.yml
```

## SQLite Operations

### Backup

SQLite in WAL mode supports safe file-level backups:

```bash
sqlite3 /path/to/wpcomposer.db ".backup '/path/to/backup.db'"
```

Or use filesystem snapshots. The WAL file (`wpcomposer.db-wal`) must be included in any backup.

### Runtime Settings

Applied automatically on startup:

```sql
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA foreign_keys=ON;
PRAGMA busy_timeout=5000;
```

## Common Recovery Steps

1. **Build fails integrity validation:**
   - Re-run with `--force`, inspect build output and manifest.

2. **Bad promotion:**
   - Use `wpcomposer deploy --rollback` or rollback to an explicit build ID.

3. **R2 sync failed mid-upload:**
   - Local symlink was not updated, so local state is consistent.
   - Re-run `wpcomposer deploy --to-r2` to retry. Uploads to the release prefix are idempotent; the root `packages.json` pointer only updates after all release files land.
   - To clean up stale releases: `wpcomposer deploy --cleanup --r2-cleanup`.

4. **Telemetry counters stale:**
   - Run `wpcomposer aggregate-installs` manually.

5. **Expired sessions accumulating:**
   - Run `wpcomposer cleanup-sessions` manually or verify the timer/cron is active.

6. **Database locked errors:**
   - Verify WAL mode is active: `sqlite3 wpcomposer.db "PRAGMA journal_mode;"`
   - Check `busy_timeout` is set.
   - Ensure only one `wpcomposer serve` process is running.
