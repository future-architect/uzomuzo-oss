#!/usr/bin/env bash
# Plan-mode architect-consultation soft reminder.
#
# WHY: plan-mode work that affects DDD layer placement (domain /
# application / infrastructure / interfaces), repository interfaces,
# or cross-layer boundaries should consult the architect subagent
# before exiting plan mode. This hook nudges — it always allows
# ExitPlanMode and emits a reminder when no genuine architect tool-use
# record is found in the session transcript. Trivial fixes can ignore.
#
# Detection scope: the entire session transcript (jq scans every JSONL
# record for a tool_use object whose name == "Task" and
# input.subagent_type == "architect"). If architect was consulted earlier
# in the same session for an unrelated topic, this hook stays silent for
# the next ExitPlanMode — acceptable for a soft nudge.
#
# Requires: jq.
set -euo pipefail

command -v jq >/dev/null 2>&1 || exit 0

# Extract transcript_path from stdin JSON. If stdin isn't valid JSON
# (jq parse error) or the field is missing, treat it as "no transcript"
# and silently allow — this hook is fail-open by design.
TRANSCRIPT=$(jq -r '.transcript_path // empty' 2>/dev/null || true)

if [ -z "$TRANSCRIPT" ] || [ ! -f "$TRANSCRIPT" ]; then
  exit 0
fi

# Scan transcript for an architect Task tool_use record. Recurse into
# nested objects with `..|objects` so we find the tool_use block
# regardless of envelope shape, then match strictly on the three fields
# together. This deliberately excludes prose mentions of the pattern
# (rule-doc text, this hook's own additionalContext echoed in a prior
# turn) — only a real Task tool invocation counts.
#
# `any(inputs | ..|objects; cond)` streams JSONL via `inputs` and
# short-circuits as soon as it finds a tool_use record whose name is
# `Task` and input.subagent_type is `architect` — does NOT walk the
# whole transcript. With `-e`: outputs `true` and exits 0 on match,
# `false` and exits 1 on no match, and prints to stderr on parse / I/O
# error.
#
# `2>&1 1>/dev/null`: redirect jq's stderr into the $() capture (same
# target as stdout at that point), then send stdout to /dev/null. Net:
# stderr lands in $JQ_STDERR, stdout discarded. No temp file needed —
# avoids a `mktemp` failure path that would break fail-open.
set +e
JQ_STDERR=$(jq -n -e '
  any(
    inputs | ..|objects;
    (.type? == "tool_use") and
    (.name? == "Task") and
    (.input?.subagent_type? == "architect")
  )
' < "$TRANSCRIPT" 2>&1 1>/dev/null)
JQ_EXIT=$?
set -e

# Match (exit 0) OR any jq stderr (parse / I/O error) → silent allow.
# Only a clean "no match" (exit 1, empty stderr) falls through to
# emit the reminder.
if [ "$JQ_EXIT" -eq 0 ] || [ -n "$JQ_STDERR" ]; then
  exit 0
fi

USER_MSG='ExitPlanMode without consulting the architect subagent. If this plan touches DDD layer placement, repository interfaces, or cross-layer boundaries, consider consulting architect first. Trivial fixes can ignore.'
MODEL_CTX='REMINDER: no architect subagent tool-use found in this session before ExitPlanMode. If this plan involves DDD layer placement (domain / application / infrastructure / interfaces), repository interface design, or cross-layer concerns, consult the architect subagent before exiting plan mode. Trivial fixes (typo, comment, single-file refactor, simple flag addition) can ignore.'

# Emit the reminder JSON. If jq fails for any reason, still exit 0 to
# preserve the soft-reminder contract — silent allow is preferable to a
# blocked ExitPlanMode.
jq -nc --arg msg "$USER_MSG" --arg ctx "$MODEL_CTX" \
  '{systemMessage: $msg, hookSpecificOutput: {hookEventName: "PreToolUse", additionalContext: $ctx}}' || true
