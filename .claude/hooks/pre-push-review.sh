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

# Determine diff base (merge-base for three-dot diff)
BASE=$(git merge-base HEAD origin/main 2>/dev/null || git merge-base HEAD main 2>/dev/null || echo "")
if [ -z "$BASE" ]; then
  exit 0
fi
DIFF=$(git diff "$BASE" HEAD 2>/dev/null || true)

if [ -z "$DIFF" ]; then
  exit 0
fi

# Collect potential issues
ISSUES=()

# 1. Exported functions without godoc comments
CHANGED_GO=$(git diff "$BASE" HEAD --name-only -- '*.go' | grep -v '_test.go$' || true)
for f in $CHANGED_GO; do
  [ -f "$f" ] || continue
  # Find exported func/type/const/var without preceding comment
  if awk '
    /^\/\// { has_comment=1; next }
    /^func [A-Z]/ || /^type [A-Z]/ || /^const [A-Z]/ || /^var [A-Z]/ {
      if (!has_comment) { found=1; exit }
    }
    { has_comment=0 }
    END { exit !found }
  ' "$f" 2>/dev/null; then
    ISSUES+=("Missing godoc on exported identifier in $f")
  fi
done

# 2. Bare error returns (return err without wrapping)
if echo "$DIFF" | grep -qE '^\+.*return err[[:space:]]*$|^\+.*return err[[:space:]}]'; then
  ISSUES+=("Bare 'return err' without fmt.Errorf wrapping detected")
fi

# 3. TODO/FIXME left in new code
if echo "$DIFF" | grep -qE '^\+.*(TODO|FIXME|HACK|XXX)'; then
  ISSUES+=("TODO/FIXME/HACK comments in new code")
fi

# 4. Magic numbers in new code (numeric literals > 1 outside const blocks)
if echo "$DIFF" | grep -qE '^\+.*[^a-zA-Z0-9_][2-9][0-9]{2,}[^0-9]' | grep -vE '(const|//|http|port|0x|0o|0b)' >/dev/null 2>&1; then
  ISSUES+=("Possible magic numbers in new code — consider named constants")
fi

# Output instructions if issues found
if [ ${#ISSUES[@]} -gt 0 ]; then
  BULLET_LIST=$(printf '\\n- %s' "${ISSUES[@]}")
  MSG="PRE-PUSH REVIEW: Potential Copilot review issues detected. Fix these before pushing:${BULLET_LIST}\\n\\nRun 'git diff ${BASE} HEAD' to review, fix the issues, amend or add a commit, then push."
  echo "{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"additionalContext\":\"${MSG}\"}}"
else
  # No static issues found, but still remind to self-review
  MSG="PRE-PUSH REVIEW: No obvious issues detected by static checks. Before pushing, briefly verify: (1) all exported identifiers have godoc comments, (2) errors are wrapped with context, (3) no unnecessary code changes included."
  echo "{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"additionalContext\":\"${MSG}\"}}"
fi
