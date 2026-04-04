.PHONY: sync-instructions update-doc-examples check-doc-examples

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

# check-doc-examples: dry-run mode for CI (exit 1 if any block would change).
# --skip-juice-shop: trivy is not available in standard CI runners.
check-doc-examples:
	go build -o uzomuzo ./cmd/uzomuzo
	go run ./scripts/update-doc-examples --skip-build --dry-run --skip-juice-shop
