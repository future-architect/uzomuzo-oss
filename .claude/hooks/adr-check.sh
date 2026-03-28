#!/usr/bin/env bash
# ADR necessity check — analyzes git diff for architectural changes on push.
# Called as a Claude Code PreToolUse hook. Reads JSON from stdin.
set -euo pipefail

# Extract command from hook input
CMD=$(node -e "let d='';process.stdin.on('data',c=>d+=c);process.stdin.on('end',()=>console.log(JSON.parse(d).tool_input?.command||''));" <&0)

# Only trigger on git push
if ! echo "$CMD" | grep -qE '^git\s+push'; then
  exit 0
fi

# Determine diff base — robust fallback for shallow clones and small repos
if ! BASE=$(git merge-base HEAD origin/main 2>/dev/null); then
  if ! BASE=$(git merge-base HEAD main 2>/dev/null); then
    if git rev-parse HEAD~10 >/dev/null 2>&1; then
      BASE="HEAD~10"
    elif git rev-parse HEAD~1 >/dev/null 2>&1; then
      BASE="HEAD~1"
    else
      BASE=$(git rev-list --max-parents=0 HEAD | tail -n1)
    fi
  fi
fi
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

# 3. DB schema changes (lazy: only compute full diff when relevant files changed)
FULL_DIFF=""
if echo "$DIFF" | grep -qiE '\.sql$|/migrations/|\.go$'; then
  FULL_DIFF=$(git diff "$BASE"...HEAD -- . 2>/dev/null || true)
  if echo "$FULL_DIFF" | grep -qiE 'CREATE TABLE|ALTER TABLE|createTableSQL'; then
    REASONS+=("Database schema changes detected")
  fi
fi

# 4. New CLI subcommands
if echo "$DIFF" | grep -qE '^(internal/interfaces/cli/|cmd/)'; then
  if [ -z "$FULL_DIFF" ]; then
    FULL_DIFF=$(git diff "$BASE"...HEAD -- . 2>/dev/null || true)
  fi
  if echo "$FULL_DIFF" | grep -qE 'Command\{|case "'; then
    REASONS+=("New CLI subcommand possibly added")
  fi
fi

# 5. Changes spanning multiple DDD layers
LAYER_COUNT=$(echo "$DIFF" | grep -oE '^internal/(domain|application|infrastructure|interfaces)/' | sort -u | wc -l)
if [ "$LAYER_COUNT" -ge 3 ]; then
  REASONS+=("Changes span $LAYER_COUNT DDD layers")
fi

# Output warning if any reasons found
if [ ${#REASONS[@]} -gt 0 ]; then
  BULLET_LIST=$(printf '\\n- %s' "${REASONS[@]}")
  MSG="ADR CHECK: Architectural changes detected. Consider whether an ADR (docs/adr/) is needed:${BULLET_LIST}\\nIf minor, proceed. For significant decisions, write an ADR."
  echo "{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"additionalContext\":\"${MSG}\"}}"
fi
