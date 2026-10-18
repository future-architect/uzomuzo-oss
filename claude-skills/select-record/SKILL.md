---
name: select-record
description: "Before adding a dependency, run uzomuzo scan on each candidate and leave a record of what was checked and who decided"
argument-hint: "<purpose> -- <one or more PURLs with versions, written as they will appear in the manifest>"
---

# Select Record: $ARGUMENTS

Leave a record, next to the change, of what was checked before a dependency was adopted.

When an AI agent (or a person) proposes a library, the change usually shows *which* library was added but not *what was checked* before adding it. This skill runs `uzomuzo scan` on each candidate and writes that down: the verdict, the evidence uzomuzo printed, what could not be fetched, and who made the decision.

**This skill does not choose, approve, or install anything.** It records facts that uzomuzo printed and leaves the decision to a person.

**When to use**: Before a new dependency lands in `go.mod`, `package.json`, `pyproject.toml`, `Cargo.toml`, `pom.xml`, `*.csproj`, `Gemfile`, or `composer.json` -- whether you picked it or an AI agent proposed it.

## Prerequisites

- `uzomuzo` on `PATH` (see the [Quick Start](../../README.md)). Check with `uzomuzo --version`.
- Network access: uzomuzo queries deps.dev, OSV, and GitHub.
- `GITHUB_TOKEN` is optional. Without it, uzomuzo cannot fetch the archived flag, the last human commit, or fork detection, and **the verdict for the same package can differ** (a repository that is archived on GitHub can come back as `caution` instead of `replace`). Record whether the token was set; never print its value.

## Step 1: Confirm the inputs

Collect these before running anything. Ask the person for anything missing instead of guessing.

1. **Purpose** -- one sentence on what the dependency is for, in the person's words.
2. **Requirements** -- any conditions the person already stated (license, ecosystem, "must be maintained", and so on). Write "none stated" if there are none.
3. **Who proposed each candidate** -- the person, or the AI agent.
4. **Candidates as PURLs, with the version that will be requested.**
   - Use the name the package manager uses, not the import name: `pkg:pypi/pyyaml@6.0.1`, not `pkg:pypi/yaml`.
   - Keep Go major-version suffixes: `pkg:golang/github.com/Masterminds/semver/v3@v3.2.1`.
   - Encode npm scopes: `pkg:npm/%40types/node@20.11.0`.
   - If the name or version is ambiguous, show the PURL you would use and ask before running. Do not pick a version silently.

## Step 2: Run the scan

```bash
uzomuzo --version
uzomuzo scan <purl> [<purl> ...] --format json
```

- Run all candidates in one call so they share the same timestamp.
- Note the date and time of the run, the uzomuzo version, and whether `GITHUB_TOKEN` was set.
- If the command fails (network error, rate limit), record "verdict could not be obtained" for the affected candidates. Do not substitute a verdict of your own.

If you need the evidence behind a verdict (release dates, Scorecard values, the Registry link), also run `uzomuzo scan <purl> --format detailed` for that candidate.

## Step 3: Read the result, candidate by candidate

For each entry in `packages`:

- **Copy these fields exactly as printed**: `purl`, `verdict`, `lifecycle`, `reason`, `archived`, `repo_url`, `stable_version`, `version_license`. Do not translate or soften them.
- **`review` means uzomuzo could not decide.** It is not `ok`. A misspelled or nonexistent package name also comes back as `review` / `Review Needed` with exit code 0 -- check that the package exists in its registry (the Registry link in `--format detailed`) before reading anything else. If it does not exist, fix the PURL and rerun.
- **`stable_version` is the latest stable release, which may not be the version you requested.** Vulnerability and license information in the output may describe that release, not yours. If you need advisories for the requested version, say so as "not checked for the requested version" unless you check it separately.
- **List what was not obtained**: no token, a failed request, a field that is empty. An empty field is "not obtained", never "none found".

## Step 4: Write the record

Fill in the template below. **Do not fill in "Decided by" or "Reason for the decision" yourself** -- ask the person, and leave them as "not decided yet" if there is no answer.

Put the record where it will be found later: the PR description that adds the dependency, or a file the person names.

```markdown
## Dependency selection record

- Purpose:
- Requirements stated beforehand:
- Checked on: <date time, timezone>
- uzomuzo version:
- GITHUB_TOKEN set: yes | no

| Candidate (PURL) | Proposed by | verdict | lifecycle | reason | archived | repository |
|---|---|---|---|---|---|---|
|  |  |  |  |  |  |  |

- Not obtained:
- Not checked for the requested version:

`ok` is a maintenance verdict, not a safety verdict.
The accuracy of these verdicts has not been compared with human judgement.

- Decided by:
- Reason for the decision (in the decider's words):
```

## Rules

- Report uzomuzo's words (`ok` / `caution` / `replace` / `review` and the lifecycle labels). Do not map them to "approved", "safe", "rejected", or a score.
- Keep the two fixed sentences in every record.
- Keep facts from uzomuzo and your own reading separate. If you add a comment, label it as yours.
- If a candidate comes back `replace` or `caution`, you may suggest what to check next (another candidate, the standard library, writing a small part yourself), but the choice stays with the person.
