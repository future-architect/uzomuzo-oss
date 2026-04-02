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

// labelMap maps CLI label strings to domain MaintenanceStatus values.
var labelMap = map[string]analysis.MaintenanceStatus{
	"eol-confirmed": analysis.LabelEOLConfirmed,
	"eol-effective": analysis.LabelEOLEffective,
	"eol-scheduled": analysis.LabelEOLScheduled,
	"stalled":       analysis.LabelStalled,
	"legacy-safe":   analysis.LabelLegacySafe,
}

// ValidFailLabels returns the list of valid --fail-on label strings.
func ValidFailLabels() []string {
	return []string{"eol-confirmed", "eol-effective", "eol-scheduled", "stalled", "legacy-safe"}
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
		if p.IsTriggered(lr.Label) {
			return true
		}
	}
	return false
}
