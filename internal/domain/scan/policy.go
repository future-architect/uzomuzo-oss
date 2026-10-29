// Package scan defines the fail-on policy for the unified scan command.
//
// DDD Layer: Domain (pure business logic, no I/O)
package scan

import (
	"fmt"
	"strings"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	domainaudit "github.com/future-architect/uzomuzo-oss/internal/domain/audit"
)

// Trigger identifies a matched fail-on policy condition.
type Trigger string

const (
	// FailEOLConfirmed selects the eol-confirmed condition.
	FailEOLConfirmed Trigger = "eol-confirmed"
	// FailEOLEffective selects the eol-effective condition.
	FailEOLEffective Trigger = "eol-effective"
	// FailEOLScheduled selects the eol-scheduled condition.
	FailEOLScheduled Trigger = "eol-scheduled"
	// FailStalled selects the stalled condition.
	FailStalled Trigger = "stalled"
	// FailLegacySafe selects the legacy-safe condition.
	FailLegacySafe Trigger = "legacy-safe"
	// FailReviewNeeded selects the review-needed condition.
	FailReviewNeeded Trigger = "review-needed"
	// FailMalicious selects the malicious condition.
	FailMalicious Trigger = "malicious"
	// FailDormantRelease selects the dormant-release condition.
	FailDormantRelease Trigger = "dormant-release"
	// FailArchived selects the archived condition.
	FailArchived Trigger = "archived"
	// FailDisabled selects the disabled condition.
	FailDisabled Trigger = "disabled"
)

// failLabels is the single source of the --fail-on vocabulary: which CLI label
// strings exist, which MaintenanceStatus each names, and the order they appear
// in. The order is deliberate, not sorted — it feeds user-visible help and error
// text, so it runs by descending severity and ends with the labels that are not
// a severity at all.
//
// One literal rather than a list plus a map: the vocabulary used to be written
// out three times, and a label added to one copy but not the others is how
// review-needed became reachable but not gatable (#498).
var failLabels = []struct {
	label  string
	status analysis.MaintenanceStatus
}{
	{string(FailEOLConfirmed), analysis.LabelEOLConfirmed},
	{string(FailEOLEffective), analysis.LabelEOLEffective},
	{string(FailEOLScheduled), analysis.LabelEOLScheduled},
	{string(FailStalled), analysis.LabelStalled},
	{string(FailLegacySafe), analysis.LabelLegacySafe},
	{string(FailReviewNeeded), analysis.LabelReviewNeeded},
}

// labelMap indexes failLabels for lookup while parsing.
var labelMap = func() map[string]analysis.MaintenanceStatus {
	m := make(map[string]analysis.MaintenanceStatus, len(failLabels))
	for _, fl := range failLabels {
		m[fl.label] = fl.status
	}
	return m
}()

// failFacts lists the fact values in display order, after the labels.
var failFacts = []string{string(FailMalicious), string(FailDormantRelease), string(FailArchived), string(FailDisabled)}

// ValidFailLabels returns the valid --fail-on label strings in display order.
func ValidFailLabels() []string {
	out := make([]string, 0, len(failLabels)+len(failFacts))
	for _, fl := range failLabels {
		out = append(out, fl.label)
	}
	out = append(out, failFacts...)
	return out
}

// FailPolicy determines which lifecycle labels or facts (malicious advisories,
// dormant-release markers, archived or disabled repositories) trigger a non-zero exit.
// Zero value (empty triggers) means nothing triggers failure.
type FailPolicy struct {
	triggers  map[analysis.MaintenanceStatus]struct{}
	malicious bool
	// dormant is set only by an explicit "dormant-release"; no other trigger
	// implies it. See ADR-0028.
	dormant bool
	// archived and disabled are set only by their own explicit value and match
	// the repository state whatever the label or verdict. See ADR-0029.
	archived bool
	disabled bool
}

// ParseFailPolicy parses a comma-separated --fail-on string into a FailPolicy.
// Returns an error if any label is unrecognized.
func ParseFailPolicy(raw string) (FailPolicy, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return FailPolicy{}, nil
	}

	parts := strings.Split(raw, ",")
	triggers := make(map[analysis.MaintenanceStatus]struct{}, len(parts))
	malicious := false
	dormant := false
	archived := false
	disabled := false
	for _, part := range parts {
		label := strings.TrimSpace(strings.ToLower(part))
		if label == "" {
			continue
		}
		if label == string(FailMalicious) {
			malicious = true
			continue
		}
		if label == string(FailDormantRelease) {
			dormant = true
			continue
		}
		if label == string(FailArchived) {
			archived = true
			continue
		}
		if label == string(FailDisabled) {
			disabled = true
			continue
		}
		ms, ok := labelMap[label]
		if !ok {
			return FailPolicy{}, fmt.Errorf("invalid --fail-on label %q; valid labels: %s",
				label, strings.Join(ValidFailLabels(), ", "))
		}
		triggers[ms] = struct{}{}
	}
	// Any non-empty selection also fails on a malicious version, including one
	// that names only a fact such as dormant-release or archived (ADR-0027).
	if len(triggers) > 0 || dormant || archived || disabled {
		malicious = true
	}
	return FailPolicy{triggers: triggers, malicious: malicious, dormant: dormant, archived: archived, disabled: disabled}, nil
}

// IsEmpty returns true when no triggers are configured.
func (p FailPolicy) IsEmpty() bool {
	return len(p.triggers) == 0 && !p.malicious && !p.dormant && !p.archived && !p.disabled
}

// NeedsRepositoryState reports whether the policy selects archived or disabled,
// which can only be decided from repository state read with a GitHub token.
// Without one, IsDisabled is never set and IsArchived only through Scorecard,
// so such a policy would pass entries it never checked.
func (p FailPolicy) NeedsRepositoryState() bool {
	return p.archived || p.disabled
}

// IsTriggered returns true if the given label is in the fail set.
func (p FailPolicy) IsTriggered(label analysis.MaintenanceStatus) bool {
	if p.triggers == nil {
		return false
	}
	_, ok := p.triggers[label]
	return ok
}

// Evaluate checks whether any audit entry matches the fail policy.
// Returns true if at least one entry matches: its lifecycle label is in the trigger set, or it carries a fact the policy selects.
func (p FailPolicy) Evaluate(entries []domainaudit.AuditEntry) bool {
	for i := range entries {
		if len(p.Matches(&entries[i])) > 0 {
			return true
		}
	}
	return false
}

// Matches returns every selected condition matched by an entry, in fact then label order.
func (p FailPolicy) Matches(e *domainaudit.AuditEntry) []Trigger {
	if p.IsEmpty() || e == nil || e.Analysis == nil {
		return nil
	}
	var matches []Trigger
	if p.malicious && e.Analysis.Malicious() {
		matches = append(matches, FailMalicious)
	}
	if p.dormant && e.Analysis.HasDormantRelease() {
		matches = append(matches, FailDormantRelease)
	}
	if p.archived && e.Analysis.IsArchived() {
		matches = append(matches, FailArchived)
	}
	if p.disabled && e.Analysis.IsDisabled() {
		matches = append(matches, FailDisabled)
	}
	if lr := e.Analysis.GetLifecycleResult(); lr != nil && p.IsTriggered(analysis.MaintenanceStatus(lr.Label)) {
		for _, label := range failLabels {
			if label.status == analysis.MaintenanceStatus(lr.Label) {
				matches = append(matches, Trigger(label.label))
				break
			}
		}
	}
	return matches
}
