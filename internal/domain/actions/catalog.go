package actions

import (
	"sort"
	"strings"
)

// EvidenceSource is the EOLEvidence.Source value used for evidence emitted
// by this catalog. Kept exported so producers and tests reference the same
// literal and downstream consumers can filter by source.
const EvidenceSource = "ActionPinCatalog"

// DeprecatedEntry records that specific major versions of a GitHub Action
// are known to be deprecated or end-of-life by the upstream maintainer.
//
// Each entry must cite a verifiable primary source in ReferenceURL; this
// reputational guarantee is load-bearing because uzomuzo surfaces these
// claims in PR bodies against external projects.
type DeprecatedEntry struct {
	// Owner is the GitHub owner (e.g. "actions").
	Owner string
	// Repo is the GitHub repository name (e.g. "checkout").
	Repo string
	// DeprecatedMajors is the set of major-version tags (canonical "v<N>" form)
	// that are deprecated or EOL. Minor/patch pins within a deprecated major
	// are matched by MatchesMajor.
	DeprecatedMajors []string
	// Reason is a concise, date-bearing rationale suitable for PR body text
	// (e.g. "Sunset 2025-01-30 per GitHub announcement").
	Reason string
	// EOLDate is the ISO 8601 date on which the deprecation takes effect.
	// Empty for cases where the upstream did not publish a hard date.
	EOLDate string
	// SuggestedVersion is the recommended upgrade target in canonical "v<N>" form.
	SuggestedVersion string
	// ReferenceURL points to the authoritative upstream announcement.
	ReferenceURL string
}

// deprecatedActions is the initial seed catalog. Each entry has been
// verified against the primary source cited in ReferenceURL.
//
// Seed scope: actions with hard, dated EOL announcements from GitHub.
// "Soft deprecations" (Node runtime recommended-upgrade warnings without
// a removal date) are intentionally excluded to prevent false claims in
// generated PR bodies. Additional entries may be appended in follow-up
// PRs after verification.
var deprecatedActions = []DeprecatedEntry{
	{
		Owner:            "actions",
		Repo:             "upload-artifact",
		DeprecatedMajors: []string{"v1", "v2", "v3"},
		Reason:           "Sunset on 2024-11-30 (v1-v2) and 2025-01-30 (v3); artifact service v4 is the only supported version.",
		EOLDate:          "2025-01-30",
		SuggestedVersion: "v4",
		ReferenceURL:     "https://github.blog/changelog/2024-04-16-deprecation-notice-v3-of-the-artifact-actions/",
	},
	{
		Owner:            "actions",
		Repo:             "download-artifact",
		DeprecatedMajors: []string{"v1", "v2", "v3"},
		Reason:           "Sunset on 2024-11-30 (v1-v2) and 2025-01-30 (v3); artifact service v4 is the only supported version.",
		EOLDate:          "2025-01-30",
		SuggestedVersion: "v4",
		ReferenceURL:     "https://github.blog/changelog/2024-04-16-deprecation-notice-v3-of-the-artifact-actions/",
	},
	{
		Owner:            "actions",
		Repo:             "checkout",
		DeprecatedMajors: []string{"v1", "v2"},
		Reason:           "Uses Node 12, removed from GitHub-hosted runners on 2023-06-30. Action has been replaced by v4 (Node 20).",
		EOLDate:          "2023-06-30",
		SuggestedVersion: "v4",
		ReferenceURL:     "https://github.blog/changelog/2023-09-22-github-actions-transitioning-from-node-16-to-node-20/",
	},
}

func init() {
	// Deterministic lookup order: sort by Owner+Repo; majors sorted within each entry.
	sort.SliceStable(deprecatedActions, func(i, j int) bool {
		a := deprecatedActions[i]
		b := deprecatedActions[j]
		if a.Owner != b.Owner {
			return a.Owner < b.Owner
		}
		return a.Repo < b.Repo
	})
	for i := range deprecatedActions {
		majors := deprecatedActions[i].DeprecatedMajors
		sort.Strings(majors)
	}
}

// Lookup returns the catalog entry matching owner/repo/pin, if any.
// Matching semantics:
//   - owner/repo must match an entry exactly (case-insensitive).
//   - pin must be a tag ref (see IsTagRef) and resolve to one of the entry's
//     DeprecatedMajors (see MatchesMajor).
//
// Non-tag refs (branches, commit SHAs, empty) never match: callers should
// fall back to repository-level lifecycle evaluation for those pins.
func Lookup(owner, repo, pin string) (DeprecatedEntry, bool) {
	if !IsTagRef(pin) {
		return DeprecatedEntry{}, false
	}
	for _, e := range deprecatedActions {
		if !strings.EqualFold(e.Owner, owner) || !strings.EqualFold(e.Repo, repo) {
			continue
		}
		for _, major := range e.DeprecatedMajors {
			if MatchesMajor(pin, major) {
				return e, true
			}
		}
	}
	return DeprecatedEntry{}, false
}

// AllEntries returns a copy of the seed catalog for diagnostic/testing use.
// The returned slice is sorted deterministically; callers must not assume
// the order matches the source declaration.
func AllEntries() []DeprecatedEntry {
	out := make([]DeprecatedEntry, len(deprecatedActions))
	copy(out, deprecatedActions)
	return out
}
