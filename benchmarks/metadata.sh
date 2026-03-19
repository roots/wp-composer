#!/usr/bin/env bash
#
# Benchmark: Repository metadata — sizes, TTFB, transfer times
#
# Compares packages.json, provider files, and p2 metadata endpoints
# between WP Packages and WPackagist.
#
# Usage: ./benchmarks/metadata.sh [--runs N]
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
RESULTS_DIR="${SCRIPT_DIR}/results"
mkdir -p "$RESULTS_DIR"

RUNS=5
WPC_BASE="https://repo.wp-packages.org"
WPKG_BASE="https://wpackagist.org"

while [[ $# -gt 0 ]]; do
  case $1 in
    --runs) RUNS="$2"; shift 2 ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

# ── Helpers ───────────────────────────────────────────────────────

measure_url() {
  local label="$1"
  local url="$2"

  local ttfb_sum=0
  local total_sum=0
  local size=0
  local ttfb_min=999 ttfb_max=0 total_min=999 total_max=0
  local ok=true

  for i in $(seq 1 "$RUNS"); do
    local result
    result=$(curl -so /dev/null -w '%{time_starttransfer} %{time_total} %{size_download} %{http_code}' "$url" 2>/dev/null) || true

    local ttfb total dl_size http_code
    ttfb=$(echo "$result" | awk '{printf "%.3f", $1}')
    total=$(echo "$result" | awk '{printf "%.3f", $2}')
    dl_size=$(echo "$result" | awk '{print $3}')
    http_code=$(echo "$result" | awk '{print $4}')

    if [[ "$http_code" != "200" ]]; then
      ok=false
      break
    fi

    size="$dl_size"
    ttfb_sum=$(echo "$ttfb_sum + $ttfb" | bc)
    total_sum=$(echo "$total_sum + $total" | bc)

    ttfb_min=$(echo "$ttfb $ttfb_min" | awk '{print ($1 < $2) ? $1 : $2}')
    ttfb_max=$(echo "$ttfb $ttfb_max" | awk '{print ($1 > $2) ? $1 : $2}')
    total_min=$(echo "$total $total_min" | awk '{print ($1 < $2) ? $1 : $2}')
    total_max=$(echo "$total $total_max" | awk '{print ($1 > $2) ? $1 : $2}')
  done

  if [[ "$ok" == "false" ]]; then
    printf "  %-45s  FAILED (HTTP %s)\n" "$label" "$http_code"
    echo "${label},${url},FAIL,0,0,0,0,0,0,0" >> "${RESULTS_DIR}/metadata.csv"
    return
  fi

  local ttfb_avg total_avg
  ttfb_avg=$(echo "$ttfb_sum / $RUNS" | bc -l | xargs printf "%.3f")
  total_avg=$(echo "$total_sum / $RUNS" | bc -l | xargs printf "%.3f")

  local size_human
  if [[ "$size" -gt 1048576 ]]; then
    size_human="$(echo "$size / 1048576" | bc -l | xargs printf "%.1f")MB"
  elif [[ "$size" -gt 1024 ]]; then
    size_human="$(echo "$size / 1024" | bc -l | xargs printf "%.1f")KB"
  else
    size_human="${size}B"
  fi

  printf "  %-45s  %8s  TTFB %ss (min %s, max %s)  Total %ss\n" \
    "$label" "$size_human" "$ttfb_avg" "$ttfb_min" "$ttfb_max" "$total_avg"

  echo "${label},${url},${size},${ttfb_avg},${ttfb_min},${ttfb_max},${total_avg},${total_min},${total_max}" >> "${RESULTS_DIR}/metadata.csv"
}

# ── Resolve p2 URLs ──────────────────────────────────────────────

# Fetch packages.json to find the actual p2 metadata-url template
resolve_p2_url() {
  local base="$1"
  local package="$2"
  local pj
  pj=$(curl -sf "${base}/packages.json" 2>/dev/null) || return 1

  local metadata_url
  metadata_url=$(echo "$pj" | jq -r '.["metadata-url"] // empty' 2>/dev/null)

  if [[ -n "$metadata_url" ]]; then
    local path="${metadata_url/\%package\%/$package}"
    # Handle relative vs absolute
    if [[ "$path" == http* ]]; then
      echo "$path"
    else
      echo "${base}${path}"
    fi
  fi
}

# ── Main ──────────────────────────────────────────────────────────

echo "=== Metadata Benchmark ==="
echo "Runs per URL: ${RUNS}"
echo ""

echo "label,url,size_bytes,ttfb_avg,ttfb_min,ttfb_max,total_avg,total_min,total_max" > "${RESULTS_DIR}/metadata.csv"

# Root packages.json
echo "── packages.json ──"
measure_url "wp-packages/packages.json" "${WPC_BASE}/packages.json"
measure_url "wpackagist/packages.json" "${WPKG_BASE}/packages.json"

# p2 metadata for specific packages
SAMPLE_PLUGINS=(akismet wordpress-seo woocommerce contact-form-7 elementor)

echo ""
echo "── p2 metadata (per-package) ──"

for plugin in "${SAMPLE_PLUGINS[@]}"; do
  wpc_p2=$(resolve_p2_url "$WPC_BASE" "wp-plugin/${plugin}" 2>/dev/null || echo "")
  wpkg_p2=$(resolve_p2_url "$WPKG_BASE" "wpackagist-plugin/${plugin}" 2>/dev/null || echo "")

  if [[ -n "$wpc_p2" ]]; then
    measure_url "wp-packages/p2/${plugin}" "$wpc_p2"
  else
    printf "  %-45s  No p2 metadata-url\n" "wp-packages/p2/${plugin}"
  fi

  if [[ -n "$wpkg_p2" ]]; then
    measure_url "wpackagist/p2/${plugin}" "$wpkg_p2"
  else
    printf "  %-45s  No p2 metadata-url\n" "wpackagist/p2/${plugin}"
  fi
done

# Cache headers comparison
echo ""
echo "── Cache headers ──"

echo "  wp-packages/packages.json:"
curl -sI "${WPC_BASE}/packages.json" 2>/dev/null | grep -iE '^(cache-control|cdn-cache|cf-cache|x-cache|age):' | sed 's/^/    /' || echo "    (none)"

echo "  wpackagist/packages.json:"
curl -sI "${WPKG_BASE}/packages.json" 2>/dev/null | grep -iE '^(cache-control|cdn-cache|cf-cache|x-cache|age):' | sed 's/^/    /' || echo "    (none)"

echo ""
echo "Raw CSV: ${RESULTS_DIR}/metadata.csv"
