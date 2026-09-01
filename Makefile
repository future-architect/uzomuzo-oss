.PHONY: build build-diet build-all test lint bench bench-save clean sync-instructions update-doc-examples check-doc-examples

# ── Build ──────────────────────────────────────────────────

build:
	go build -o uzomuzo ./cmd/uzomuzo

build-diet:
	CGO_ENABLED=1 go build -o uzomuzo-diet ./cmd/uzomuzo-diet

build-all: build build-diet

# ── Test & Lint ────────────────────────────────────────────

test:
	go test ./... -short -count=1

lint:
	go vet ./...

# ── Benchmark ──────────────────────────────────────────────

# BENCH_CMD: shared invocation for `bench` and `bench-save`, kept in one place
# so the two targets can't drift apart on flags. CGO_ENABLED=1 is required
# because the treesitter package is //go:build cgo; without it those
# benchmarks are silently excluded from the run.
BENCH_CMD = CGO_ENABLED=1 go test ./... -run='^$$' -bench=. -benchmem -count=10

# bench: run every benchmark at 10 counts for benchstat comparison.
bench:
	$(BENCH_CMD)

# bench-save: same run, captured to FILE for benchstat before/after comparison.
# Usage: make bench-save FILE=bench-0.txt
# Redirects to FILE and re-emits via cat rather than piping through tee, so a
# failing `go test` still fails the target — under the default POSIX `/bin/sh`
# (dash), `cmd | tee FILE` reports tee's exit status, not cmd's, and dash has
# no `pipefail` to fix that.
bench-save:
	@test -n "$(FILE)" || (echo "FILE is required: make bench-save FILE=path" >&2; exit 1)
	$(BENCH_CMD) > "$(FILE)"; status=$$?; cat -- "$(FILE)"; exit $$status

# ── Clean ──────────────────────────────────────────────────

clean:
	rm -f uzomuzo uzomuzo-diet

# AGENTS_MD_MAX_BYTES: budget for the generated AGENTS.md.
# Codex reads AGENTS.md files from the repository root downwards, concatenates
# them, and stops adding content once the combined size reaches
# `project_doc_max_bytes` (32 KiB by default) — silently, with no error. Nothing
# else in CI can see that happen: the file would still match its source, so the
# freshness gate stays green while agents receive a truncated document.
# Half the cap leaves room for the user's own ~/.codex/AGENTS.md and for any
# nested AGENTS.md added later. Currently ~6 KB.
AGENTS_MD_MAX_BYTES := 16384

# INSTRUCTION_SOURCES: the canonical rule files, from two places.
#   .github/instructions/*.instructions.md           — this repository's own rules
#   .github/instructions/base/<profile>/*.md         — the shared base, one directory
#                                                      per profile, consumed by sibling repos
# `$(sort)` de-duplicates and imposes a deterministic order independent of
# directory-listing order, which the generated index depends on.
INSTRUCTION_SOURCES := $(sort $(wildcard .github/instructions/*.instructions.md) \
                              $(wildcard .github/instructions/base/*/*.instructions.md))

# sync-instructions: .github/ → .claude/rules/ and AGENTS.md generated copies
sync-instructions:
	@set -e; \
	[ -n "$(INSTRUCTION_SOURCES)" ] || { echo "ERROR: no instruction sources found under .github/instructions/ — refusing to generate an empty rule set" >&2; exit 1; }
	@set -e; \
	for src in $(INSTRUCTION_SOURCES); do \
		base=$$(basename "$$src" .instructions.md); \
		dest=".claude/rules/$$base.md"; \
		if [ "$$base" = "agent-orchestration" ]; then dest=".claude/rules/agents.md"; fi; \
		echo "<!-- Generated from $$src — DO NOT EDIT DIRECTLY -->" > "$$dest"; \
		echo "" >> "$$dest"; \
		cat "$$src" >> "$$dest"; \
		echo "  $$src → $$dest"; \
	done
	@set -e; \
	out="AGENTS.md"; \
	tmp=$$(mktemp ./AGENTS.md.tmp.XXXXXX); \
	trap 'rm -f "$$tmp"' EXIT; \
	echo "<!-- Generated from .github/AGENTS.base.md — DO NOT EDIT DIRECTLY -->" > "$$tmp"; \
	echo "" >> "$$tmp"; \
	markers=0; \
	while IFS= read -r line || [ -n "$$line" ]; do \
		if [ "$$line" = "<!-- INSTRUCTION-INDEX -->" ]; then \
			markers=$$((markers + 1)); \
			printf '%s\n' "| File | Topic |" "|------|-------|"; \
			for src in $(INSTRUCTION_SOURCES); do \
				[ -e "$$src" ] || continue; \
				title=$$(grep -m1 '^# ' "$$src" | sed 's/^# //; s/|/\\|/g'); \
				[ -n "$$title" ] || { echo "ERROR: $$src has no '# ' heading — cannot build the instruction index" >&2; exit 1; }; \
				printf '| `%s` | %s |\n' "$$src" "$$title"; \
			done; \
		else \
			printf '%s\n' "$$line"; \
		fi; \
	done < .github/AGENTS.base.md >> "$$tmp"; \
	[ "$$markers" = "1" ] || { echo "ERROR: expected exactly one <!-- INSTRUCTION-INDEX --> marker line in .github/AGENTS.base.md, substituted $$markers. The match is exact — a typo or leading/trailing whitespace on that line will not be recognised." >&2; exit 1; }; \
	size=$$(wc -c < "$$tmp"); \
	[ "$$size" -le "$(AGENTS_MD_MAX_BYTES)" ] || { echo "ERROR: $$out would be $$size bytes, over the $(AGENTS_MD_MAX_BYTES)-byte budget. Codex concatenates AGENTS.md files and silently DISCARDS everything past project_doc_max_bytes (32 KiB by default) — an oversized file loses its tail with no error. Shorten .github/AGENTS.base.md; the rule text belongs in .github/instructions/, which the index points at rather than inlining." >&2; exit 1; }; \
	chmod 0644 "$$tmp"; \
	mv "$$tmp" "$$out"; \
	echo "  .github/AGENTS.base.md → $$out"

# update-doc-examples: rebuild binary then refresh all doc output blocks.
# Two-step build: "go build" produces the binary whose output we capture,
# then "go run" executes the replacement script with --skip-build.
update-doc-examples:
	go build -o uzomuzo ./cmd/uzomuzo
	go run ./scripts/update-doc-examples --skip-build

# check-doc-examples: validate marker structure in CI (no binary or API calls needed).
# Checks that every command in commands.json has matching begin/end markers
# in the target Markdown files. Does not compare output content (which varies
# due to non-deterministic API data like star counts and release dates).
check-doc-examples:
	go run ./scripts/update-doc-examples --check-markers
