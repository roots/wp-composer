#!/usr/bin/env bash
#
# End-to-end smoke test for WP Composer
#
# Boots the app, builds repository artifacts, then uses Composer to
# install real WordPress plugins/themes from the local repository.
# Verifies telemetry events are recorded via notify-batch.
#
# Prerequisites: go, composer, curl, sqlite3, jq
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
TEST_DIR=$(mktemp -d)
APP_PORT=19876
APP_URL="http://localhost:${APP_PORT}"
DB_PATH="${TEST_DIR}/wpcomposer.db"
BINARY="${ROOT_DIR}/wpcomposer"
PASSED=0
FAILED=0

cleanup() {
  if [ -n "${APP_PID:-}" ] && kill -0 "$APP_PID" 2>/dev/null; then
    kill "$APP_PID" 2>/dev/null || true
    wait "$APP_PID" 2>/dev/null || true
  fi
  rm -rf "$TEST_DIR"
  rm -rf "${ROOT_DIR}/storage"

  echo ""
  echo "================================"
  echo "Results: ${PASSED} passed, ${FAILED} failed"
  echo "================================"

  if [ "$FAILED" -gt 0 ]; then
    exit 1
  fi
}
trap cleanup EXIT

pass() {
  echo "  ✓ $1"
  PASSED=$((PASSED + 1))
}

fail() {
  echo "  ✗ $1"
  FAILED=$((FAILED + 1))
}

assert_eq() {
  if [ "$1" = "$2" ]; then
    pass "$3"
  else
    fail "$3 (expected '$2', got '$1')"
  fi
}

assert_contains() {
  if echo "$1" | grep -q "$2"; then
    pass "$3"
  else
    fail "$3 (expected to contain '$2')"
  fi
}

assert_gt() {
  if [ "$1" -gt "$2" ]; then
    pass "$3"
  else
    fail "$3 (expected > $2, got $1)"
  fi
}

echo "=== Building binary ==="
cd "$ROOT_DIR"
go build -o "$BINARY" ./cmd/wpcomposer

echo ""
echo "=== Setting up database and repository ==="
"$BINARY" migrate --db "$DB_PATH" > /dev/null 2>&1
"$BINARY" discover --source=config --type=plugin --limit=5 --db "$DB_PATH" > /dev/null 2>&1
"$BINARY" discover --source=config --type=theme --limit=2 --db "$DB_PATH" > /dev/null 2>&1
"$BINARY" update --force --db "$DB_PATH" > /dev/null 2>&1
APP_URL="$APP_URL" "$BINARY" build --db "$DB_PATH" > /dev/null 2>&1
"$BINARY" deploy --db "$DB_PATH" > /dev/null 2>&1

echo ""
echo "=== Starting server ==="
"$BINARY" serve --db "$DB_PATH" --addr ":${APP_PORT}" > /dev/null 2>&1 &
APP_PID=$!
sleep 2

if ! kill -0 "$APP_PID" 2>/dev/null; then
  echo "Server failed to start"
  exit 1
fi

# ─── Health check ───────────────────────────────────────────────────

echo ""
echo "--- Health & endpoints ---"

HEALTH=$(curl -sf "${APP_URL}/health")
assert_contains "$HEALTH" '"status":"ok"' "GET /health returns ok"

INDEX_STATUS=$(curl -sf -o /dev/null -w "%{http_code}" "${APP_URL}/")
assert_eq "$INDEX_STATUS" "200" "GET / returns 200"

# ─── packages.json validation ──────────────────────────────────────

echo ""
echo "--- packages.json ---"

PACKAGES_JSON=$(curl -sf "${APP_URL}/packages.json")
NOTIFY_BATCH=$(echo "$PACKAGES_JSON" | python3 -c "import sys,json; print(json.load(sys.stdin).get('notify-batch',''))")
assert_eq "$NOTIFY_BATCH" "${APP_URL}/downloads" "notify-batch is absolute URL"

PROVIDERS_URL=$(echo "$PACKAGES_JSON" | python3 -c "import sys,json; print(json.load(sys.stdin).get('providers-url',''))")
assert_eq "$PROVIDERS_URL" "/p/%package%\$%hash%.json" "providers-url is set"

METADATA_URL=$(echo "$PACKAGES_JSON" | python3 -c "import sys,json; print(json.load(sys.stdin).get('metadata-url',''))")
assert_eq "$METADATA_URL" "/p2/%package%.json" "metadata-url is set"

PROVIDER_COUNT=$(echo "$PACKAGES_JSON" | python3 -c "import sys,json; print(len(json.load(sys.stdin).get('provider-includes',{})))")
assert_gt "$PROVIDER_COUNT" "0" "provider-includes has entries"

# ─── Composer install ──────────────────────────────────────────────

echo ""
echo "--- Composer install ---"

COMPOSER_DIR="${TEST_DIR}/composer-project"
mkdir -p "$COMPOSER_DIR"

cat > "${COMPOSER_DIR}/composer.json" << COMPOSERJSON
{
  "repositories": [
    {
      "type": "composer",
      "url": "${APP_URL}",
      "only": ["wp-plugin/*", "wp-theme/*"]
    }
  ],
  "require": {
    "composer/installers": "^2.2",
    "wp-plugin/akismet": "*",
    "wp-plugin/classic-editor": "*",
    "wp-theme/astra": "*"
  },
  "config": {
    "secure-http": false,
    "allow-plugins": {
      "composer/installers": true
    }
  },
  "minimum-stability": "dev",
  "prefer-stable": true
}
COMPOSERJSON

cd "$COMPOSER_DIR"
COMPOSER_OUTPUT=$(composer install --no-interaction --no-progress 2>&1) || true

# Check packages were installed (composer/installers puts them in vendor/ by default
# since we don't have the web/app directory structure)
if echo "$COMPOSER_OUTPUT" | grep -q "Installing wp-plugin/akismet"; then
  pass "wp-plugin/akismet installed"
else
  fail "wp-plugin/akismet not installed"
  echo "  Composer output:"
  echo "$COMPOSER_OUTPUT" | head -20 | sed 's/^/    /'
fi

if echo "$COMPOSER_OUTPUT" | grep -q "Installing wp-plugin/classic-editor"; then
  pass "wp-plugin/classic-editor installed"
else
  fail "wp-plugin/classic-editor not installed"
fi

if echo "$COMPOSER_OUTPUT" | grep -q "Installing wp-theme/astra"; then
  pass "wp-theme/astra installed"
else
  fail "wp-theme/astra not installed"
fi

# ─── Telemetry verification ────────────────────────────────────────

echo ""
echo "--- Telemetry ---"

# Give the notify-batch POST a moment to complete
sleep 2

EVENT_COUNT=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM install_events;")
assert_gt "$EVENT_COUNT" "0" "install_events has records after composer install"

# Check specific packages got events
AKISMET_EVENTS=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM install_events ie JOIN packages p ON p.id = ie.package_id WHERE p.name = 'akismet';")
assert_gt "$AKISMET_EVENTS" "0" "akismet has install events"

# Aggregate and verify counters
cd "$ROOT_DIR"
"$BINARY" aggregate-installs --db "$DB_PATH" > /dev/null 2>&1

AKISMET_TOTAL=$(sqlite3 "$DB_PATH" "SELECT wp_composer_installs_total FROM packages WHERE name = 'akismet';")
assert_gt "$AKISMET_TOTAL" "0" "akismet wp_composer_installs_total > 0 after aggregation"

AKISMET_30D=$(sqlite3 "$DB_PATH" "SELECT wp_composer_installs_30d FROM packages WHERE name = 'akismet';")
assert_gt "$AKISMET_30D" "0" "akismet wp_composer_installs_30d > 0 after aggregation"

AKISMET_LAST=$(sqlite3 "$DB_PATH" "SELECT last_installed_at FROM packages WHERE name = 'akismet';")
if [ -n "$AKISMET_LAST" ] && [ "$AKISMET_LAST" != "" ]; then
  pass "akismet last_installed_at is set"
else
  fail "akismet last_installed_at is not set"
fi

# ─── Admin access ──────────────────────────────────────────────────

echo ""
echo "--- Admin access ---"

# Login page should be accessible
LOGIN_STATUS=$(curl -sf -o /dev/null -w "%{http_code}" "${APP_URL}/admin/login")
assert_eq "$LOGIN_STATUS" "200" "GET /admin/login returns 200"

# Admin dashboard should redirect to login (no session)
ADMIN_STATUS=$(curl -s -o /dev/null -w "%{http_code}" "${APP_URL}/admin/")
assert_eq "$ADMIN_STATUS" "303" "GET /admin/ without auth redirects (303)"

# ─── Dedupe verification ──────────────────────────────────────────

echo ""
echo "--- Dedupe ---"

# POST the same install twice — second should be deduplicated
curl -sf -X POST "${APP_URL}/downloads" \
  -H 'Content-Type: application/json' \
  -d '{"downloads":[{"name":"wp-plugin/akismet","version":"5.6"}]}' > /dev/null
sleep 1
AFTER_FIRST=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM install_events;")

curl -sf -X POST "${APP_URL}/downloads" \
  -H 'Content-Type: application/json' \
  -d '{"downloads":[{"name":"wp-plugin/akismet","version":"5.6"}]}' > /dev/null
sleep 1
AFTER_SECOND=$(sqlite3 "$DB_PATH" "SELECT COUNT(*) FROM install_events;")

assert_eq "$AFTER_SECOND" "$AFTER_FIRST" "duplicate notify-batch is deduplicated"

# ─── Version pinning ─────────────────────────────────────────────

echo ""
echo "--- Version pinning ---"

PINNED_DIR="${TEST_DIR}/composer-pinned"
mkdir -p "$PINNED_DIR"

cat > "${PINNED_DIR}/composer.json" << COMPOSERJSON
{
  "repositories": [
    {
      "type": "composer",
      "url": "${APP_URL}",
      "only": ["wp-plugin/*", "wp-theme/*"]
    }
  ],
  "require": {
    "composer/installers": "^2.2",
    "wp-plugin/akismet": "5.3.3",
    "wp-plugin/classic-editor": "1.6.6"
  },
  "config": {
    "secure-http": false,
    "allow-plugins": {
      "composer/installers": true
    }
  },
  "minimum-stability": "dev",
  "prefer-stable": true
}
COMPOSERJSON

cd "$PINNED_DIR"
PINNED_OUTPUT=$(composer install --no-interaction --no-progress 2>&1) || true

if echo "$PINNED_OUTPUT" | grep -q "Locking wp-plugin/akismet (5.3.3)"; then
  pass "wp-plugin/akismet pinned to 5.3.3"
else
  fail "wp-plugin/akismet version pin failed"
  echo "  Output:" && echo "$PINNED_OUTPUT" | grep -i "akismet\|error\|lock" | head -5 | sed 's/^/    /'
fi

if echo "$PINNED_OUTPUT" | grep -q "Locking wp-plugin/classic-editor (1.6.6)"; then
  pass "wp-plugin/classic-editor pinned to 1.6.6"
else
  fail "wp-plugin/classic-editor version pin failed"
  echo "  Output:" && echo "$PINNED_OUTPUT" | grep -i "classic\|error\|lock" | head -5 | sed 's/^/    /'
fi

# ─── Package detail pages ────────────────────────────────────────

echo ""
echo "--- Package pages ---"

DETAIL_STATUS=$(curl -sf -o /dev/null -w "%{http_code}" "${APP_URL}/packages/wp-plugin/akismet")
assert_eq "$DETAIL_STATUS" "200" "GET /packages/wp-plugin/akismet returns 200"

DETAIL_BODY=$(curl -sf "${APP_URL}/packages/wp-plugin/akismet")
assert_contains "$DETAIL_BODY" "composer require wp-plugin/akismet" "detail page has install command"
assert_contains "$DETAIL_BODY" "Package Info" "detail page has Package Info sidebar"

MISSING_STATUS=$(curl -s -o /dev/null -w "%{http_code}" "${APP_URL}/packages/wp-plugin/nonexistent-plugin-xyz")
assert_eq "$MISSING_STATUS" "404" "GET /packages/wp-plugin/nonexistent returns 404"

# ─── Admin auth flow ─────────────────────────────────────────────

echo ""
echo "--- Admin auth flow ---"

cd "$ROOT_DIR"

# Create test admin user
echo 'smoke-test-pass' | "$BINARY" admin create \
  --email smoke@test.com --name "Smoke Test" --password-stdin \
  --db "$DB_PATH" > /dev/null 2>&1

# Login with correct credentials
LOGIN_RESPONSE=$(curl -s -D - -o /dev/null -X POST "${APP_URL}/admin/login" \
  -d "email=smoke@test.com&password=smoke-test-pass" \
  -H "Content-Type: application/x-www-form-urlencoded" 2>&1)

if echo "$LOGIN_RESPONSE" | grep -q "Set-Cookie: session="; then
  pass "login sets session cookie"
  SESSION_COOKIE=$(echo "$LOGIN_RESPONSE" | grep "Set-Cookie: session=" | sed 's/.*session=//;s/;.*//')
else
  fail "login did not set session cookie"
  SESSION_COOKIE=""
fi

if echo "$LOGIN_RESPONSE" | grep -q "Location: /admin"; then
  pass "login redirects to /admin"
else
  fail "login did not redirect to /admin"
fi

# Access admin with session cookie
if [ -n "$SESSION_COOKIE" ]; then
  AUTHED_STATUS=$(curl -sf -o /dev/null -w "%{http_code}" \
    -b "session=${SESSION_COOKIE}" "${APP_URL}/admin/")
  assert_eq "$AUTHED_STATUS" "200" "GET /admin/ with session returns 200"

  AUTHED_PACKAGES=$(curl -sf -o /dev/null -w "%{http_code}" \
    -b "session=${SESSION_COOKIE}" "${APP_URL}/admin/packages")
  assert_eq "$AUTHED_PACKAGES" "200" "GET /admin/packages with session returns 200"

  AUTHED_BUILDS=$(curl -sf -o /dev/null -w "%{http_code}" \
    -b "session=${SESSION_COOKIE}" "${APP_URL}/admin/builds")
  assert_eq "$AUTHED_BUILDS" "200" "GET /admin/builds with session returns 200"
fi

# Login with wrong password
BAD_LOGIN=$(curl -s -o /dev/null -w "%{http_code}" -X POST "${APP_URL}/admin/login" \
  -d "email=smoke@test.com&password=wrong" \
  -H "Content-Type: application/x-www-form-urlencoded")
assert_eq "$BAD_LOGIN" "303" "login with wrong password redirects (303)"

# Logout
if [ -n "$SESSION_COOKIE" ]; then
  LOGOUT_RESPONSE=$(curl -s -D - -o /dev/null -X POST "${APP_URL}/admin/logout" \
    -b "session=${SESSION_COOKIE}" 2>&1)
  if echo "$LOGOUT_RESPONSE" | grep -q "session=;"; then
    pass "logout clears session cookie"
  elif echo "$LOGOUT_RESPONSE" | grep -q "Max-Age=-1\|Max-Age=0"; then
    pass "logout clears session cookie"
  else
    # Check if session is invalidated by trying to access admin
    POST_LOGOUT=$(curl -s -o /dev/null -w "%{http_code}" \
      -b "session=${SESSION_COOKIE}" "${APP_URL}/admin/")
    assert_eq "$POST_LOGOUT" "303" "admin inaccessible after logout"
  fi
fi

# ─── Golden fixture: p2 metadata structure ───────────────────────

echo ""
echo "--- Golden fixtures ---"

P2_AKISMET=$(curl -sf "${APP_URL}/p2/wp-plugin/akismet.json")

# Verify structure has packages key with wp-plugin/akismet
HAS_PACKAGES=$(echo "$P2_AKISMET" | python3 -c "
import sys, json
d = json.load(sys.stdin)
pkgs = d.get('packages', {})
akismet = pkgs.get('wp-plugin/akismet', {})
print(len(akismet))
" 2>/dev/null || echo "0")
assert_gt "$HAS_PACKAGES" "0" "p2/wp-plugin/akismet.json has version entries"

# Verify a version entry has required Composer fields
HAS_FIELDS=$(echo "$P2_AKISMET" | python3 -c "
import sys, json
d = json.load(sys.stdin)
versions = d.get('packages', {}).get('wp-plugin/akismet', {})
v = next(iter(versions.values()), {})
fields = ['name', 'version', 'type', 'dist', 'source', 'require']
print(sum(1 for f in fields if f in v))
" 2>/dev/null || echo "0")
assert_eq "$HAS_FIELDS" "6" "version entry has all required Composer fields (name, version, type, dist, source, require)"

# Verify dist URL points to wordpress.org
DIST_URL=$(echo "$P2_AKISMET" | python3 -c "
import sys, json
d = json.load(sys.stdin)
versions = d.get('packages', {}).get('wp-plugin/akismet', {})
v = next(iter(versions.values()), {})
print(v.get('dist', {}).get('url', ''))
" 2>/dev/null || echo "")
assert_contains "$DIST_URL" "downloads.wordpress.org" "dist URL points to wordpress.org"

# Verify type is wordpress-plugin
PKG_TYPE=$(echo "$P2_AKISMET" | python3 -c "
import sys, json
d = json.load(sys.stdin)
versions = d.get('packages', {}).get('wp-plugin/akismet', {})
v = next(iter(versions.values()), {})
print(v.get('type', ''))
" 2>/dev/null || echo "")
assert_eq "$PKG_TYPE" "wordpress-plugin" "package type is wordpress-plugin"

# ─── Build integrity ─────────────────────────────────────────────

echo ""
echo "--- Build integrity ---"

MANIFEST=$(cat storage/repository/current/manifest.json)
ROOT_HASH=$(echo "$MANIFEST" | python3 -c "import sys,json; print(json.load(sys.stdin).get('root_hash',''))")
if [ -n "$ROOT_HASH" ] && [ "$ROOT_HASH" != "" ]; then
  pass "manifest.json has root_hash"
else
  fail "manifest.json missing root_hash"
fi

ARTIFACT_COUNT=$(echo "$MANIFEST" | python3 -c "import sys,json; print(int(json.load(sys.stdin).get('artifact_count',0)))")
assert_gt "$ARTIFACT_COUNT" "0" "manifest reports artifact_count > 0"

# ─── Rollback ────────────────────────────────────────────────────

echo ""
echo "--- Rollback ---"

# Build again to get a second build
sleep 1
APP_URL="$APP_URL" "$BINARY" build --db "$DB_PATH" > /dev/null 2>&1
"$BINARY" deploy --db "$DB_PATH" > /dev/null 2>&1

BUILD_COUNT=$(ls storage/repository/builds/ | wc -l | tr -d ' ')
assert_gt "$BUILD_COUNT" "1" "multiple builds exist"

CURRENT_BEFORE=$(readlink storage/repository/current | xargs basename)
"$BINARY" deploy --rollback --db "$DB_PATH" > /dev/null 2>&1
CURRENT_AFTER=$(readlink storage/repository/current | xargs basename)

if [ "$CURRENT_BEFORE" != "$CURRENT_AFTER" ]; then
  pass "rollback changed current build"
else
  fail "rollback did not change current build"
fi

# Verify rolled-back build still serves valid packages.json
ROLLBACK_PJ=$(curl -sf "${APP_URL}/packages.json" | python3 -c "import sys,json; print(json.load(sys.stdin).get('notify-batch',''))" 2>/dev/null || echo "")
assert_eq "$ROLLBACK_PJ" "${APP_URL}/downloads" "packages.json valid after rollback"

echo ""
echo "=== Smoke test complete ==="
