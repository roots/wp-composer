#!/usr/bin/env bash
set -euo pipefail

# Migrate composer.json from WPackagist to WP Packages
# https://wp-packages.org/wp-packages-vs-wpackagist

# --dry-run / -n: show a diff of what would change without modifying the file.
DRY_RUN=false
COMPOSER_FILE=""

for arg in "$@"; do
  case "$arg" in
    --dry-run|-n) DRY_RUN=true ;;
    *) COMPOSER_FILE="$arg" ;;
  esac
done

COMPOSER_FILE="${COMPOSER_FILE:-composer.json}"

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

if $DRY_RUN; then
  echo "Dry run — no files will be modified."
fi
echo "Migrating $COMPOSER_FILE from WPackagist to WP Packages..."

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

  # WP Packages repo entry
  def wp_packages_repo:
    {
      "name": "wp-packages",
      "type": "composer",
      "url": "https://repo.wp-packages.org"
    };

  # Replace wpackagist repository with wp-packages (handles both array and object formats)
  (if .repositories then
    if (.repositories | type) == "array" then
      .repositories = [(.repositories[] | select(is_wpackagist | not)), wp_packages_repo]
    else
      # Object format: remove wpackagist entries by key, add wp-packages
      .repositories |= (
        to_entries
        | map(select(.value | is_wpackagist | not))
        | from_entries
      )
      | .repositories["wp-packages"] = (wp_packages_repo | del(.name))
    end
  else . end)
' "$COMPOSER_FILE" > "${COMPOSER_FILE}.tmp"

if $DRY_RUN; then
  rm "${COMPOSER_FILE}.tmp"

  # Collect all wpackagist package references across require, require-dev, and extra.patches
  RENAMED=$(jq -r '
    [
      (.require // {} | to_entries[]),
      (.["require-dev"] // {} | to_entries[]),
      (.extra.patches // {} | to_entries[])
    ][]
    | select(.key | test("^wpackagist-(plugin|theme)/"))
    | [
        .key,
        (if (.key | startswith("wpackagist-plugin/")) then
          "wp-plugin/" + (.key | ltrimstr("wpackagist-plugin/"))
        else
          "wp-theme/" + (.key | ltrimstr("wpackagist-theme/"))
        end)
      ]
    | @tsv
  ' "$COMPOSER_FILE")

  # Collect wpackagist references in extra.installer-paths
  RENAMED_PATHS=$(jq -r '
    (.extra["installer-paths"] // {} | to_entries[].value[])
    | select(test("^wpackagist-(plugin|theme)/"))
    | [
        .,
        (if startswith("wpackagist-plugin/") then
          "wp-plugin/" + ltrimstr("wpackagist-plugin/")
        else
          "wp-theme/" + ltrimstr("wpackagist-theme/")
        end)
      ]
    | @tsv
  ' "$COMPOSER_FILE")

  # Find wpackagist repository entries that would be removed
  REMOVED_REPOS=$(jq -r '
    (.repositories // {}) |
    if type == "array" then
      .[] | select((.url // "") | test("wpackagist\\.org")) | .url
    else
      to_entries[] | select(.value.url // "" | test("wpackagist\\.org")) | .value.url
    end
  ' "$COMPOSER_FILE")

  echo ""

  if [[ -n "$RENAMED" ]]; then
    echo "Packages to be renamed:"
    while IFS=$'\t' read -r from to; do
      printf "  %s  →  %s\n" "$from" "$to"
    done <<< "$RENAMED"
  fi

  if [[ -n "$RENAMED_PATHS" ]]; then
    echo ""
    echo "References in extra.installer-paths to be renamed:"
    while IFS=$'\t' read -r from to; do
      printf "  %s  →  %s\n" "$from" "$to"
    done <<< "$RENAMED_PATHS"
  fi

  echo ""
  echo "Repository changes:"
  if [[ -n "$REMOVED_REPOS" ]]; then
    while IFS= read -r url; do
      echo "  - $url  (removed)"
    done <<< "$REMOVED_REPOS"
  fi
  echo "  + https://repo.wp-packages.org  (wp-packages added)"

  echo ""
  echo "Dry run complete. Run without --dry-run to apply these changes."
else
  mv "${COMPOSER_FILE}.tmp" "$COMPOSER_FILE"
  echo "Done! Changes made to $COMPOSER_FILE:"
  echo "  - Renamed wpackagist-plugin/* → wp-plugin/*"
  echo "  - Renamed wpackagist-theme/* → wp-theme/*"
  echo "  - Renamed wpackagist-plugin/*, wpackagist-theme/* in extra.patches"
  echo "  - Renamed wpackagist-plugin/*, wpackagist-theme/* in extra.installer-paths"
  echo "  - Replaced WPackagist repository with WP Packages"
  echo ""
  echo "Run 'composer update' to install packages from WP Packages."

  script_name="$(basename -- "${BASH_SOURCE[0]}")"
  read -r -p "Delete the downloaded migration script ($script_name)? [y/N] " reply
  [[ "$reply" =~ ^[Yy]$ ]] && rm -- "${BASH_SOURCE[0]}"
fi
