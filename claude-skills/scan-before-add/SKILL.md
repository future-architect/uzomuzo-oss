---
name: scan-before-add
description: "Use when selecting, comparing, or recommending third-party libraries, and before adding a new dependency (go.mod, package.json, pyproject.toml, requirements.txt, Cargo.toml, pom.xml, *.csproj, Gemfile, composer.json). Applies both when you choose the candidates and when the user names a package. Runs uzomuzo scan on the candidates and leaves a record of what was checked and who decided."
argument-hint: "<purpose> [-- <candidate PURLs with versions>]"
---

# Scan Before Add: $ARGUMENTS

Scan the candidates with uzomuzo before a dependency is added, and leave a record of what was checked and who decided.

When an AI agent (or a person) adds a library, the change usually shows *which* library was added but not *what was checked* before adding it. This skill runs `uzomuzo scan` on the candidates and writes that down: the verdict, the evidence uzomuzo printed, what could not be fetched, who proposed each candidate, who chose, and whether a person has reviewed the choice.

**Contract**: uzomuzo returns maintenance evidence. You choose and add a dependency only within the authority the person gave you. The record keeps uzomuzo's output and your own reading apart, and states whether a person has reviewed the choice. This skill never turns a verdict into an approval.

## Prerequisites

- `uzomuzo` on `PATH` (see [Installation](https://github.com/future-architect/uzomuzo-oss#installation)). Check with `uzomuzo --version`.
- Network access: uzomuzo queries deps.dev, OSV, and GitHub.
- `GITHUB_TOKEN` is optional. Without it, archive detection relies on the Scorecard "Maintained" check only (no direct GitHub API check), and the last human commit date, the human/bot commit ratio, and fork detection are not available ([details](https://github.com/future-architect/uzomuzo-oss/blob/main/docs/usage.md#analysis-precision-and-github_token)). **The verdict for the same package can differ** -- a repository archived on GitHub has come back as `caution` without a token and `replace` with one. Record whether the token was set ("unknown" if you cannot check; do not infer it from rate-limit numbers); never print its value.

## Step 1: Establish the purpose and the candidates

1. **Purpose** -- one sentence on what the dependency is for, in the person's words.
2. **Requirements** -- conditions the person already stated (license, ecosystem, "must be maintained", and so on). Write "none stated" if there are none.
3. **Authority** -- write down three things separately; later steps depend on them: (a) did the person name the package or ask you to choose one, (b) did the person ask you to **add** it to the manifest, or only to recommend, and (c) did the person tell you not to ask questions.
4. **Candidates**:
   - If the person named a package, that package is the candidate. You may add alternatives you think are worth comparing.
   - If you are choosing, pick the candidates that meet the purpose and scan **all of them before you pick one**, not only the one you end up adding. Consider whether the standard library already covers the purpose. If you scan only one candidate, write down why.
5. **PURLs**:
   - Use the name the package manager uses, not the import name: `pkg:pypi/pyyaml@6.0.1`, not `pkg:pypi/yaml`.
   - Keep Go major-version suffixes: `pkg:golang/github.com/Masterminds/semver/v3@v3.2.1`.
   - Encode npm scopes: `pkg:npm/%40types/node@20.11.0`.
   - If a package name is ambiguous, ask rather than guess (unless told not to ask; then state your interpretation in the record).

## Step 2: Run the scan

```bash
uzomuzo --version
uzomuzo scan <purl> [<purl> ...] --format json
```

- Scanning PURLs without a version is fine while exploring candidates. **Before adding a dependency, decide the version you will pin and scan that exact PURL** so the record matches what lands in the manifest.
- Run the final candidates in one call so they share the same timestamp. Note the date and time, the uzomuzo version, and whether `GITHUB_TOKEN` was set.
- If the command fails (network error, rate limit), record "verdict could not be obtained" for the affected candidates. Do not substitute a verdict of your own.
- `--format detailed` adds release dates, the last human commit date, Scorecard values, and the Registry link. Step 4 says when it is required; you may also run it for other checks (for example, the Registry link of a `review` package).

## Step 3: Read the result, candidate by candidate

For each entry in `packages`:

- **Copy these fields exactly as printed**: `purl`, `verdict`, `lifecycle`, `reason`, `archived`, `repo_url`, `stable_version`, `version_license`. Do not translate or soften them. Write "not printed" for a field that is absent.
- **`review` means uzomuzo could not decide.** It is not `ok`. A misspelled or nonexistent package name also comes back as `review` / `Review Needed` with exit code 0 -- check that the package exists in its registry (the Registry link in `--format detailed`) before reading anything else. If it does not exist, fix the PURL and rerun.
- **`stable_version` is the latest stable release, which may not be the version you pin.** The advisory fields (`advisory_count`, `max_advisory_severity`, and so on) describe the release uzomuzo picks as latest -- stable, else the highest version, else a pre-release, else the requested version -- not necessarily yours. Unless you check the pinned version separately, write "advisories not checked for the pinned version".
- **A `caution` candidate may be `caution` because of its dependencies' advisories, not its own.** For each `caution` candidate also copy `transitive_advisory_count`, `max_transitive_advisory_severity`, and, when present, `dependency_advisory_age` (`status` and `advisory_id`). `status: "unknown"` means the OSV lookup failed and the verdict might have been `replace`; record it under "Not obtained".
- **Copy `dependencies` for every candidate** (`version`, `direct`, `transitive`), or "not printed" when it is absent. It describes the dependency graph of that `version`, which may not be the version you pin. An absent field means uzomuzo did not report the counts (for example, cargo crates, ecosystems deps.dev has no graph for, or a graph deps.dev could not resolve); do not fill it in from memory.
- **`version_license` is looked up for the requested version**, falling back to the project license when the version's license cannot be resolved. Copy it as printed.
- **Detailed output is evidence, not a second verdict.** Scorecard scores, stars, and the other numbers in `--format detailed` may be copied, but do not rank candidates by them and do not let them override `verdict` or `lifecycle`. uzomuzo can return `Active` because of a recent release while also printing `Maintained Score: 0/10`; these are different indicators. Copy both as printed and write "The relationship between these indicators was not investigated." A JSON `overall_score` of `0` together with `(unavailable)` in the detailed report means the score was not measured; copy "(unavailable)".
- **Missing field vs. failed lookup**: many JSON fields are omitted when their value is zero or empty (for example, `advisory_count` is omitted when it is 0; `dependencies` is the exception and prints a measured `0`). An omitted field is "not printed", not a failure. Record "not obtained" only when there is explicit evidence: an `error` field, a warning on stderr, a failed command, or no token for token-only signals.

## Step 4: Choose, or stop and ask

- **Before you choose: if two or more candidates came back `ok` or `caution`, run `uzomuzo scan <purl> <purl> ... --format detailed` once, with all of those candidates in the same call.** The JSON output has no release date and no commit date; the detailed report has both. For each of these candidates copy two lines into the second table of the record: `Releases` -> `Stable` (version and date) and `Signals` -> `Last Human Commit`. Copy the value as printed, including `(unavailable)`; when `GITHUB_TOKEN` is not set, write `(unavailable; no token)`. Write "not printed" only when the line itself is absent, and "not obtained" with the error when the command fails. If you leave an `ok` or `caution` candidate out of this comparison because it does not fit the purpose, write the specific reason in the record; preference alone is not a reason.
- You may use these dates when you choose, but only in "Reason for the decision" and only labelled as your reading. You may likewise use the `dependencies` counts. Do not choose a candidate only because its date is newer or its dependency counts are smaller, and do not rank candidates by Scorecard values, stars, or any other number in the detailed report.
- **Fit and maintenance are separate.** Do not treat `ok` as the winner by default: a candidate must first fit the purpose. Numbers you read (dependents, scores) are facts; what you infer from them is your reading -- keep the two apart.
- **You were asked only to recommend** (no permission to change the manifest): recommend and write the record. Do not add anything.
- **You were asked to choose and add, and not to ask questions**: choose, scan the pinned version, add it, and record "Decided by: AI agent (delegated by the person; not reviewed by a person yet)".
- **You were asked to choose and add, but may ask**: show the record with your recommendation and stop before adding. Leave "Decided by" as "not decided yet" until the person answers.
- **The person named a package and it came back `replace` or `caution`**: the verdict does not block the addition. If you may ask, show the verdict, the reason, and an alternative (another candidate or the standard library) and wait. If you were told not to ask, add it, put the verdict and the reason at the top of your reply, and record "Decided by: the person named it; the agent added it after a `<verdict>` verdict (not reviewed by a person yet)".
- If the project already has its own rules for adding dependencies, follow them.

## Step 5: Write the record

```markdown
## Dependency selection record

- Purpose:
- Requirements stated beforehand:
- Authority given to the agent: (named the package / asked to choose / told not to ask)
- Checked on: <date time, timezone>
- uzomuzo version:
- GITHUB_TOKEN set: yes | no | unknown (could not check)

| Candidate (PURL) | Proposed by | verdict | lifecycle | reason | archived | repository | stable_version | version_license |
|---|---|---|---|---|---|---|---|---|
|  |  |  |  |  |  |  |  |  |

Candidates compared with `--format detailed` (required when two or more candidates were `ok` or `caution`):

| Candidate (PURL) | Stable release (version, date) | Last human commit |
|---|---|---|
|  |  |  |

(If not run, write instead: "detailed not run: fewer than two candidates were ok or caution".)

- Dependencies of each candidate, from the JSON scan: (per candidate: `dependencies` version, direct, transitive, or "not printed")
- Dependency advisories of `caution` candidates: (per candidate: transitive_advisory_count, max_transitive_advisory_severity, dependency_advisory_age status and advisory_id, or "not printed")
- Indicator notes:
- Not obtained:
- Not checked for the pinned version:

`ok` is a maintenance verdict, not a safety verdict.
The accuracy of these verdicts has not been compared with human judgement.

- Decided by:
- Reviewed by a person: yes | not yet
- Reason for the decision: (the decider's words; if the agent decided, label it as the agent's reading, not uzomuzo's)
```

**Where the record goes**: show it in the chat. If you are also creating the commit or PR that adds the dependency, put the record in that commit message body or PR description, next to the change. Otherwise do not create files; say where it has not been saved. Never edit an existing PR, issue, or other shared place without showing the full text and the target and getting explicit approval.

## Rules

- Report uzomuzo's words (`ok` / `caution` / `replace` / `review` and the lifecycle labels). Do not map them to "approved", "safe", "rejected", or a score.
- Keep the two fixed sentences in every record.
- Keep facts from uzomuzo and your own reading separate. Label your comments as yours.
- Never record that a person reviewed or approved a choice unless they did.
