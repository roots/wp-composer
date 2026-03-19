#!/usr/bin/env bash
#
# Capture WordPress.org API fixtures for integration tests.
# Run once to snapshot, commit the results, re-run to refresh.
#
# Usage: ./test/capture-fixtures.sh
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
FIXTURE_DIR="${ROOT_DIR}/internal/wporg/testdata"

PLUGINS=(akismet classic-editor contact-form-7)
THEMES=(astra twentytwentyfive)

mkdir -p "${FIXTURE_DIR}/plugins" "${FIXTURE_DIR}/themes"

echo "Capturing plugin fixtures..."
for slug in "${PLUGINS[@]}"; do
  echo "  → ${slug}"
  curl -sS "https://api.wordpress.org/plugins/info/1.2/?action=plugin_information&request%5Bslug%5D=${slug}&request%5Bfields%5D%5Bversions%5D=true&request%5Bfields%5D%5Bdescription%5D=false&request%5Bfields%5D%5Bsections%5D=false&request%5Bfields%5D%5Bcompatibility%5D=false&request%5Bfields%5D%5Breviews%5D=false&request%5Bfields%5D%5Bbanners%5D=false&request%5Bfields%5D%5Bicons%5D=false&request%5Bfields%5D%5Bdonate_link%5D=false&request%5Bfields%5D%5Bratings%5D=false&request%5Bfields%5D%5Bcontributors%5D=false&request%5Bfields%5D%5Btags%5D=false&request%5Bfields%5D%5Bactive_installs%5D=true&request%5Bfields%5D%5Brequires%5D=true&request%5Bfields%5D%5Btested%5D=true&request%5Bfields%5D%5Brequires_php%5D=true&request%5Bfields%5D%5Bauthor%5D=true&request%5Bfields%5D%5Bshort_description%5D=true&request%5Bfields%5D%5Bhomepage%5D=true&request%5Bfields%5D%5Blast_updated%5D=true&request%5Bfields%5D%5Badded%5D=true&request%5Bfields%5D%5Bdownload_link%5D=true" | python3 -m json.tool > "${FIXTURE_DIR}/plugins/${slug}.json"
done

echo "Capturing theme fixtures..."
for slug in "${THEMES[@]}"; do
  echo "  → ${slug}"
  curl -sS "https://api.wordpress.org/themes/info/1.2/?action=theme_information&request%5Bslug%5D=${slug}&request%5Bfields%5D%5Bversions%5D=true&request%5Bfields%5D%5Bactive_installs%5D=true&request%5Bfields%5D%5Bsections%5D=true&request%5Bfields%5D%5Bauthor%5D=true&request%5Bfields%5D%5Bhomepage%5D=true&request%5Bfields%5D%5Blast_updated%5D=true" | python3 -m json.tool > "${FIXTURE_DIR}/themes/${slug}.json"
done

echo "Done. Fixtures written to ${FIXTURE_DIR}"
