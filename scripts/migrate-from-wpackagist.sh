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

# Detect indent: find first indented line and count leading spaces
INDENT=$(awk '/^[ \t]+[^ \t]/ { match($0, /^[ \t]+/); print RLENGTH; exit }' "$COMPOSER_FILE")
if [[ -z "$INDENT" || "$INDENT" -lt 1 ]]; then
  INDENT=4
fi

# Rename wpackagist-plugin/* → wp-plugin/* and wpackagist-theme/* → wp-theme/*
# in require, require-dev, and extra.patches, then swap the repository entry
jq --indent "$INDENT" '
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

  # Rename packages in extra.patches (composer-patches plugin)
  (if .extra.patches then .extra.patches |= rename_packages else . end) |

  # Rename package references in extra.installer-paths values
  (if .extra["installer-paths"] then
    .extra["installer-paths"] |= (
      to_entries | map(
        .value |= map(
          if startswith("wpackagist-plugin/") then
            "wp-plugin/" + ltrimstr("wpackagist-plugin/")
          elif startswith("wpackagist-theme/") then
            "wp-theme/" + ltrimstr("wpackagist-theme/")
          else .
          end
        )
      ) | from_entries
    )
  else . end) |

  # Filter out wpackagist repo entry
  def is_wpackagist:
    (.url // "" | test("wpackagist\\.org")) or ((.name // "") == "wpackagist");

  # WP Composer repo entry
  def wp_composer_repo:
    {
      "name": "wp-composer",
      "type": "composer",
      "url": "https://wp-composer.com",
      "only": ["wp-plugin/*", "wp-theme/*"]
    };

  # Replace wpackagist repository with wp-composer (handles both array and object formats)
  (if .repositories then
    if (.repositories | type) == "array" then
      .repositories = [(.repositories[] | select(is_wpackagist | not)), wp_composer_repo]
    else
      # Object format: remove wpackagist entries by key, add wp-composer
      .repositories |= (
        to_entries
        | map(select(.value | is_wpackagist | not))
        | from_entries
      )
      | .repositories["wp-composer"] = (wp_composer_repo | del(.name))
    end
  else . end)
' "$COMPOSER_FILE" > "${COMPOSER_FILE}.tmp" && mv "${COMPOSER_FILE}.tmp" "$COMPOSER_FILE"

echo "Done! Changes made to $COMPOSER_FILE:"
echo "  - Renamed wpackagist-plugin/* → wp-plugin/*"
echo "  - Renamed wpackagist-theme/* → wp-theme/*"
echo "  - Renamed wpackagist-plugin/*, wpackagist-theme/* in extra.patches"
echo "  - Renamed wpackagist-plugin/*, wpackagist-theme/* in extra.installer-paths"
echo "  - Replaced WPackagist repository with WP Composer"
echo ""
echo "Run 'composer update' to install packages from WP Composer."
