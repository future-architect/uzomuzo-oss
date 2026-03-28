#!/bin/bash
# check-new-deps: Detect newly added Go dependencies and evaluate with uzomuzo.
# Called by Claude Code PreToolUse hook before "git commit" when go.mod is staged.
# Exit 0 = allow (prints warnings to stderr). Advisory only — never blocks.

set -euo pipefail

REPO_ROOT=$(git rev-parse --show-toplevel 2>/dev/null || echo ".")

# Check if go.mod has staged changes
if ! git diff --cached --name-only | grep -q '^go\.mod$'; then
  exit 0
fi

# Extract newly added module paths from require lines only
NEW_DEPS=$(git diff --cached go.mod | grep '^+' | grep -v '^+++' | sed 's/^+//' | \
  awk '
    # Track block context (require vs replace/exclude/retract)
    /^require \(/ { in_require = 1; next }
    /^\)/        { in_require = 0; next }
    # Single-line require: "require module/path v1.2.3"
    $1 == "require" && NF >= 3 && $3 ~ /^v[0-9]/ { print $2; next }
    # Inside require () block: rely on version field to identify deps
    in_require && $2 ~ /^v[0-9]/ { print $1 }
  ' | sort -u || true)

if [ -z "$NEW_DEPS" ]; then
  exit 0
fi

echo "=== OSS Health Check: new dependencies detected ===" >&2

# Build PURL array (skip self-owned modules)
PURLS=()
for dep in $NEW_DEPS; do
  case "$dep" in
    github.com/vuls-saas/*|github.com/future-architect/uzomuzo*) continue ;;
  esac
  PURLS+=("pkg:golang/$dep")
done

if [ ${#PURLS[@]} -eq 0 ]; then
  exit 0
fi

# Run uzomuzo evaluation (capture stderr for diagnostics on failure)
echo "Evaluating: ${PURLS[*]}" >&2
STDERR_TMP=$(mktemp -t check-new-deps.XXXXXX)
RESULT=$(cd "$REPO_ROOT" && GOWORK=off go run . "${PURLS[@]}" 2>"$STDERR_TMP") || {
  echo "WARN: uzomuzo evaluation failed ($(head -1 "$STDERR_TMP")), skipping health check" >&2
  rm -f "$STDERR_TMP"
  exit 0
}
rm -f "$STDERR_TMP"

# Check for EOL / Stalled — warn but do not block
while IFS= read -r line; do
  if echo "$line" | grep -qE 'EOL-Confirmed|EOL-Effective|EOL-Scheduled'; then
    echo "⚠ EOL:  $line" >&2
  elif echo "$line" | grep -q 'Stalled'; then
    echo "⚠ WARN: $line" >&2
  elif echo "$line" | grep -q 'Active'; then
    echo "✓ OK:   $line" >&2
  else
    echo "        $line" >&2
  fi
done <<< "$RESULT"

echo "=== OSS Health Check: done ===" >&2
exit 0
