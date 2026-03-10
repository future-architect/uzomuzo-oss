// Package eolphrases provides the canonical EOL / deprecation keyword definitions
// used across Domain and Infrastructure layers.
//
// Every phrase is assigned a Tier that determines its usage scope:
//   - TierStrong:  auto-detect as project-level EOL (detector.go KindStrong)
//   - TierReeval:  trigger not_eol re-evaluation (evidence.go)
//   - TierSnippet: include in evidence snippets for LLM / human review
//
package eolphrases

import "strings"

// Tier represents the confidence / usage level of a phrase.
type Tier int

const (
	// TierSnippet is the lowest tier — phrases used only for snippet extraction.
	TierSnippet Tier = iota + 1
	// TierReeval triggers re-evaluation of existing not_eol records.
	TierReeval
	// TierStrong enables auto-detection as project-level EOL.
	TierStrong
)

type entry struct {
	text string
	tier Tier
}

// catalog is the single source of truth for all EOL-related phrases.
// All entries are lower-case for case-insensitive matching.
var catalog = []entry{
	// ── TierStrong ─────────────────────────────────────────
	// These phrases strongly indicate project-level EOL.
	{text: "no longer maintained", tier: TierStrong},
	{text: "this project has been discontinued", tier: TierStrong},
	{text: "no longer supported", tier: TierStrong},
	{text: "deprecated permanently", tier: TierStrong},
	{text: "superseded by", tier: TierStrong},
	{text: "replaced by", tier: TierStrong},
	{text: "retired", tier: TierStrong},
	// Issue #17 additions
	{text: "decommissioned", tier: TierStrong},
	{text: "development halted", tier: TierStrong},
	{text: "end of maintenance", tier: TierStrong},
	{text: "maintenance ended", tier: TierStrong},

	// ── TierReeval ─────────────────────────────────────────
	// These phrases trigger re-evaluation when found in new evidence.
	{text: "deprecated", tier: TierReeval},
	{text: "end of life", tier: TierReeval},
	{text: "end-of-life", tier: TierReeval},
	{text: "end of support", tier: TierReeval},
	{text: "end-of-support", tier: TierReeval},
	{text: "unmaintained", tier: TierReeval},
	{text: "abandoned", tier: TierReeval},
	{text: "discontinued", tier: TierReeval},
	// Issue #17 additions
	{text: "sunset", tier: TierReeval},
	{text: "no further development", tier: TierReeval},
	{text: "no further updates", tier: TierReeval},
	{text: "will not be updated", tier: TierReeval},

	// ── TierSnippet ────────────────────────────────────────
	// Phrases used for evidence snippet extraction only.
	// Migrated from eoldiscovery/snippets.go lifecyclePhrases.
	{text: "deprecation", tier: TierSnippet},
	{text: "deprecating", tier: TierSnippet},
	{text: " eol", tier: TierSnippet},
	{text: "eol ", tier: TierSnippet},
	{text: " eol.", tier: TierSnippet},
	{text: " eol,", tier: TierSnippet},
	{text: "unmaintain", tier: TierSnippet},
	{text: "no longer updated", tier: TierSnippet},
	{text: "archived", tier: TierSnippet},
	{text: "this repository is archived", tier: TierSnippet},
	{text: "migrated to", tier: TierSnippet},
	{text: "moved to", tier: TierSnippet},
	{text: "renamed to", tier: TierSnippet},
	{text: "maintenance mode", tier: TierSnippet},
	{text: "maintenance-mode", tier: TierSnippet},
	{text: "will be deprecated", tier: TierSnippet},
	{text: "relocated", tier: TierSnippet},
	{text: "relocation", tier: TierSnippet},
	// Issue #17 additions
	{text: "final release", tier: TierSnippet},
	{text: "sunsetting", tier: TierSnippet},
	{text: "project sunset", tier: TierSnippet},
	{text: "obsolete", tier: TierSnippet},
	{text: "obsoleted", tier: TierSnippet},
	{text: "read-only mode", tier: TierSnippet},
	{text: "minimal maintenance", tier: TierSnippet},
	{text: "limited maintenance", tier: TierSnippet},
	{text: "maintenance updates only", tier: TierSnippet},
	{text: "bugfix only", tier: TierSnippet},
	{text: "community driven", tier: TierSnippet},
	{text: "community maintained", tier: TierSnippet},
	{text: "successor is", tier: TierSnippet},
	{text: "consolidated into", tier: TierSnippet},
	{text: "integrated into", tier: TierSnippet},
	{text: "will be removed", tier: TierSnippet},
	{text: "will be retired", tier: TierSnippet},
	{text: "will be sunset", tier: TierSnippet},
	{text: "support until", tier: TierSnippet},
	{text: "security fixes until", tier: TierSnippet},
}

// TextsAtTier returns all phrase texts whose tier is >= minTier.
// The returned slice is a fresh copy safe to modify.
func TextsAtTier(minTier Tier) []string {
	var out []string
	for _, e := range catalog {
		if e.tier >= minTier {
			out = append(out, e.text)
		}
	}
	return out
}

// Phrases returns all phrases at TierReeval or above.
func Phrases() []string {
	return TextsAtTier(TierReeval)
}

// ContainsStrongPhrase returns the de-duplicated set of TierReeval+ phrases
// found in text (case-insensitive). Returns nil when no match.
// Backward compatible with the original signature.
func ContainsStrongPhrase(text string) []string {
	if text == "" {
		return nil
	}
	lower := strings.ToLower(text)
	var found []string
	for _, e := range catalog {
		if e.tier >= TierReeval && strings.Contains(lower, e.text) {
			found = append(found, e.text)
		}
	}
	return found
}

// AllEntries returns a copy of the full catalog for inspection (e.g. keyword-stats).
func AllEntries() []struct {
	Text string
	Tier Tier
} {
	out := make([]struct {
		Text string
		Tier Tier
	}, len(catalog))
	for i, e := range catalog {
		out[i] = struct {
			Text string
			Tier Tier
		}{Text: e.text, Tier: e.tier}
	}
	return out
}

// TierString returns a human-readable label for the tier.
func (t Tier) String() string {
	switch t {
	case TierSnippet:
		return "snippet"
	case TierReeval:
		return "reeval"
	case TierStrong:
		return "strong"
	default:
		return "unknown"
	}
}
