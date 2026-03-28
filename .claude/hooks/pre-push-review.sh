#!/usr/bin/env bash
# pre-push-review.sh — Self-review against coding standards before pushing.
# Called as a Claude Code PreToolUse hook. Reads JSON from stdin.
# Instructs Claude to review its own diff for issues Copilot would flag.
set -euo pipefail

# Extract command from hook input
CMD=$(node -e "let d='';process.stdin.on('data',c=>d+=c);process.stdin.on('end',()=>{try{const j=JSON.parse(d||'{}');console.log(j.tool_input?.command||'');}catch(e){console.log('');}});" <&0)

# Only trigger on git push
if ! echo "$CMD" | grep -qE '^git\s+push'; then
  exit 0
fi

# Determine diff base
BASE=$(git merge-base HEAD origin/main 2>/dev/null || git merge-base HEAD main 2>/dev/null || echo "")
if [ -z "$BASE" ]; then
  exit 0
fi
# Only check Go source files (avoid false positives from scripts, docs, etc.)
DIFF=$(git diff "$BASE" HEAD -- '*.go' 2>/dev/null || true)

if [ -z "$DIFF" ]; then
  exit 0
fi

# Strip diff metadata lines to avoid false positives on headers
DIFF_CONTENT=$(echo "$DIFF" | grep -v '^+++ [ab]/' | grep -v '^--- [ab]/' | grep -v '^diff --git' | grep -v '^@@')

# Collect potential issues
ISSUES=()

# 1. Exported functions/types without godoc comments (only in newly added lines)
ADDED_EXPORTS=$(echo "$DIFF_CONTENT" | grep -E '^\+\s*(func [A-Z]|func \([^)]+\) [A-Z]|type [A-Z]|const [A-Z]|var [A-Z])' || true)
if [ -n "$ADDED_EXPORTS" ]; then
  # Check if preceding diff line is a comment
  while IFS= read -r export_line; do
    # Get the line number in the diff, check if previous added line is a comment
    LINE_NUM=$(echo "$DIFF" | grep -nF "$export_line" | head -1 | cut -d: -f1)
    if [ -n "$LINE_NUM" ] && [ "$LINE_NUM" -gt 1 ]; then
      PREV_LINE=$(echo "$DIFF" | sed -n "$((LINE_NUM - 1))p")
      if ! echo "$PREV_LINE" | grep -qE '^[+ ]\s*//'; then
        ISSUES+=("Missing godoc on exported identifier in diff")
        break
      fi
    fi
  done <<< "$ADDED_EXPORTS"
fi

# 2. Bare error returns (return err without wrapping)
if echo "$DIFF_CONTENT" | grep -qE '^\+.*return\s+(nil,\s*)?err\s*$|^\+.*return err[[:space:]}]'; then
  ISSUES+=("Bare 'return err' without fmt.Errorf wrapping detected")
fi

# 3. TODO/FIXME left in new code
if echo "$DIFF_CONTENT" | grep -qE '^\+.*(TODO|FIXME|HACK|XXX)'; then
  ISSUES+=("TODO/FIXME/HACK comments in new code")
fi

# 4. Magic numbers in new code (large numeric literals, heuristic check)
if echo "$DIFF_CONTENT" | grep -E '^\+.*[^a-zA-Z0-9_][2-9][0-9]{2,}([^0-9]|$)' | grep -qvE '(const|//|http|port|0x|0o|0b)'; then
  ISSUES+=("Possible magic numbers in new code — consider named constants")
fi

# Output instructions only if issues found
if [ ${#ISSUES[@]} -gt 0 ]; then
  BULLET_LIST=$(printf '\\n- %s' "${ISSUES[@]}")
  MSG="PRE-PUSH REVIEW: Potential Copilot review issues detected. Fix these before pushing:${BULLET_LIST}\\n\\nRun 'git diff ${BASE} HEAD' to review, fix the issues, amend or add a commit, then push."
  echo "{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"additionalContext\":\"${MSG}\"}}"
fi
