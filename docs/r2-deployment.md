# R2 Deployment

WP Composer deploys built repository artifacts to Cloudflare R2 for serving via CDN. Builds are generated locally (fast filesystem I/O), then synced to R2 on deploy.

## Deploy Model

Only `p2/` metadata files and `packages.json` are stored on R2. Mutable `p2/` files are overwritten in place when packages change. `packages.json` is uploaded last as the final step.

```
packages.json                                        ← root index (mutable)
p2/wp-plugin/akismet.json                            ← overwritten when changed
p2/wp-theme/astra.json                               ← overwritten when changed
```

The deploy diffs the current build against the previous build locally. Mutable `p2/` files are byte-compared and only uploaded if changed. This reduces R2 operations to only the number of changed packages per build.

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

**R2 > your bucket > Settings > Custom Domains > Connect Domain**. This gives you a URL like `https://repo.wp-packages.org` backed by Cloudflare's CDN.

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
2. Uploads `p2/` files in parallel — skips unchanged files (byte-compared against previous build). Each upload retries up to 3 times with exponential backoff.
3. Uploads `packages.json` last.
4. Promotes the local build symlink (for rollback capability).

If R2 sync fails, the local symlink is **not** updated — the previous build remains promoted.

Files are uploaded with `Content-Type: application/json`.

## CDN Cache Headers

When using a Cloudflare custom domain on the R2 bucket, cache behavior is controlled by the `Cache-Control` headers set during upload:

| Path pattern | Cache-Control | Rationale |
|---|---|---|
| `packages.json` | `max-age=300` | Root index, mutable |
| `p2/*.json` | `max-age=300` | Mutable, overwritten on package changes |

## URL Requirements

The generated `packages.json` contains:

- `metadata-url`: `/p2/%package%.json`
- `notify-batch`: absolute URL pointing to the **app domain** (not R2, not rewritten)
- `available-package-patterns`: `["wp-plugin/*", "wp-theme/*"]`

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
aws s3 ls s3://wp-composer-repo/p2/ --profile r2 --endpoint-url https://<account-id>.r2.cloudflarestorage.com
```

### Cleanup legacy R2 objects

After dropping Composer v1 support, the following R2 prefixes are orphaned and can be manually deleted:

- `p/` — content-addressed v1 package files and provider group files
- `releases/` — per-release snapshots from the old deploy model

Use the AWS CLI to delete these when ready:

```bash
aws s3 rm s3://wp-composer-repo/p/ --recursive --profile r2 --endpoint-url https://<account-id>.r2.cloudflarestorage.com
aws s3 rm s3://wp-composer-repo/releases/ --recursive --profile r2 --endpoint-url https://<account-id>.r2.cloudflarestorage.com
```

Local build cleanup is handled by `wpcomposer deploy --cleanup`.

## Rollback

Rollback deploys the target build to R2 — it diffs the target build's `p2/` files against the currently deployed build and uploads only changed files:

```bash
wpcomposer deploy --rollback --to-r2
wpcomposer deploy --rollback=20260313-130000 --to-r2
```

Rollback takes roughly the same time as a normal deploy (proportional to the number of changed files).

## Local-Only Mode

When `WP_COMPOSER_DEPLOY_R2` is unset or `false`, deploy only updates the local `current` symlink. Use this for development or when serving directly from the local filesystem.

## Monitoring

After deploy, verify the bucket:

```bash
# Check root packages.json has metadata-url
curl -s https://repo.wp-packages.org/packages.json | jq '.["metadata-url"]'

# Check a specific package
curl -s https://repo.wp-packages.org/p2/wp-plugin/akismet.json | head -c 200
```
