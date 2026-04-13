# Claude Code Skills for Dependency Diet

Reusable [Claude Code](https://claude.ai/code) skills for dependency analysis. These implement the "Detection with a tool, Analysis with an LLM" workflow.

## Skills

### `/diet-assess-risk` — EOL Dependency Risk Assessment

Detects EOL packages with uzomuzo, then uses the LLM to trace data flow through source code and assess security impact.

**What it does:**
1. Runs `uzomuzo scan` to detect EOL/Archived dependencies
2. Optionally runs `govulncheck` for reachability analysis
3. For each EOL package, reads the source code to trace what data flows through it
4. Constructs attack scenarios (unpatched vulnerabilities + supply chain takeover)
5. Identifies mitigating factors (encryption, validation, hash pinning)
6. Outputs a risk assessment table with recommended actions

```bash
# In Claude Code
/diet-assess-risk pkg:golang/github.com/foo/bar@v1.0.0
/diet-assess-risk top 5
```

### `/diet-evaluate-removal` — Data-Driven Dependency Removal Evaluation

Evaluates a single dependency for removal using a 5-phase data-driven framework that leverages diet's full JSON output.

**The 5 phases:**
1. **Ingest Diet Data** -- Extract all fields from diet JSON (existing file or fresh run)
2. **Usage Classification** -- Classify `import_files` into production/test/CI/example/generated
3. **Feasibility Analysis** -- API leakage gate check + symbol-by-symbol migration map
4. **6-Axis Scoring** -- Data-anchored ratings (no guesswork)
5. **Verdict** -- Structured REMOVE/DEFER/KEEP recommendation

**The 6 axes** (each anchored to concrete diet data):
| Axis | Data source |
|------|-------------|
| Transitive Cleanup | `exclusive_transitive` |
| Production Scope | Usage classification from Phase 2 |
| Coupling Depth | `coupling_effort`, `call_site_count`, `api_breadth` |
| Replaceability | Symbol migration map from Phase 3 |
| Security Urgency | `has_vulnerabilities`, `max_cvss_score`, `lifecycle` |
| Cascade Potential | `exclusive_transitive` + project knowledge |

```bash
# In Claude Code
/diet-evaluate-removal github.com/mitchellh/go-homedir
```

### `/diet-remove` — Guided Dependency Removal

Analyzes a dependency for removal, then either files an issue (default) or implements the change directly.

**Two modes:**
- **Issue mode (default)**: Runs pre-flight analysis, then files a GitHub Issue with findings and migration plan. Best for external OSS contributions.
- **PR mode (`--pr`)**: Full removal lifecycle — analysis → replacement → verification → commit. Use when you own the project.

```bash
# File an issue (default) — safe for external OSS
/diet-remove @vercel/kv --repo vercel/next.js

# Direct implementation — for your own project
/diet-remove github.com/pkg/errors --pr
```

## Setup

### For your own project

Copy the skill directories into your project's `.claude/skills/` directory:

```bash
# Copy all diet skills
cp -r claude-skills/diet-* /path/to/your/project/.claude/skills/

# Or copy individual skills
cp -r claude-skills/diet-assess-risk /path/to/your/project/.claude/skills/
```

Then open your project in Claude Code and use the `/diet-*` slash commands.

### Directory structure

```
.claude/skills/
├── diet-assess-risk/
│   └── SKILL.md          # /diet-assess-risk
├── diet-evaluate-removal/
│   └── SKILL.md          # /diet-evaluate-removal
└── diet-remove/
    └── SKILL.md          # /diet-remove
```

## How it fits together

These skills are the LLM-powered stages of the `scan → diet → LLM → remove` pipeline:

```
uzomuzo diet              Rank all deps by removability        (automated, CLI)
       ↓
/diet-assess-risk         "How dangerous is it to keep?"       (LLM traces data flow)
/diet-evaluate-removal    "Is removal worth the effort?"       (LLM classifies usage + scores 6 axes)
       ↓
/diet-remove              "Remove it safely"                   (LLM implements change)
```

`uzomuzo diet` provides the structured data (graph impact, coupling metrics, health signals). These skills read the actual source code to make decisions that automated scoring cannot — data flow tracing, replacement feasibility, API leakage detection.

See [Diet Command](../docs/diet.md) for the automated analysis, and [Diet Workflow](../docs/diet.md#diet-workflow-scan--diet--llm--remove) for the full pipeline documentation.

## Background

These skills were developed as part of the [Code Diet](https://github.com/future-architect/vuls) project — a systematic approach to reducing unnecessary dependencies in OSS projects. Presented at VulnCon 2026.
