#!/usr/bin/env bash
#
# Benchmark: Composer resolve times — WP Packages vs WPackagist
#
# Measures cold (no cache) and warm (cached) composer update times
# across small/medium/large dependency sets.
#
# Usage: ./benchmarks/resolve.sh [--runs N] [--size small|medium|large|all]
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
RESULTS_DIR="${SCRIPT_DIR}/results"
mkdir -p "$RESULTS_DIR"

RUNS=5
SIZE="all"
WPC_URL="https://repo.wp-packages.org"
WPKG_URL="https://wpackagist.org"

while [[ $# -gt 0 ]]; do
  case $1 in
    --runs) RUNS="$2"; shift 2 ;;
    --size) SIZE="$2"; shift 2 ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

# ── Plugin sets ───────────────────────────────────────────────────

SMALL_PLUGINS=(akismet classic-editor wordpress-seo)
MEDIUM_PLUGINS=(akismet classic-editor wordpress-seo woocommerce contact-form-7 elementor wordfence updraftplus wp-super-cache redirection)
LARGE_PLUGINS=(akismet classic-editor wordpress-seo woocommerce contact-form-7 elementor wordfence updraftplus wp-super-cache redirection advanced-custom-fields all-in-one-seo-pack jetpack duplicate-post regenerate-thumbnails litespeed-cache really-simple-ssl query-monitor wp-mail-smtp google-site-kit)

# ── Helpers ───────────────────────────────────────────────────────

generate_composer_json() {
  local repo_url="$1"
  local prefix="$2"
  shift 2
  local plugins=("$@")

  local require=""
  for p in "${plugins[@]}"; do
    require+="    \"${prefix}/${p}\": \"*\","$'\n'
  done
  # Remove trailing comma+newline, add newline
  require="${require%,$'\n'}"

  cat <<EOF
{
  "repositories": [
    {
      "type": "composer",
      "url": "${repo_url}"
    }
  ],
  "require": {
    "composer/installers": "^2.2",
${require}
  },
  "config": {
    "allow-plugins": {
      "composer/installers": true
    }
  },
  "minimum-stability": "dev",
  "prefer-stable": true
}
EOF
}

run_benchmark() {
  local label="$1"
  local repo_url="$2"
  local prefix="$3"
  shift 3
  local plugins=("$@")

  local work_dir
  work_dir=$(mktemp -d)
  local cache_dir
  cache_dir=$(mktemp -d)

  generate_composer_json "$repo_url" "$prefix" "${plugins[@]}" > "${work_dir}/composer.json"

  echo ""
  echo "  ${label} (${#plugins[@]} plugins)"
  echo "  ─────────────────────────────────"

  # Cold runs (fresh cache each time)
  local cold_times=()
  for i in $(seq 1 "$RUNS"); do
    local run_cache
    run_cache=$(mktemp -d)
    local run_dir
    run_dir=$(mktemp -d)
    cp "${work_dir}/composer.json" "${run_dir}/composer.json"

    local start end elapsed
    start=$(perl -MTime::HiRes=time -e 'printf "%.3f\n", time')
    COMPOSER_HOME="$run_cache" composer update --no-interaction --no-progress --no-scripts --no-autoloader -d "$run_dir" > /dev/null 2>&1 || true
    end=$(perl -MTime::HiRes=time -e 'printf "%.3f\n", time')
    elapsed=$(echo "$end - $start" | bc)
    cold_times+=("$elapsed")

    rm -rf "$run_cache" "$run_dir"
  done

  # Warm runs (shared cache, pre-warmed)
  local warm_dir
  warm_dir=$(mktemp -d)
  cp "${work_dir}/composer.json" "${warm_dir}/composer.json"
  COMPOSER_HOME="$cache_dir" composer update --no-interaction --no-progress --no-scripts --no-autoloader -d "$warm_dir" > /dev/null 2>&1 || true
  rm -rf "$warm_dir"

  local warm_times=()
  for i in $(seq 1 "$RUNS"); do
    local run_dir
    run_dir=$(mktemp -d)
    cp "${work_dir}/composer.json" "${run_dir}/composer.json"

    local start end elapsed
    start=$(perl -MTime::HiRes=time -e 'printf "%.3f\n", time')
    COMPOSER_HOME="$cache_dir" composer update --no-interaction --no-progress --no-scripts --no-autoloader -d "$run_dir" > /dev/null 2>&1 || true
    end=$(perl -MTime::HiRes=time -e 'printf "%.3f\n", time')
    elapsed=$(echo "$end - $start" | bc)
    warm_times+=("$elapsed")

    rm -rf "$run_dir"
  done

  # Calculate stats
  local cold_avg warm_avg
  cold_avg=$(printf '%s\n' "${cold_times[@]}" | awk '{sum+=$1} END {printf "%.3f", sum/NR}')
  warm_avg=$(printf '%s\n' "${warm_times[@]}" | awk '{sum+=$1} END {printf "%.3f", sum/NR}')

  local cold_min cold_max warm_min warm_max
  cold_min=$(printf '%s\n' "${cold_times[@]}" | sort -n | head -1)
  cold_max=$(printf '%s\n' "${cold_times[@]}" | sort -n | tail -1)
  warm_min=$(printf '%s\n' "${warm_times[@]}" | sort -n | head -1)
  warm_max=$(printf '%s\n' "${warm_times[@]}" | sort -n | tail -1)

  printf "  Cold:  avg %ss  (min %ss, max %ss)\n" "$cold_avg" "$cold_min" "$cold_max"
  printf "  Warm:  avg %ss  (min %ss, max %ss)\n" "$warm_avg" "$warm_min" "$warm_max"

  rm -rf "$work_dir" "$cache_dir"

  # Return as CSV line
  echo "${label},${#plugins[@]},cold,${cold_avg},${cold_min},${cold_max}" >> "${RESULTS_DIR}/resolve.csv"
  echo "${label},${#plugins[@]},warm,${warm_avg},${warm_min},${warm_max}" >> "${RESULTS_DIR}/resolve.csv"
}

# ── Main ──────────────────────────────────────────────────────────

echo "=== Composer Resolve Benchmark ==="
echo "Runs per test: ${RUNS}"
echo ""

# CSV header
echo "repo,plugin_count,cache,avg_s,min_s,max_s" > "${RESULTS_DIR}/resolve.csv"

run_size() {
  local size_label="$1"
  shift
  local plugins=("$@")

  echo ""
  echo "── ${size_label} (${#plugins[@]} plugins) ──"

  run_benchmark "wp-packages-${size_label}" "$WPC_URL" "wp-plugin" "${plugins[@]}"
  run_benchmark "wpackagist-${size_label}" "$WPKG_URL" "wpackagist-plugin" "${plugins[@]}"
}

if [[ "$SIZE" == "all" || "$SIZE" == "small" ]]; then
  run_size "small" "${SMALL_PLUGINS[@]}"
fi

if [[ "$SIZE" == "all" || "$SIZE" == "medium" ]]; then
  run_size "medium" "${MEDIUM_PLUGINS[@]}"
fi

if [[ "$SIZE" == "all" || "$SIZE" == "large" ]]; then
  run_size "large" "${LARGE_PLUGINS[@]}"
fi

echo ""
echo "=== Results ==="
echo ""
column -t -s',' "${RESULTS_DIR}/resolve.csv"
echo ""
echo "Raw CSV: ${RESULTS_DIR}/resolve.csv"
