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
	{"eol-confirmed", analysis.LabelEOLConfirmed},
	{"eol-effective", analysis.LabelEOLEffective},
	{"eol-scheduled", analysis.LabelEOLScheduled},
	{"stalled", analysis.LabelStalled},
	{"legacy-safe", analysis.LabelLegacySafe},
	{"review-needed", analysis.LabelReviewNeeded},
}

// labelMap indexes failLabels for lookup while parsing.
var labelMap = func() map[string]analysis.MaintenanceStatus {
	m := make(map[string]analysis.MaintenanceStatus, len(failLabels))
	for _, fl := range failLabels {
		m[fl.label] = fl.status
	}
	return m
}()

// ValidFailLabels returns the valid --fail-on label strings in display order.
func ValidFailLabels() []string {
	out := make([]string, 0, len(failLabels))
	for _, fl := range failLabels {
		out = append(out, fl.label)
	}
	return out
}

// FailPolicy determines which lifecycle labels trigger a non-zero exit.
// Zero value (empty triggers) means nothing triggers failure.
type FailPolicy struct {
	triggers map[analysis.MaintenanceStatus]struct{}
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
	for _, part := range parts {
		label := strings.TrimSpace(strings.ToLower(part))
		if label == "" {
			continue
		}
		ms, ok := labelMap[label]
		if !ok {
			return FailPolicy{}, fmt.Errorf("invalid --fail-on label %q; valid labels: %s",
				label, strings.Join(ValidFailLabels(), ", "))
		}
		triggers[ms] = struct{}{}
	}
	return FailPolicy{triggers: triggers}, nil
}

// IsEmpty returns true when no triggers are configured.
func (p FailPolicy) IsEmpty() bool {
	return len(p.triggers) == 0
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
// Returns true if at least one entry's lifecycle label is in the trigger set.
func (p FailPolicy) Evaluate(entries []domainaudit.AuditEntry) bool {
	if p.IsEmpty() {
		return false
	}
	for i := range entries {
		e := &entries[i]
		if e.Analysis == nil {
			continue
		}
		lr := e.Analysis.GetLifecycleResult()
		if lr == nil {
			continue
		}
		if p.IsTriggered(analysis.MaintenanceStatus(lr.Label)) {
			return true
		}
	}
	return false
}
