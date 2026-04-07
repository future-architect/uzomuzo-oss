# Case Study: Removing @vercel/kv from next.js

**Date**: 2026-04-07
**Target**: [vercel/next.js](https://github.com/vercel/next.js) — `@vercel/kv` → `@upstash/redis`
**Outcome**: Discussion filed ([#92479](https://github.com/vercel/next.js/discussions/92479))
**Theme**: Detection is solved. Remediation is not.

## What uzomuzo diet found

| Metric | Value |
|--------|-------|
| Package | `@vercel/kv@0.2.4` |
| Lifecycle | EOL-Confirmed |
| Priority rank | 141 / 304 |
| Difficulty | moderate |
| Files | 5 |
| Call sites | 7 across 5 APIs |
| Exclusive transitive deps | 2 |
| Stays as indirect | No — fully removable |

The automated analysis took seconds. The dependency was clearly EOL, the coupling was shallow, and the replacement (`@upstash/redis`) was a 1:1 compatible upstream. On paper, this was a straightforward removal.

## What happened when we tried to remove it

### Attempt 1: Direct PR → CI failure

We forked the repo, replaced all 5 source files, updated 4 `package.json` files, and submitted [PR #92476](https://github.com/vercel/next.js/pull/92476).

Every CI job failed:

| Failure | Cause |
|---------|-------|
| `ERR_PNPM_OUTDATED_LOCKFILE` | `pnpm-lock.yaml` not regenerated — requires `pnpm install` in the monorepo environment |
| `Job unable to validate for kotakanbe` | Self-hosted runners reject PRs from external forks (security policy) |
| `stats-results ENOENT` | Downstream jobs failed because build never completed |

**Lesson**: The code change was trivial (11 files, +29 -25 lines). The infrastructure to validate it was not. next.js is a monorepo with pnpm workspaces, Rust/cargo builds, and self-hosted CI — an external contributor cannot reproduce this environment.

### Attempt 2: Issue → auto-closed by bot

We filed [Issue #92477](https://github.com/vercel/next.js/issues/92477) with a detailed analysis.

Within seconds, a GitHub Actions bot closed it:

> We could not detect a valid reproduction link. Make sure to follow the bug report template carefully.

**Cause**: next.js has `blank_issues_enabled: false` in `.github/ISSUE_TEMPLATE/config.yml`. Only bug reports (with reproduction links) and docs reports are accepted as issues. Dependency removal proposals are not bugs.

### Attempt 3: Discussion → success

The `config.yml` pointed to Discussions for feature requests. We created [Discussion #92479](https://github.com/vercel/next.js/discussions/92479) in the Ideas category with the full analysis.

This was the correct channel.

## The analysis that diet enabled

Even though diet didn't execute the removal, it provided the data that made the Discussion credible:

### Usage classification

| File | Usage | Category |
|------|-------|----------|
| `run-tests.js` | `createClient` → `get`/`set` for test timings | CI infrastructure |
| `.github/actions/next-stats-action/src/add-comment.js` | `createClient` → `lrange`/`rpush`/`ltrim` | CI infrastructure |
| `.github/actions/upload-turboyet-data/src/main.js` | `createClient` → `rpush`/`set` | CI infrastructure |
| `examples/with-redis/app/actions.tsx` | `kv.hset`, `kv.zadd`, `kv.sadd` | Example code |
| `examples/with-redis/app/page.tsx` | `kv.zrange`, `kv.multi`, `kv.hgetall` | Example code |

### API mapping (1:1 compatible)

| @vercel/kv | @upstash/redis |
|---|---|
| `createClient({ url, token })` | `new Redis({ url, token })` |
| `import { kv }` (auto-env) | `Redis.fromEnv()` |
| All Redis commands | Identical |

### Key insight

3 of 5 files were CI infrastructure — not production code. The remaining 2 were example code. This means:
- **Zero production code impact** — the dependency is used only in tooling and examples
- **No API leakage** — no exported types depend on `@vercel/kv`
- **Environment variable change** only affects the example app's `.env`

Without diet's file-level coupling data, this classification would have required manual investigation.

## Barriers to external dependency removal

| Barrier | Description | Generalizable? |
|---------|-------------|----------------|
| **Lockfile regeneration** | Monorepo lockfiles require project-specific tooling and environment | Yes — any project with lockfiles |
| **CI access** | Self-hosted runners reject external fork PRs | Common in large OSS |
| **Issue templates** | `blank_issues_enabled: false` + bot enforcement auto-closes free-form issues | Varies by project |
| **Correct channel** | Issues vs Discussions vs PRs — each project has different norms | Always check first |
| **Duplicate check** | GitHub search is word-level tokenized, not semantic — multiple queries needed | Yes |

## Improvements made to diet-remove skill

Each failure led to a concrete improvement in the `/diet-remove` skill:

| Failure | Skill change |
|---------|-------------|
| PR broke CI | Issue mode is now the default; `--pr` required for direct implementation |
| Issue auto-closed | Added check for `blank_issues_enabled` and Discussion fallback |
| Didn't check for duplicates | Added mandatory multi-query duplicate search before filing |
| Single search query insufficient | Search by package name + replacement name + keywords |

## VulnCon takeaways

### 1. Detection is the easy part

uzomuzo diet found the EOL dependency in seconds. The analysis (files, calls, API mapping, replacement) took minutes with AI assistance. But **actually getting the change accepted** took multiple attempts and required understanding each project's contribution norms.

### 2. The remediation workflow matters more than the detection algorithm

A perfect detection tool that outputs "remove X, replace with Y" is useless if the remediation path is blocked by:
- Build infrastructure the contributor can't access
- Contribution policies the contributor doesn't know
- Communication channels the contributor can't find

### 3. AI can bridge the analysis gap, not the trust gap

AI (Claude Code + diet-remove skill) handled:
- ✅ Source code analysis (5 files, usage classification)
- ✅ API mapping (createClient → new Redis)
- ✅ Impact assessment (no API leakage, no production code)
- ✅ Discussion drafting (structured, data-backed)

AI could not bypass:
- ❌ CI validation (needs project environment)
- ❌ Maintainer trust (external contributor reputation)
- ❌ Project governance (issue templates, contribution norms)

### 4. The ideal workflow for external OSS remediation

```
detect (tool) → analyze (AI) → propose (Discussion/Issue) → wait → implement (maintainer or trusted contributor)
```

Not:

```
detect (tool) → implement (AI) → PR → hope CI works
```

The first workflow respects the maintainer's context. The second assumes the contributor knows everything — and fails when they don't.
