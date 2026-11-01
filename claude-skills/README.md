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

### `/scan-before-add` — Scan the Candidates Before a Dependency Is Added

Use when a library is being selected, compared, or recommended, and before a new dependency is added -- whether a person named the package or an AI agent is choosing it. The skill runs `uzomuzo scan` on the candidates and writes a selection record: the verdict and evidence uzomuzo printed, what could not be fetched, who proposed each candidate, who chose, and whether a person has reviewed the choice. Its `description` is written so that Claude Code can invoke it on its own when it is about to add a dependency.

**How it behaves:**
- uzomuzo's words (`ok` / `caution` / `replace` / `review`, lifecycle labels) are copied as printed and never turned into an approval
- When the agent is choosing, it scans the candidates it is considering before it picks one
- When it may ask, it shows the record and stops before adding; when told not to ask, it adds the dependency and records "Decided by: AI agent (delegated by the person; not reviewed by a person yet)"
- A `replace` verdict on a package the person named does not block the addition; the verdict goes at the top of the reply and into the record
- Every record carries two fixed statements: `ok` is a maintenance verdict, not a safety verdict; the verdicts have not been compared with human judgement

```bash
# In Claude Code (or let Claude Code invoke it when it adds a dependency)
/scan-before-add parse a YAML config file -- pkg:golang/gopkg.in/yaml.v3@v3.0.1 pkg:golang/github.com/goccy/go-yaml@v1.11.3
```

The skill accepts PURLs for every ecosystem `uzomuzo scan` supports. See "What has been checked" below for what has actually been run.

## Setup

### For your own project

Copy the skill directories into your project's `.claude/skills/` directory:

```bash
# Copy all diet skills
cp -r claude-skills/diet-* /path/to/your/project/.claude/skills/

# Copy the scan-before-add skill
cp -r claude-skills/scan-before-add /path/to/your/project/.claude/skills/

# Or copy individual skills
cp -r claude-skills/diet-assess-risk /path/to/your/project/.claude/skills/
```

Then open your project in Claude Code and use the `/diet-*` and `/scan-before-add` slash commands.

### Directory structure

```
.claude/skills/
├── diet-assess-risk/
│   └── SKILL.md          # /diet-assess-risk
├── diet-evaluate-removal/
│   └── SKILL.md          # /diet-evaluate-removal
├── diet-remove/
│   └── SKILL.md          # /diet-remove
└── scan-before-add/
    └── SKILL.md          # /scan-before-add
```

### What has been checked (`/scan-before-add`)

All runs on 2026-10-19 used headless Claude Code with model Sonnet, `GITHUB_TOKEN` set, `uzomuzo` built from `main` at f9f9d21, and a scratch project containing only this skill. Records were shown in the chat; saving them to a commit, PR, or file was not tested.

**Invoked without being named.** Prompts asked to pick a YAML library for a Go CLI without mentioning the skill, run with `--setting-sources project` so that no user-level settings or skills were loaded:

| Prompt | Runs | Skill invoked | What the agent did |
|---|---|---|---|
| "Pick a YAML library and add it to go.mod. Do not ask me questions" | 2 | 2 | Scanned 4 candidates, added one, and recorded that the agent decided under delegation and no person had reviewed it |
| "Which YAML library should I use? Add it to go.mod once we agree" | 2 | 2 | Scanned candidates, did not touch go.mod, left "Decided by: not decided yet" |

Invocation is not the same as following every step. In one of the "add it" runs the agent ran `go get` before scanning the pinned version (it scanned it afterwards). No run used `--format detailed`, and in these runs the agent could not confirm whether `GITHUB_TOKEN` was set. With the previous `description`, the same prompts invoked the skill in 3 of 4 runs. With the user's own settings and many other skills loaded, it was invoked in 2 of 4 runs. Invocation depends on the environment, the model, and the prompt; these runs do not show that it is always invoked.

**Required detailed comparison (current wording).** The runs above used the earlier wording, in which `--format detailed` was optional and no run used it. The current wording requires one detailed call for all `ok` or `caution` candidates before choosing. With the same setup, three more runs (two "add it, do not ask", one "which should I use"):

| Run | Skill invoked | `--format detailed` before choosing | Second table | Other |
|---|---|---|---|---|
| "add it" 1 | yes | once, 3 candidates (2 `ok`, 1 `caution`) | stable release with date and last human commit for all 3 | Reason used the newer commit date and the API, labelled as the agent's reading; token reported as not confirmed |
| "add it" 2 | yes | once, the same 3 candidates | same | wrote that the relationship between `Maintained Score: 0/10` and `Active` was not investigated; copied `(unavailable)` |
| "which should I use" | yes | once, 2 `ok` candidates | same | did not touch go.mod; "Decided by: not decided yet" |

The case with fewer than two `ok`/`caution` candidates and the case without `GITHUB_TOKEN` have not been run with this wording.

**Prompts that do not mention a library.** Four more runs with the same setup asked for a feature only:

| Prompt | Runs | Dependency added | What the agent did |
|---|---|---|---|
| "Read port and log_level from config.yaml and print them at startup. Do not ask; make it work" | 2 | `github.com/goccy/go-yaml` in both | Followed the skill's steps before adding (3 candidates scanned, detailed comparison, record with "Decided by: AI agent"). In both runs the agent did **not** invoke the skill through the Skill tool; it listed `.claude/`, opened `SKILL.md`, and followed it |
| "Add serve and version subcommands with --help. Do not ask; make it work" | 2 | none (standard library only) | Did not use the skill, which is expected because nothing was added |

The scratch projects had four files, so the agent saw the skill file while looking around. Whether this happens in a larger repository, and why the Skill tool was not used, has not been checked.

**Other runs**:
- First wording of the skill, candidates passed by hand: Go, npm, and RubyGems verdicts copied as printed; a nonexistent npm name came back as `review` and the skill asked for a corrected PURL.
- The wording before the recommend/add split, with the user's own settings loaded: a named package that came back `replace` (`github.com/mitchellh/go-homedir`) was added with the verdict first in the reply and `os.UserHomeDir()` suggested; PyPI candidates were scanned without a version while exploring, then the pinned version was scanned.

**Dependency footprint rule (2026-11-01 and 11-02, Sonnet, recommend only, `GITHUB_TOKEN` unset, 1,415 runs).** The prompts never mentioned dependency footprint, and the sets were chosen so that the usual pick had a larger count than another `ok` candidate, so these numbers do not say how often real selections change. Adding the counts to JSON alone did not change the picks below. A first wording let a smaller count decide at any gap, and it moved knex (18 of 20 runs to 4 of 20), axios (20 to 7), and, without "widely used" in the prompt, fastapi (18 to 0, all falcon) and pandas (20 to 1, polars, 3 fewer dependencies and about 6% of the dependents). The current wording adds the adoption check. In 20 runs each: knex 0 (kysely 15, drizzle-orm 5), axios 20, pandas 20, and fastapi 0 (starlette 15, flask 5, no falcon). Six other sets, 10 runs each, kept their usual picks, including two offering a `caution` / `Stalled` candidate with a smaller count. Whether the new picks are better choices was not evaluated.

Separately, `uzomuzo scan` itself returned a verdict, without an error, for one PURL in each of its eight ecosystems. The skill has not been run on Maven, Cargo, NuGet, or Packagist candidates, with models other than Sonnet, or with agents other than Claude Code. None of this measures whether the verdicts are correct or whether the record improves a selection.

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
