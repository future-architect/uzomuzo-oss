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

# sync-instructions: .github/ → .claude/rules/ and AGENTS.md generated copies
sync-instructions:
	@for src in .github/instructions/*.instructions.md; do \
		base=$$(basename "$$src" .instructions.md); \
		dest=".claude/rules/$$base.md"; \
		if [ "$$base" = "agent-orchestration" ]; then dest=".claude/rules/agents.md"; fi; \
		echo "<!-- Generated from $$src — DO NOT EDIT DIRECTLY -->" > "$$dest"; \
		echo "" >> "$$dest"; \
		cat "$$src" >> "$$dest"; \
		echo "  $$src → $$dest"; \
	done
	@out="AGENTS.md"; \
	echo "<!-- Generated from .github/AGENTS.base.md — DO NOT EDIT DIRECTLY -->" > "$$out"; \
	echo "" >> "$$out"; \
	while IFS= read -r line; do \
		if [ "$$line" = "<!-- INSTRUCTION-INDEX -->" ]; then \
			echo "| File | Topic |"; \
			echo "|------|-------|"; \
			for src in .github/instructions/*.instructions.md; do \
				title=$$(grep -m1 '^# ' "$$src" | sed 's/^# //'); \
				echo "| \`$$src\` | $$title |"; \
			done; \
		else \
			echo "$$line"; \
		fi; \
	done < .github/AGENTS.base.md >> "$$out"; \
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
