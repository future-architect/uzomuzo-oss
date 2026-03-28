#!/usr/bin/env bash
# ADR necessity check — analyzes git diff for architectural changes on push.
# Called as a Claude Code PreToolUse hook. Reads JSON from stdin.
set -euo pipefail

# Extract command from hook input
CMD=$(node -e "let d='';process.stdin.on('data',c=>d+=c);process.stdin.on('end',()=>{try{const j=JSON.parse(d||'{}');console.log(j.tool_input?.command||'');}catch(e){console.log('');}});" <&0)

# Only trigger on git push
if ! echo "$CMD" | grep -qE '^git\s+push'; then
  exit 0
fi

# Determine diff base
BASE=$(git merge-base HEAD origin/main 2>/dev/null || git merge-base HEAD main 2>/dev/null || echo "HEAD~10")
DIFF=$(git diff "$BASE"...HEAD --name-only 2>/dev/null || true)

if [ -z "$DIFF" ]; then
  exit 0
fi

REASONS=()

# 1. Public API changes (exported types/methods in pkg/)
if echo "$DIFF" | grep -q '^pkg/'; then
  REASONS+=("Public API changes in pkg/")
fi

# 2. New external dependencies in go.mod
if echo "$DIFF" | grep -q '^go\.mod$'; then
  MOD_DIFF=$(git diff "$BASE"...HEAD -- go.mod 2>/dev/null || true)
  if echo "$MOD_DIFF" | grep -qE '^\+\s+\S+\s+v'; then
    REASONS+=("External dependencies changed in go.mod")
  fi
fi

# 3. DB schema changes
if git diff "$BASE"...HEAD -- . 2>/dev/null | grep -qiE 'CREATE TABLE|ALTER TABLE|createTableSQL'; then
  REASONS+=("Database schema changes detected")
fi

# 4. New CLI subcommands
if echo "$DIFF" | grep -qE '^(internal/interfaces/cli/|cmd/)'; then
  if git diff "$BASE"...HEAD -- internal/interfaces/cli cmd 2>/dev/null | grep -qE 'Command\{|case "'; then
    REASONS+=("New CLI subcommand possibly added")
  fi
fi

# 5. Changes spanning multiple DDD layers
LAYER_COUNT=$(echo "$DIFF" | awk -F/ '/^internal\/(domain|application|infrastructure|interfaces)\// {print "internal/" $2 "/"}' | sort -u | wc -l)
if [ "$LAYER_COUNT" -ge 3 ]; then
  REASONS+=("Changes span $LAYER_COUNT DDD layers")
fi

# Output warning if any reasons found
if [ ${#REASONS[@]} -gt 0 ]; then
  BULLET_LIST=$(printf '\\n- %s' "${REASONS[@]}")
  MSG="ADR CHECK: Architectural changes detected. Consider whether an ADR (docs/adr/) is needed:${BULLET_LIST}\\nIf minor, proceed. For significant decisions, write an ADR."
  echo "{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"additionalContext\":\"${MSG}\"}}"
fi
