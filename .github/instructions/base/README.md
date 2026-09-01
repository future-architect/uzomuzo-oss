# Shared instruction base

Rule files in here are **not only this repository's**. They are the canonical
copy that sibling repositories in this organization sync from. Everything
directly under `.github/instructions/` (outside `base/`) is local to this
repository and is never synced.

Editing a file in here changes what other repositories will be told to do.

## Profiles

One directory per profile. A consuming repository opts in to a profile; files in
a profile it did not select are **not candidates at all**, so no per-file "skip"
entry has to be remembered.

| Profile | Contents |
|---|---|
| `core/` | Language policy, coding standards, error handling, git workflow, security |
| `arch-ddd/` | DDD layering and its enforcement rules |

**Architecture is a separate profile because consumers do not all share one
architecture.** If DDD lived in `core/`, the only thing standing between a
repository built on a different pattern and a document telling it to follow DDD
would be someone remembering to write an opt-out — and because that document is
generated, the mistake would be easy to miss in review. Opt-in by profile
removes the chance to forget.

For the same reason, a profile is not created speculatively for every pattern
someone might use. A profile needs consumers, per the bar below; an almost-empty
profile is an invitation to dump things into it.

## Admission bar

A file belongs in `base/` only when **two or more repositories would inherit it
verbatim**. Until then it stays local to the repository that needs it. The same
bar applies to creating a profile.

A pull request that adds a file here must say how many repositories will consume
it. "We might share this later" is not a reason — move it when the second
consumer actually exists.

## What must stay out

- Anything naming this repository's own packages, commands, or domain types.
  Those belong in the repository-local files outside `base/`.
- **Anything about a non-public repository** — its name, its internal
  conventions, its architecture, or quotations from its documents. This
  repository is public, so every byte here is published the moment it is
  committed. Describe consumers generically ("a consuming repository") and keep
  the specifics on the consuming side.
- Anything else that cannot be published.

## Editing

`base/` is a source, not an output. Edit the files here directly, then run
`make sync-instructions` to regenerate `.claude/rules/*.md` and `AGENTS.md`. CI
(`instruction-sync-freshness.yml`) fails a pull request whose generated files do
not match.
