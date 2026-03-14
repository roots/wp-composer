#!/usr/bin/env bash
set -euo pipefail

# Migrate composer.json from WPackagist to WP Composer
# https://wp-composer.com/wp-composer-vs-wpackagist

COMPOSER_FILE="${1:-composer.json}"

if ! command -v jq &>/dev/null; then
  echo "Error: jq is required. Install it with:"
  echo "  brew install jq    # macOS"
  echo "  apt install jq     # Debian/Ubuntu"
  exit 1
fi

if [[ ! -f "$COMPOSER_FILE" ]]; then
  echo "Error: $COMPOSER_FILE not found"
  exit 1
fi

echo "Migrating $COMPOSER_FILE from WPackagist to WP Composer..."

# Rename wpackagist-plugin/* → wp-plugin/* and wpackagist-theme/* → wp-theme/*
# in both require and require-dev, then swap the repository entry
jq '
  # Rename package keys in a given object
  def rename_packages:
    to_entries | map(
      if (.key | startswith("wpackagist-plugin/")) then
        .key = ("wp-plugin/" + (.key | ltrimstr("wpackagist-plugin/")))
      elif (.key | startswith("wpackagist-theme/")) then
        .key = ("wp-theme/" + (.key | ltrimstr("wpackagist-theme/")))
      else .
      end
    ) | from_entries;

  # Rename packages in require
  (if .require then .require |= rename_packages else . end) |

  # Rename packages in require-dev
  (if .["require-dev"] then .["require-dev"] |= rename_packages else . end) |

  # Replace wpackagist repository with wp-composer
  (if .repositories then
    .repositories = [
      (.repositories[] | select(
        (.url // "" | test("wpackagist\\.org") | not) and
        ((.name // "") != "wpackagist")
      )),
      {
        "name": "wp-composer",
        "type": "composer",
        "url": "https://wp-composer.com",
        "only": ["wp-plugin/*", "wp-theme/*"]
      }
    ]
  else . end)
' "$COMPOSER_FILE" > "${COMPOSER_FILE}.tmp" && mv "${COMPOSER_FILE}.tmp" "$COMPOSER_FILE"

echo "Done! Changes made to $COMPOSER_FILE:"
echo "  - Renamed wpackagist-plugin/* → wp-plugin/*"
echo "  - Renamed wpackagist-theme/* → wp-theme/*"
echo "  - Replaced WPackagist repository with WP Composer"
echo ""
echo "Run 'composer update' to install packages from WP Composer."
