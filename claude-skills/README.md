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

### `/diet-evaluate-removal` — 6-Axis Dependency Removal Evaluation

Evaluates a single dependency for removal feasibility across six axes.

**The 6 axes:**
| Axis | Question |
|------|----------|
| Update PR reduction | How many automated PRs does this dep generate? |
| Clean dependency list | Does removing it clarify the dependency intent? |
| Code standardization | Can it be replaced with stdlib? |
| Supply chain risk | Is it abandoned? Does removing reduce attack surface? |
| Code portability | How coupled is the usage? |
| Future removal readiness | Does removing this unblock further cleanups? |

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

## Background

These skills were developed as part of the [Code Diet](https://github.com/future-architect/vuls) project — a systematic approach to reducing unnecessary dependencies in OSS projects. Presented at VulnCon 2026.
