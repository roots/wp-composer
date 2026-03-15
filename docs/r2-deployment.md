# R2 Deployment

WP Composer deploys built repository artifacts to Cloudflare R2 for serving via CDN. Builds are generated locally (fast filesystem I/O), then synced to R2 on deploy.

## Versioned Deploy Model

Each deploy uploads all files to an immutable release prefix (`releases/<build-id>/`). The only mutable object in the bucket is the root `packages.json`, which acts as an atomic pointer to the current release.

```
packages.json                                        ← atomic pointer (only mutable object)
releases/20260314-150405/packages.json               ← snapshot reference (immutable)
releases/20260314-150405/manifest.json
releases/20260314-150405/p/wp-plugin/akismet$abc.json
releases/20260314-150405/p2/wp-plugin/akismet.json   ← immutable within release
```

The root `packages.json` is rewritten on each deploy so that `metadata-url`, `providers-url`, and `provider-includes` keys point into the new release prefix. This single PUT is the atomic switch — clients either see the old release or the new one, never a mix.

## Prerequisites

1. A Cloudflare account with R2 enabled.
2. An R2 bucket created for the repository (e.g., `wp-composer-repo`).
3. An R2 API token with read/write access to the bucket.
4. AWS CLI v2 installed (for manual operations and debugging).

## R2 Bucket Setup

### Create the bucket

In the Cloudflare dashboard: **R2 > Create bucket**. Pick a name (e.g., `wp-composer-repo`), choose a location hint close to your server.

### Create API credentials

**R2 > Manage R2 API Tokens > Create API Token**:
- Permission: **Object Read & Write**
- Scope: the specific bucket

Save the **Access Key ID** and **Secret Access Key**.

### Connect a custom domain (recommended)

**R2 > your bucket > Settings > Custom Domains > Connect Domain**. This gives you a URL like `https://repo.wp-composer.com` backed by Cloudflare's CDN.

Without a custom domain, R2 provides a `.r2.dev` URL, but it has rate limits and no caching.

## Environment Configuration

```env
# R2 credentials
R2_ACCESS_KEY_ID=your-access-key-id
R2_SECRET_ACCESS_KEY=your-secret-access-key
R2_BUCKET=wp-composer-repo
R2_ENDPOINT=https://<account-id>.r2.cloudflarestorage.com

# Enable R2 deploy
WP_COMPOSER_DEPLOY_R2=true
```

Find your account ID in the Cloudflare dashboard under **R2 > Overview**.

## How Deploy Works

When deploying to R2 (`wpcomposer deploy --to-r2`):

1. Validates the build (packages.json and manifest.json must exist).
2. Uploads all files under `releases/<build-id>/` with appropriate `Cache-Control` headers. Provider and package files upload first; `packages.json` uploads last within the release prefix. Each upload retries up to 3 times with exponential backoff.
3. Rewrites `packages.json` URL templates to point at the new release prefix.
4. Uploads the rewritten `packages.json` as the root — the atomic switch.
5. Promotes the local build symlink (for rollback capability).

If R2 sync fails, the local symlink is **not** updated — the previous build remains promoted.

Files are uploaded with `Content-Type: application/json`.

## CDN Cache Headers

When using a Cloudflare custom domain on the R2 bucket, cache behavior is controlled by the `Cache-Control` headers set during upload:

| Path pattern | Cache-Control | Rationale |
|---|---|---|
| `packages.json` (root) | `max-age=300` | Atomic pointer, only mutable object |
| `releases/*` (everything) | `max-age=31536000, immutable` | Entire release prefix is immutable |

Legacy flat-path cases (backward compat during transition):

| Path pattern | Cache-Control | Rationale |
|---|---|---|
| `manifest.json` | `max-age=300` | Build metadata |
| `p/*$hash.json` | `max-age=31536000, immutable` | Content-addressed, never changes |
| `p2/*.json` | `max-age=300` | No hash in URL, must revalidate |

## URL Requirements

The generated root `packages.json` on R2 contains prefixed URLs pointing into the current release:

- `metadata-url`: `/releases/<build-id>/p2/%package%.json`
- `providers-url`: `/releases/<build-id>/p/%package%$%hash%.json`
- `provider-includes` keys: `releases/<build-id>/p/providers-*$hash.json`
- `notify-batch`: absolute URL pointing to the **app domain** (not R2, not rewritten)

## AWS CLI Setup (Manual Operations)

Configure a named profile for R2:

```bash
aws configure --profile r2
```

Enter:
- **Access Key ID**: your R2 access key
- **Secret Access Key**: your R2 secret key
- **Default region**: `auto`
- **Default output format**: `json`

### Verify access

```bash
aws s3 ls s3://wp-composer-repo/ --profile r2 --endpoint-url https://<account-id>.r2.cloudflarestorage.com
```

### List bucket contents

```bash
aws s3 ls s3://wp-composer-repo/releases/ --profile r2 --endpoint-url https://<account-id>.r2.cloudflarestorage.com
```

### Cleanup stale R2 releases

The `wpcomposer pipeline` command automatically cleans up old R2 releases after each successful deploy, keeping the live release + 5 most recent + releases within a 24-hour grace window. Cleanup is best-effort — failures are logged as warnings but do not fail the pipeline. If cleanup errors persist, run manual cleanup to investigate.

For manual cleanup:

```bash
# Remove R2 releases beyond retention (keeps live + 5 most recent + within grace period)
wpcomposer deploy --cleanup --r2-cleanup

# Shorter grace period (default 24 hours)
wpcomposer deploy --cleanup --r2-cleanup --grace-hours 6
```

`--r2-cleanup` is required — plain `--cleanup` only removes local build directories. The cleanup reads R2 state directly (no local filesystem dependency), identifies release prefixes, and deletes those outside the keep set. It also deletes legacy flat files (anything not under `releases/` except root `packages.json`).

The keep set is: live release (from root `packages.json`) + releases within `--grace-hours` + top `--retain` most recent. The retain count has a hard minimum of 5 — even if `--retain` is set lower, at least 5 recent releases are always preserved.

## Rollback

Rollback re-syncs the target build to R2 under its release prefix and rewrites the root `packages.json` pointer:

```bash
wpcomposer deploy --rollback --to-r2
wpcomposer deploy --rollback=20260313-130000 --to-r2
```

If the target release prefix still exists on R2 from a previous deploy, the upload is idempotent. The key operation is the root `packages.json` rewrite pointing back to the old release.

## Local-Only Mode

When `WP_COMPOSER_DEPLOY_R2` is unset or `false`, deploy only updates the local `current` symlink. Use this for development or when serving directly from the local filesystem.

## Monitoring

After deploy, verify the bucket:

```bash
# Check root packages.json has prefixed URLs
curl -s https://repo.wp-composer.com/packages.json | jq '.["metadata-url"]'

# Check a specific package through the release prefix
curl -s https://repo.wp-composer.com/releases/20260314-150405/p2/wp-plugin/akismet.json | head -c 200
```
