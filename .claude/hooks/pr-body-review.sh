#!/usr/bin/env bash
# pr-body-review.sh — Advisory check that PR / issue body + title prose is plain English, not
# stacked coined internal jargon. Fires as a Claude Code PreToolUse (Bash) hook on
# gh pr create|edit / gh issue create|edit. Reads JSON from stdin. Companion to the jargon
# check in pre-push-review.sh (which scans Go // comment lines); this covers PR/issue prose.
# Advisory only, fail-open.
set -euo pipefail

CMD=$(node -e "let d='';process.stdin.on('data',c=>d+=c);process.stdin.on('end',()=>{try{const j=JSON.parse(d||'{}');console.log(j.tool_input?.command||'');}catch(e){console.log('');}});" <&0)

# Fire only on PR / issue create|edit (the surfaces with author-written prose). Not view/list.
if ! echo "$CMD" | grep -qE '(^|[[:space:]]|;|&|\|)gh[[:space:]]+(pr|issue)[[:space:]]+(create|edit)([[:space:]]|$)'; then
  exit 0
fi

# Denylist of coined jargon that should be plain English. Same core as pre-push-review.sh's
# jargon check — KEEP THE TWO IN SYNC. Deliberately omits settled domain vocabulary (e.g.
# taint / verdict / gate) to keep false positives low. `|| true` keeps the fail-open contract
# under set -o pipefail. Blind spot: only inline --body / heredoc text reaches the command
# string; --body-file content is not scanned.
JARGON=$(echo "$CMD" \
  | grep -oE 'materialize|fail-closed|over-?claim|wire DTO|tiebreaker|leaf helper|skip set|self-driven|adapter-direct' \
  | sort -u | tr '\n' ' ' || true)

if [ -n "$JARGON" ]; then
  MSG="PR/ISSUE BODY REVIEW: title/body contains likely coined jargon: ${JARGON}— rewrite in plain English before creating/editing (copilot-learned-coding: comment-jargon-density). If precision is needed, keep the body plain and fold exact terms into a trailing <details> glossary."
  echo "{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"additionalContext\":\"${MSG}\"}}"
fi
