#!/usr/bin/env bash
# readme-walkthrough.sh — pre-push static check for README / docs walkthroughs.
#
# Catches the "walkthrough-mismatch" class: a fenced ` ```bash ` /
# ` ```sh ` / ` ```shell ` / ` ```zsh ` / ` ```console ` / ` ```sh-session ` /
# ` ```shell-session ` block in README*.md or
# docs/*.md that mixes repo-root-relative paths (e.g., `cmd/uzomuzo/...`,
# `internal/...`) with a `cd <name>` fixture-relative shell CWD change. The
# block cannot be copy-pasted verbatim from any single CWD, so readers get
# stuck. Note: only `cd <name>` actually changes CWD; bare `./<name>` path
# arguments do NOT trigger the fixture signal (see Step body below for why).
#
# Hook contract: PreToolUse on Bash matcher gated on `git push`. Reads the
# tool input JSON from stdin (same as adr-check.sh / pre-push-review.sh). If
# issues are detected, emits an `additionalContext` JSON line that Claude
# Code surfaces back into the conversation. Non-blocking by design — the
# user can still push if they decide the heuristic is wrong; the next
# /review-until-clean Phase A round will catch real cases via the
# consistency-auditor agent.
set -euo pipefail

CMD=$(node -e "let d='';process.stdin.on('data',c=>d+=c);process.stdin.on('end',()=>{try{const j=JSON.parse(d||'{}');console.log(j.tool_input?.command||'');}catch(e){console.log('');}});" <&0)

# Trigger on any occurrence of `git push` in the command (whitespace-bounded,
# so `git pushback` does not trigger). Common Bash-tool invocations include:
#   `cd repo && git push`
#   `env GH_TOKEN=... git push`
#   multi-line scripts ending in `git push`
# A whitespace-or-start prefix plus whitespace-or-end suffix is sufficient
# for the heuristic; we never act on the false-positive cases (the hook is
# advisory `additionalContext`-only).
if ! echo "$CMD" | grep -qE '(^|[[:space:]]|;|&|\|)git[[:space:]]+push([[:space:]]|;|&|\||$)'; then
  exit 0
fi

BASE=$(git merge-base HEAD origin/main 2>/dev/null || git merge-base HEAD main 2>/dev/null || echo "")
if [ -z "$BASE" ]; then
  exit 0
fi

# Pathspecs use git's default fnmatch where `*` matches any character INCLUDING
# `/` — so `*README*.md` matches READMEs at any depth, and `docs/*.md`
# recurses through `docs/<sub>/`, etc.
#
# `:(exclude)testdata/**` / `:(exclude)internal/testdata/**` exclusion
# (long-form git pathspec magic; equivalent to the short `:!` prefix):
# testdata fixtures may DELIBERATELY contain mixed-CWD shell blocks as
# acceptance data for the consistency-auditor agent or other test
# scenarios. Without the exclusion,
# every edit to those fixtures would produce a pre-push warning even though
# the mismatch is the whole point of the fixture.
DOC_FILES=$(git diff --name-only "$BASE" HEAD -- '*README*.md' 'docs/*.md' ':(exclude)testdata/**' ':(exclude)internal/testdata/**' 2>/dev/null | sort -u || true)
if [ -z "$DOC_FILES" ]; then
  exit 0
fi

# scan_blocks <file>: walks the file inside one awk program (no fold/unfold
# round-trip), tracks fenced bash/sh/shell/zsh/console/sh-session/shell-session blocks, and emits
# "<file>:<start-line>: <reason>" for each block whose lines mix repo-root-
# relative paths with a `cd <name>` fixture-relative shell CWD change. Only
# `cd <name>` is a true CWD change; bare `./<name>` path arguments are not
# treated as fixture-relative (see fixture_re comment in BEGIN).
#
# Awk is the right tool here because (a) it handles the line-by-line state
# machine cleanly, (b) it avoids the bash `||`-as-newline-separator hack
# (which would corrupt blocks containing literal `||` shell logical-or), and
# (c) it strips the trailing CR for CRLF line endings via gsub.
scan_blocks() {
  local file="$1"
  awk -v file="$file" '
    function reset_block() {
      in_block = 0
      start_line = 0
      saw_root = 0
      saw_fixture = 0
      saw_clone = 0
    }

    BEGIN {
      reset_block()
      # POSIX character classes: ERE does not define \s. Keep bracket
      # expressions explicit so the script works on both gawk and BSD awk.
      # Repo-root anchors include `./bin/` so commands like `./bin/uzomuzo`
      # are classified as repo-root-relative.
      # Keep this prefix list aligned with the gsub mask below — when one
      # treats a path as repo-root, the other must too (otherwise a mixed
      # block can have its repo-root signal stripped from fixture_re without
      # firing root_re, and the mismatch goes undetected).
      # Repo-root anchors: directory prefixes (cmd/, internal/, pkg/, etc.)
      # AND bare top-level repo files commonly referenced from walkthroughs
      # (Makefile, go.mod, go.sum, .golangci.yml). Without the bare-file
      # alternatives, a block that says `cd demo && cat go.mod` would have
      # saw_root=0 and slip through even though the block requires repo-root
      # CWD to find go.mod.
      root_re      = "(^|[[:space:]])(\\./)?(cmd|internal|pkg|examples|scripts|testdata|third_party|claude-skills|bin|docs|\\.github|\\.claude)/"
      root_re      = root_re "|(^|[[:space:]])(\\./)?(Makefile|go\\.mod|go\\.sum|\\.golangci\\.yml|uzomuzo|uzomuzo-diet)([[:space:]]|$)"
      # Catch `cd <name>` in five common shapes: `cd foo`, `cd foo/`,
      # `cd ./foo`, `cd ./foo/`, and multi-segment forms like
      # `cd internal/corpus/sample`. The cd target is what actually changes
      # the shell CWD; bare `./<name>` path arguments (e.g.,
      # `trivy fs ./my-project`, `./uzomuzo scan`) do NOT change CWD and
      # therefore must not trigger the fixture signal — flagging them
      # produces false positives on every README example that names a path.
      cd_arg       = "(\\./)?[a-z][a-z0-9_./-]*/?"
      fixture_re   = "(^|[[:space:]])cd[[:space:]]+" cd_arg "([[:space:]]|$)"
      # Fence opener: tolerates optional whitespace plus either (a) a single-
      # token language info-string suffix like `bash {.line-numbers}` or
      # `bash title=...` (whitespace then arbitrary content), or (b) common
      # session-flavored language tags like `sh-session` / `shell-session`
      # (treated as single tokens by the GitHub Markdown syntax highlighter).
      fence_open   = "^```[[:space:]]*(bash|sh|shell|zsh|console|sh-session|shell-session)([[:space:]].*)?$"
      fence_close  = "^```[[:space:]]*$"
    }

    {
      # Strip CR so CRLF-line-ending docs are handled identically to LF docs.
      gsub(/\r$/, "")
    }

    !in_block && match($0, fence_open) {
      in_block   = 1
      start_line = NR
      saw_root   = 0
      saw_fixture = 0
      saw_clone  = 0
      next
    }

    in_block && match($0, fence_close) {
      if (saw_root && saw_fixture) {
        printf "%s:%d: shell block mixes repo-root-relative paths (e.g., cmd/, internal/, pkg/) with a `cd <name>` fixture-relative shell CWD change — readers cannot copy-paste from a single CWD\n", file, start_line
      }
      reset_block()
      next
    }

    in_block {
      if (match($0, root_re)) saw_root = 1
      # `git clone <url>` followed by `cd <repo-name>` is the canonical
      # "from-scratch install" pattern: the cd target becomes the repo root,
      # so any subsequent repo-root-relative paths are coherent. Detect the
      # clone and skip the cd-after-clone from fixture_re below.
      if (match($0, /(^|[[:space:]])git[[:space:]]+clone([[:space:]]|$)/)) saw_clone = 1
      # Mask any `cd <repo-root-prefix>...` token before fixture_re
      # matches. Without this, `cd internal/corpus/...` (a perfectly
      # valid repo-root walkthrough) fires fixture_re via the `cd ...`
      # shape even though the cd target IS itself a repo-root path.
      # Bare `./<prefix>/...` path arguments (without `cd`) need no
      # masking because fixture_re requires a `cd` prefix — they
      # cannot match fixture_re regardless.
      # The mask strips repo-root cd targets from a per-line copy
      # before fixture_re inspects it, so genuine fixture-relative
      # tokens (`cd vulnerable`) still match while repo-root cd
      # targets are excluded.
      line_for_fixture = $0
      gsub(/(^|[[:space:]])cd[[:space:]]+(\.\/)?(cmd|internal|pkg|examples|scripts|testdata|third_party|claude-skills|bin|docs|\.github|\.claude)\/[^[:space:]]*/, " ", line_for_fixture)
      # Also strip bare top-level repo-root file tokens (Makefile, go.mod,
      # go.sum, .golangci.yml, uzomuzo, uzomuzo-diet). The first gsub above
      # only strips `cd <dir-prefix>/...` (note the trailing `/`), so bare
      # names without a path separator slip through. Without this mask, a
      # line like `cd uzomuzo && go build ./cmd/uzomuzo` would have
      # `cd uzomuzo` match fixture_re (since `uzomuzo` starts with [a-z]
      # and satisfies cd_arg), while `cmd/` matches root_re — producing a
      # false-positive mixed-CWD flag even though `cd uzomuzo` is navigating
      # into the repo root (after a clone), not a fixture subdirectory.
      # Keep this list in lockstep with the bare-file alternation in
      # root_re above.
      gsub(/(^|[[:space:]])(\.\/)?(Makefile|go\.mod|go\.sum|\.golangci\.yml|uzomuzo|uzomuzo-diet)([[:space:]]|$)/, " ", line_for_fixture)
      # If `git clone` appeared earlier in this block, strip the FIRST
      # post-clone `cd <single-token>` so a benign clone-and-cd block like:
      #     git clone https://github.com/foo/bar
      #     cd bar
      #     go build ./cmd/bar
      # is not flagged. The mask only strips a single-segment cd target
      # (no slashes), which is the shape that matches the working
      # directory of a freshly-cloned repo. After consuming the post-clone
      # cd, clear `saw_clone` so later cd commands (`cd demo`, `cd fixture`)
      # still participate in mismatch detection — otherwise the exception
      # applies too broadly and hides real fixture transitions.
      if (saw_clone) {
        if (gsub(/(^|[[:space:]])cd[[:space:]]+(\.\/)?[a-z][a-z0-9_-]*\/?([[:space:]]|$)/, " ", line_for_fixture)) {
          saw_clone = 0
        }
      }
      if (match(line_for_fixture, fixture_re)) saw_fixture = 1
    }
  ' "$file"
}

ISSUES=()
while IFS= read -r doc; do
  [ -z "$doc" ] && continue
  # Skip files deleted in the diff (DOC_FILES comes from `git diff --name-only`,
  # which lists deletions too). awk on a missing file would error and, under
  # `set -e`, would kill the hook even though it is intended to be non-blocking.
  [ ! -f "$doc" ] && continue
  while IFS= read -r line; do
    [ -z "$line" ] && continue
    ISSUES+=("$line")
  done < <(scan_blocks "$doc")
done <<< "$DOC_FILES"

if [ ${#ISSUES[@]} -gt 0 ]; then
  BULLET_LIST=$(printf '\\n- %s' "${ISSUES[@]}")
  MSG="PRE-PUSH README WALKTHROUGH: Working-directory mismatch detected in fenced shell blocks. Either prefix the block with an explicit \\\"Run from <CWD>\\\" hint and rewrite all paths to that CWD, or split into two blocks each consistent with one CWD.${BULLET_LIST}\\n\\nThis is the walkthrough-mismatch class tracked by the consistency-auditor agent (.claude/agents/consistency-auditor.md). Run 'git diff ${BASE} HEAD -- \\\"*README*.md\\\" \\\"docs/*.md\\\" \\\":(exclude)testdata/**\\\" \\\":(exclude)internal/testdata/**\\\"' to reproduce the same file set the scanner walked."
  echo "{\"hookSpecificOutput\":{\"hookEventName\":\"PreToolUse\",\"additionalContext\":\"${MSG}\"}}"
fi
