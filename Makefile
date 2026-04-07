.PHONY: build build-diet build-all test lint clean sync-instructions update-doc-examples check-doc-examples

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

# ── Clean ──────────────────────────────────────────────────

clean:
	rm -f uzomuzo uzomuzo-diet

# sync-instructions: .github/instructions/ → .claude/rules/ generated copy
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
