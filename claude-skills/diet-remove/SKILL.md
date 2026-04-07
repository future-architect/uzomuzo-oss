---
name: diet-remove
description: "Remove a dependency identified by uzomuzo diet — analysis + issue (default) or direct PR"
argument-hint: "<module path or PURL> [--pr] [--repo owner/repo]"
---

# Diet Remove: $ARGUMENTS

Analyze and plan the removal of dependency `$ARGUMENTS`, then take action.

## Mode selection

- **Default (Issue mode)**: Run Phase 1 analysis, then file a GitHub Issue with the findings and proposed migration plan. Appropriate for external OSS contributions and large projects where you don't own the build environment.
- **`--pr` (PR mode)**: Run the full removal lifecycle locally: analysis → replacement → verification → commit. Use this only when you own the project and can run build/test locally.

Parse `$ARGUMENTS` for flags:
- If `--pr` is present → PR mode (direct implementation)
- If `--repo owner/repo` is present → target that repository for the issue
- Otherwise → Issue mode (default)

**When to use**: After `/diet-evaluate-removal` confirms the dependency is worth removing, or when `uzomuzo diet` ranks it as trivial/easy.

**Safety principle**: Every removal must pass build + vet + test before committing. If any step fails, stop and diagnose — don't force it.

## Phase 1: Pre-flight checks

Before writing any code, answer these three questions:

### 1. Will it actually disappear?

Check the **STAYS** column in the `uzomuzo diet` output (or `stays_as_indirect` in JSON):

- **STAYS = `-`** → Removing this dep fully removes it from the dependency tree. Go ahead.
- **STAYS = `yes`** → Another direct dep depends on this transitively. It will remain as an indirect dependency after removal. Still worth doing (version management delegation, future removal readiness), but set expectations: it won't leave go.sum / lockfile.

In detailed output, the `IndirectVia` field shows exactly which direct deps pull it in transitively. These are the upstream targets for Phase 5 (Upstream Diet).

If diet output is not available, you can verify manually:
```bash
# Go
go mod why -m $ARGUMENTS
# npm
npm ls $ARGUMENTS
# pip
pip show $ARGUMENTS | grep "Required-by"
```

### 2. What's the replacement?

Determine the replacement strategy. In order of preference:

| Strategy | When to use | Example |
|----------|-------------|---------|
| **Delete** | Unused (0 imports) | Remove from go.mod/package.json, run tidy |
| **Standard library** | stdlib equivalent exists | `go-homedir` → `os.UserHomeDir()` |
| **Consolidate** | Another dep already does this | Two JSON libs → keep one |
| **Self-implement** | Small, non-crypto, well-defined API | `lfshook` → 20-line `logrus.Hook` impl |
| **Submodule isolate** | Used only in one subcommand/tool | `gosnmp` → `contrib/snmp2cpe/go.mod` |
| **Framework peel** | Dep comes via a framework you don't fully need | Trivy fanal → direct parser calls |

**NEVER self-implement**: crypto, TLS, protocol negotiation, auth token handling, or anything where subtle bugs create security vulnerabilities.

### 3. Are there hidden complications?

Check these before starting:

- **API leakage**: Does this dependency's types appear in exported identifiers?
  If yes, removal is a breaking change — needs major version bump or deprecation period.
  ```bash
  # Go: search for exported identifiers using the dep's types
  grep -rn "func.*$ARGUMENTS\|type.*$ARGUMENTS" --include="*.go" | grep -v _test.go | grep "^[A-Z]"
  ```

- **Build tags**: Is the import behind a build tag? (e.g., `//go:build jsoniter`)
  If yes, the dep may not affect default builds — consider just deleting the tagged file.

- **Generated code**: Files with `// Code generated` headers are trivially migrated
  by re-running the generator with the replacement tool.

- **Blank imports**: `import _ "pkg"` only runs `init()`. Check what `init()` does
  (usually driver/codec registration) before removing.

## Issue mode (default): File a GitHub Issue

### Step 0: Duplicate check (MANDATORY)

Before filing anything, search for existing issues and discussions. GitHub search is word-level tokenized — not semantic — so **run multiple queries** with different phrasings to reduce false negatives:

```bash
# Search by package name (exact)
gh search issues "{dependency}" --repo {owner/repo} --limit 10
# Search by replacement package name
gh search issues "{replacement}" --repo {owner/repo} --limit 10
# Search by keywords describing the change
gh search issues "replace deprecated {short-name}" --repo {owner/repo} --limit 10
# Search discussions (same queries)
gh api graphql -f query='{ search(query: "repo:{owner/repo} {dependency} type:discussion", type: DISCUSSION, first: 10) { nodes { ... on Discussion { title url } } } }'
```

Example for `@vercel/kv`:
```bash
gh search issues "@vercel/kv" --repo vercel/next.js --limit 10
gh search issues "@upstash/redis" --repo vercel/next.js --limit 10
gh search issues "replace deprecated kv" --repo vercel/next.js --limit 10
```

**Post-filter**: GitHub fuzzy search can return false positives. Verify that each hit is actually about the same dependency removal — not just a mention in passing.

If a matching issue/discussion already exists, **do not file a duplicate**. Instead, add a comment with any new analysis (e.g., impact data from diet) and stop.

### Step 1: File the issue or discussion

After completing Phase 1, **stop and file an issue** instead of implementing. This is the default because:
- External contributors cannot run CI or regenerate lockfiles
- Maintainers need context to evaluate the change
- Large monorepos have project-specific build/test requirements

### Issue template

Use `gh issue create` with the following structure:

```
Title: dep: replace EOL {dependency} with {replacement}

Body:
## Problem

`{dependency}` is {lifecycle status} (detected by [uzomuzo diet](https://github.com/future-architect/uzomuzo-oss)).
{1-2 sentences on why this matters — security risk, no more patches, etc.}

## Impact analysis

- **Files**: {N} files import this dependency
- **Call sites**: {N} calls across {N} APIs
- **Exclusive transitive deps**: {N} (removed together)
- **Stays as indirect**: {yes/no}
- **Difficulty**: {trivial/easy/moderate/hard}

### Usage breakdown

| File | Usage | Category |
|------|-------|----------|
{table of files and how they use the dependency}

## Proposed replacement

{replacement} — {why this is the right alternative}

### API mapping

| Current | Replacement |
|---------|-------------|
{API-level migration table}

### Environment variable changes

{any env var renames needed, or "None"}

## Notes

- {any hidden complications from Phase 1 step 3}
- {API leakage? build tags? generated code?}
```

### Choosing the right channel

Before filing, check the target repository's issue templates:

1. Run `ls <repo>/.github/ISSUE_TEMPLATE/` or check `config.yml` for `blank_issues_enabled`
2. If `blank_issues_enabled: false` and only bug/docs templates exist, the project likely uses **Discussions** for proposals. File in the `Ideas` category instead:
   ```bash
   # Use GitHub Discussions when issues require a specific template
   gh api graphql -f query='mutation { createDiscussion(input: { repositoryId: "...", categoryId: "...", title: "...", body: "..." }) { discussion { url } } }'
   ```
3. If blank issues are enabled or a "feature request" template exists, use `gh issue create`

**After filing, stop.** Do not proceed to implementation.

If `--pr` was specified, skip this section and continue to Phase 1.5 below.

---

## PR mode (`--pr`): Direct implementation

The following phases apply only in PR mode. Use this when you own the project.

## Phase 1.5: Test coverage check — before you touch anything

**Before writing any replacement code, check if the code that uses this dependency has tests.**

```bash
# Find all files importing the dependency (production code only)
grep -rn "$ARGUMENTS" --include="*.go" -l | grep -v _test.go

# For each file, check if a corresponding test file exists
# e.g., reporter/email.go → reporter/email_test.go
```

### If tests exist: You're safe to proceed

The existing tests define the expected behavior. After replacement, run them — if they pass, the replacement is correct.

### If tests DON'T exist: Write tests FIRST, before changing anything

This is the most important step in the entire process. **Write tests against the current (working) implementation before replacing it.** This gives you a safety net that catches behavior differences in the replacement.

1. **Identify the contract**: What does the code do with this dependency? What are the inputs and outputs?
2. **Write tests that capture current behavior**:
   - Normal cases (happy path)
   - Edge cases specific to the dependency's behavior (e.g., how does it handle nil? empty input? unicode?)
   - Error cases (what happens when the dependency returns an error?)
3. **Run the tests against the current code** — they must pass before you change anything
4. **Then** proceed to Phase 2

**Why before, not after?** If you write tests after replacing the code, you're only testing that your new code does what you *think* it should do — not what the old code *actually* did. Behavior differences slip through.

Real example: `c-robinson/iplib` handled IPv4 `/31` and `/32` CIDR prefixes differently from `net/netip`. If tests had been written after the replacement, the edge case would have been missed because the test would match the new (wrong) behavior.

### For framework peels: Build a regression harness

For high-impact removals (framework replacement, parser rewrite), unit tests aren't enough. Build a comparison harness:

```bash
# Build before and after binaries
git stash && go build -o /tmp/before ./cmd/... && git stash pop
go build -o /tmp/after ./cmd/...

# Run both against real-world inputs and diff the output
/tmp/before < input.json > /tmp/out-before.json
/tmp/after  < input.json > /tmp/out-after.json
diff /tmp/out-before.json /tmp/out-after.json
```

The vuls fanal framework removal used this approach: 17 real OSS lockfiles, 7,198 libraries compared — found 1 legitimate difference (a pnpm bug fix).

## Phase 2: Implementation

### Step 1: Create the replacement

Based on the strategy from Phase 1:

**For stdlib replacement:**
1. Find all import sites: `grep -rn "$ARGUMENTS" --include="*.go" | grep -v _test.go`
2. For each site, replace the API call with the stdlib equivalent
3. Update imports
4. If the replacement API has different error handling or return types, adapt the call site

**For self-implementation:**
1. Write the replacement in the same package that uses it (don't create a shared utility for a single use site)
2. Keep it minimal — match only the API surface actually used, not the full library
3. Write tests that cover the same behavior as the original

**For submodule isolation:**
1. Create `contrib/<tool>/go.mod` with its own module path
2. Move the relevant code under `contrib/<tool>/`
3. Watch for imports back to the root module (especially `version.go`, `config` packages)
4. Add `go.work` if needed for local development

**For framework peel:**
1. Identify which specific functions you actually call through the framework
2. Call them directly, bypassing the framework's registration/discovery layer
3. This is the highest-effort strategy but has the highest payoff
4. Build a comprehensive regression test BEFORE starting (golden files, A/B comparison)

### Step 2: Handle edge cases

Lessons learned from real dependency removals:

- **Mechanical replacements aren't fully mechanical.** `xerrors` → `fmt.Errorf` looked like a sed job, but 10 of 788 call sites had edge cases:
  - `[]error` passed to `%w` (needs `errors.Join`)
  - Non-error types passed to `%w` (needs `%v`)
  - Existing bugs hidden by the old library's lax type checking
  
- **Check for behavior differences in the stdlib equivalent:**
  - `net/smtp` is frozen but not deprecated — safe to use
  - IPv4 network/broadcast address handling differs between libraries
  - `tls.Dial` + `smtp.NewClient` is not the same as a library's `DialTLS`

- **Linter rules may change:** Switching from `xerrors.New` to `errors.New` may trigger `revive` rules about error message capitalization that the old library was exempt from.

### Step 3: Update dependency manifest

For Go:
```bash
# Remove the direct dependency
go mod edit -droprequire $ARGUMENTS
go mod tidy
```

For npm: Remove from `package.json`, run `npm install` or equivalent.
For Python: Remove from `requirements.txt` / `pyproject.toml`, run `pip install`.
For Maven: Remove from `pom.xml`.

## Phase 3: Verification

**All three must pass. No exceptions.**

```bash
# 1. Build
go build ./...

# 2. Static analysis
go vet ./...

# 3. Tests
go test ./... -count=1
```

For non-Go projects, run the equivalent build + lint + test pipeline.

### Additional verification for high-impact removals:

- **Check go.sum reduction:**
  ```bash
  wc -l go.sum  # compare with before
  ```
  If go.sum didn't shrink, the dependency remains as indirect.

- **Check binary size** (for compiled languages):
  ```bash
  go build -o /tmp/binary-after ./cmd/...
  ls -la /tmp/binary-after
  ```

- **For framework peels: A/B comparison**
  Build both old and new versions, run them against real-world inputs, diff the outputs.
  This catches subtle behavior changes that unit tests miss.

## Phase 4: Commit

Create a focused commit with clear metrics:

```
fix: remove {dependency} — replace with {replacement}

{What was done and why}

Before: {N} direct deps, go.sum {N} lines
After:  {N} direct deps, go.sum {N} lines
Binary: {size before} → {size after} ({reduction}%)
```

## Phase 5: Upstream Diet — when the dep stays as indirect

If `uzomuzo diet` showed **STAYS = `yes`** for this dependency (or you see it still in go.sum / lockfile after removal), it remains as an indirect dependency.

The diet detailed output's `IndirectVia` field already tells you exactly which direct deps pull it in. Use this to plan upstream removals.

### Is the upstream under your control?

**If the upstream is your organization's repo** (e.g., your company's other OSS projects):

1. Submit the same removal PR to that upstream repo
2. Wait for it to be merged and tagged
3. Update your `go.mod` to the new version: `go get -u <upstream>@latest`
4. Run `go mod tidy` — now it should disappear from `go.sum`

Real example: Removing `go-homedir` from vuls required PRs to 6 upstream repos (`go-exploitdb`, `go-kev`, `gost`, `go-cti`, `go-msfdb`, `go-cve-dictionary`). All used the identical `os.UserHomeDir()` replacement. Only after all 6 were merged and tagged did `go-homedir` finally leave vuls's `go.sum`.

**If the upstream is a third-party project you don't control:**

- You can submit a PR, but you can't control the timeline
- In the meantime, the removal from your direct deps still has value:
  - Version management is delegated to the upstream
  - Your code no longer calls the dependency directly
  - You're ready for instant cleanup when the upstream removes it
- Note this in the commit message: "Direct usage removed. Remains as indirect via X."

### Don't stop at one repo

If you manage multiple repos that share the same dependency, plan the removal across all of them:

```
Week 1: Remove from leaf repos (no downstream dependents)
Week 2: Tag releases of leaf repos
Week 3: Update parent repos to new versions, remove there too
Week 4: Tag releases, final go mod tidy in the top-level repo
```

## Phase 6: Step back — individual removal vs structural reform

After removing a few dependencies individually, check the cumulative effect:

```bash
# Compare with the original baseline
wc -l go.sum          # did it actually shrink meaningfully?
go build -o /tmp/bin . && ls -la /tmp/bin   # did binary size change?
```

### The "9 deps removed, binary unchanged" lesson

In the vuls Code Diet project, removing 9 individual dependencies barely changed the binary size. The dependencies were gone from `go.mod`, but their code was still pulled in transitively through a framework layer (Trivy's fanal).

The breakthrough came from **removing the framework itself** — replacing the fanal analyzer registration with direct parser calls. That single change dropped the scanner binary from 106.6 MB to 34.1 MB (-68%).

### When to shift from individual removal to structural reform

| Signal | Action |
|--------|--------|
| go.sum keeps getting longer despite removals | Check for a framework pulling everything back in |
| Binary size doesn't change after 3+ removals | Look for a shared layer that imports the deps transitively |
| Multiple deps serve the same framework | Remove the framework, not the individual deps |
| `diet` shows many deps with 0 ONLY-VIA-THIS | They're all shared through a common layer — peel the layer |

Structural reform approaches:
- **Framework peel**: Call specific functions directly instead of going through a plugin/registration layer
- **Reporter/plugin extraction**: Move optional integrations to submodules with their own `go.mod`
- **Binary split**: Separate CLI subcommands into separate binaries (git-style delegation — like `uzomuzo-diet` itself)

## Common patterns (from real removals)

| Dependency | Replacement | Effort | Surprise |
|-----------|-------------|--------|----------|
| `mitchellh/go-homedir` | `os.UserHomeDir()` | 15 min | None — simplest possible removal |
| `rifflock/lfshook` | 20-line `logrus.Hook` impl | 30 min | Test was 131 lines (file I/O) |
| `samber/lo` (UniqBy) | Per-package `uniqBy` helper | 30 min | Better to duplicate than abstract |
| `golang.org/x/oauth2` | 10-line `bearerTransport` | 30 min | Just a `RoundTripper` wrapper |
| `golang.org/x/xerrors` | `fmt.Errorf` + `errors` | 2 hr | 10 edge cases in 788 call sites, found existing bug |
| `gosnmp/gosnmp` | Submodule `contrib/snmp2cpe/` | 1 hr | `version.go` imported root module |
| `emersion/go-smtp` | `net/smtp` + LOGIN auth | 1.5 hr | TLS connection setup differs |
| `c-robinson/iplib` | `net/netip` CIDR enumeration | 1 hr | IPv4 network/broadcast edge cases |
| Trivy fanal framework | Direct parser calls | 2 days | -68% binary size, found pnpm bug |

## Important rules

- **One dependency per commit.** Don't bundle multiple removals — if one breaks, you can revert cleanly.
- **Don't over-engineer the replacement.** Match only the API surface actually used. Three similar lines > premature abstraction.
- **Run the full test suite, not just affected packages.** Transitive effects are real.
- **If `go mod tidy` re-adds the dependency**, something still imports it. Use `go mod why -m` to find out what.
- **Measure before AND after.** go.sum line count, binary size, build time. Put the numbers in the commit message.
- **If it's harder than expected, stop and reassess.** The removal may not be worth the effort — that's a valid conclusion.
