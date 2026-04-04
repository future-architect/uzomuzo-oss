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

# update-doc-examples: rebuild binary and refresh all doc output blocks
update-doc-examples:
	go build -o uzomuzo ./cmd/uzomuzo
	go run ./scripts/update-doc-examples --skip-build

# check-doc-examples: dry-run mode for CI (exit 1 if any block would change)
check-doc-examples:
	go build -o uzomuzo ./cmd/uzomuzo
	go run ./scripts/update-doc-examples --skip-build --dry-run --skip-juice-shop
