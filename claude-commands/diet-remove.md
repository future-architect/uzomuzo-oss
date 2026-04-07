---
description: "Remove a dependency identified by uzomuzo diet — with safety checks and verification"
arguments:
  - name: target
    description: "Module path or PURL to remove (e.g. github.com/pkg/errors, pkg:golang/github.com/foo/bar@v1.0.0)"
    required: true
---

# Diet Remove: $ARGUMENTS

Remove the dependency `$ARGUMENTS` from this project. This command handles the full removal lifecycle: analysis → replacement → verification → cleanup.

**When to use**: After `/diet-evaluate-removal` confirms the dependency is worth removing, or when `uzomuzo diet` ranks it as trivial/easy.

**Safety principle**: Every removal must pass build + vet + test before committing. If any step fails, stop and diagnose — don't force it.

## Phase 1: Pre-flight checks

Before writing any code, answer these three questions:

### 1. Will it actually disappear?

For Go projects:
```bash
go mod why -m $ARGUMENTS
```

If the output shows multiple import paths, removing your direct usage may leave it as an indirect dependency. This is still worth doing (version management delegation, future removal readiness), but set expectations correctly.

For other ecosystems: check if other dependencies pull this in transitively.

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
