# Shared instruction base

Rule files in here are **not only uzomuzo-oss's**. They are the canonical copy
for the sibling repositories too — `uzomuzo-catalog` and `vuls-reach` sync from
this directory. Everything directly under `.github/instructions/` (outside
`base/`) is uzomuzo-oss-local and is never synced.

Editing a file in here changes what other repositories will be told to do.

## Profiles

One directory per profile. A consuming repository opts in to a profile; files in
a profile it did not select are **not candidates at all**, so no per-file "skip"
entry has to be remembered.

| Profile | Contents | Consumers |
|---|---|---|
| `core/` | Language policy, coding standards, error handling, git workflow, security | every repository |
| `arch-ddd/` | DDD layering and its enforcement rules | uzomuzo-oss, uzomuzo-catalog |

`arch-ddd` is a separate profile for a specific reason: **vuls-reach is Ports &
Adapters (Hexagonal), not DDD** — its own rules open by saying so. Its
architecture document stays local to that repository. If DDD lived in `core/`, a
missing opt-out would silently hand a Hexagonal repository a document telling it
to follow DDD, and because that document is generated, the mistake would be easy
to miss in review.

There is deliberately no `arch-hexagonal` profile: one consumer does not meet the
admission bar below, and an almost-empty profile is an invitation to dump things
in it.

## Admission bar

A file belongs in `base/` only when **two or more repositories would inherit it
verbatim**. Until then it stays local to the repository that needs it. The same
bar applies to creating a profile.

A pull request that adds a file here must name the repositories that will consume
it. "We might share this later" is not a reason — move it when the second
consumer actually exists.

## What must stay out

- Anything naming this repository's own packages, commands, or domain types.
  Those belong in the repository-local files outside `base/`.
- Anything that cannot be published: uzomuzo-oss is a **public** repository, so
  every byte in here is public the moment it is committed, including for the
  private repositories that consume it.

## Editing

`base/` is a source, not an output. Edit the files here directly, then run
`make sync-instructions` to regenerate `.claude/rules/*.md` and `AGENTS.md`. CI
(`instruction-sync-freshness.yml`) fails a pull request whose generated files do
not match.
