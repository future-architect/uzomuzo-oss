# Claude Code Commands

Reusable [Claude Code](https://claude.ai/code) slash commands for dependency analysis. These implement the "Detection with a tool, Analysis with an LLM" workflow.

## Commands

### `/assess-eol-impact` — EOL Dependency Impact Assessment

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
/assess-eol-impact /path/to/your/go/project
```

### `/evaluate-dep-removal` — 6-Axis Dependency Removal Evaluation

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
/evaluate-dep-removal github.com/mitchellh/go-homedir
```

## Setup

1. Clone this repository (or just copy the `claude-commands/` directory)
2. Open your project in Claude Code
3. Run the commands as slash commands

Claude Code automatically discovers `.md` files in `claude-commands/` directories.

## Background

These commands were developed as part of the [Code Diet](https://github.com/future-architect/vuls) project — a systematic approach to reducing unnecessary dependencies in OSS projects. Presented at VulnCon 2026.
