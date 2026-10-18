# Claude Code Skills for Dependency Diet

Reusable [Claude Code](https://claude.ai/code) skills for dependency analysis. These implement the "Detection with a tool, Analysis with an LLM" workflow.

## Skills

### `/diet-assess-risk` — EOL Dependency Risk Assessment

Detects EOL packages with uzomuzo, then uses the LLM to trace data flow through source code and assess security impact.

**What it does:**
1. Reads the diet entry, then runs `uzomuzo scan` on the pinned version for the signals diet JSON does not carry (OSV malicious-package advisories, the npm dormant-release marker, the reason behind `Review Needed`)
2. Checks the pinned version's own advisories (diet's vulnerability fields describe the release uzomuzo picks as latest -- stable, else highest, else pre-release, else the requested version -- and include that release's dependencies)
3. Classifies the threat profile; a malicious version short-circuits to incident handling (which secrets to rotate)
4. For each unhealthy package, reads the source code to trace what data flows through it, including runtime-loaded dependencies with no import
5. Constructs attack scenarios (unpatched vulnerabilities + supply chain takeover, including install-time execution)
6. Identifies mitigating factors (encryption, validation, hash pinning, disabled install scripts, release-age delays)
7. Outputs a risk assessment table with recommended actions

```bash
# In Claude Code
/diet-assess-risk pkg:golang/github.com/foo/bar@v1.0.0
/diet-assess-risk top 5
```

### `/diet-evaluate-removal` — Data-Driven Dependency Removal Evaluation

Evaluates a single dependency for removal using a 5-phase data-driven framework that leverages diet's full JSON output.

**The 5 phases:**
1. **Ingest Diet Data** -- Extract all fields from diet JSON, check that coupling was actually measured, and fetch malicious/dormant-release signals from `uzomuzo scan`
2. **Usage Classification** -- Classify `import_files` into production/test/CI/example/generated
3. **Feasibility Analysis** -- API leakage gate check + symbol-by-symbol migration map + verification cost (are the calling files tested?)
4. **6-Axis Scoring** -- Data-anchored ratings (no guesswork)
5. **Verdict** -- Structured REMOVE/DEFER/KEEP/UPGRADE recommendation, with an effort estimate that accounts for the tests that must be written first

**The 6 axes** (each anchored to concrete diet data):
| Axis | Data source |
|------|-------------|
| Transitive Cleanup | `exclusive_transitive` |
| Production Scope | Usage classification from Phase 2 |
| Coupling Depth | `coupling_effort`, `call_site_count`, `api_breadth` |
| Replaceability | Symbol migration map from Phase 3 |
| Security Urgency | `has_vulnerabilities`, `max_cvss_score` (latest release, including its dependencies), scan's `direct_advisory_count`, `lifecycle`, scan's `reason` |
| Cascade Potential | `exclusive_transitive` + project knowledge |

```bash
# In Claude Code
/diet-evaluate-removal github.com/mitchellh/go-homedir
```

### `/diet-remove` — Guided Dependency Removal

Analyzes a dependency for removal, then either files an issue (default) or implements the change directly.

**Two modes:**
- **Issue mode (default)**: Runs pre-flight analysis, then drafts a GitHub Issue with findings and migration plan, and posts it after you approve the draft. Best for external OSS contributions.
- **PR mode (`--pr`)**: Full removal lifecycle — analysis → replacement → verification → commit. Use when you own the project.

```bash
# File an issue (default) — safe for external OSS
/diet-remove @vercel/kv --repo vercel/next.js

# Direct implementation — for your own project
/diet-remove github.com/pkg/errors --pr
```

### `/select-record` — Record What Was Checked Before Adding a Dependency

Before a new dependency is added -- by you or by an AI agent -- runs `uzomuzo scan` on each candidate and writes a selection record: the verdict and evidence uzomuzo printed, what could not be fetched, and who decided. It does not choose, approve, or install anything.

**What the record contains:**
- Purpose, requirements stated beforehand, and who proposed each candidate
- uzomuzo's `verdict`, `lifecycle`, `reason`, `archived`, and repository, copied as printed
- What was not obtained (for example, no `GITHUB_TOKEN`) and what was not checked for the requested version
- Two fixed statements: `ok` is a maintenance verdict, not a safety verdict; the verdicts have not been compared with human judgement
- "Decided by" and the reason, filled in by a person, not by the skill

```bash
# In Claude Code
/select-record parse a YAML config file -- pkg:golang/gopkg.in/yaml.v3@v3.0.1 pkg:golang/github.com/goccy/go-yaml@v1.11.3
```

The skill accepts PURLs for every ecosystem `uzomuzo scan` supports. See "What has been checked" below for the ecosystems the skill itself has been run on.

## Setup

### For your own project

Copy the skill directories into your project's `.claude/skills/` directory:

```bash
# Copy all diet skills
cp -r claude-skills/diet-* /path/to/your/project/.claude/skills/

# Copy the selection-record skill
cp -r claude-skills/select-record /path/to/your/project/.claude/skills/

# Or copy individual skills
cp -r claude-skills/diet-assess-risk /path/to/your/project/.claude/skills/
```

Then open your project in Claude Code and use the `/diet-*` and `/select-record` slash commands.

### Directory structure

```
.claude/skills/
├── diet-assess-risk/
│   └── SKILL.md          # /diet-assess-risk
├── diet-evaluate-removal/
│   └── SKILL.md          # /diet-evaluate-removal
├── diet-remove/
│   └── SKILL.md          # /diet-remove
└── select-record/
    └── SKILL.md          # /select-record
```

### What has been checked (`/select-record`)

Run through Claude Code on 2026-10-19 with `GITHUB_TOKEN` set, from a scratch project that had only the copied skill directory:

| Ecosystem | Candidates | Result |
|---|---|---|
| Go | `gopkg.in/yaml.v3@v3.0.1`, `github.com/goccy/go-yaml@v1.11.3`, `sigs.k8s.io/yaml@v1.4.0` | Record written with `replace` / `ok` / `caution` as printed; "Decided by" left as "not decided yet" |
| npm | `request@2.88.2`, `axios@1.6.8`, a nonexistent package name | `replace` / `ok` copied as printed; the nonexistent name came back as `review` and the skill asked for a corrected PURL |
| RubyGems | `nokogiri@1.15.0` | `ok` copied as printed |

`uzomuzo scan` itself returned a verdict for one PURL in each of its eight ecosystems on the same day. The skill has not been run through Claude Code on PyPI, Maven, Cargo, NuGet, or Packagist candidates. None of this measures whether the verdicts are correct or whether the record improves a selection.

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
