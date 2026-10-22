package analysis

import (
	"fmt"
	"strings"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
)

// ReleaseAge records whether the analysed version is within the configured
// publish window or has no trustworthy publish time. See ADR-0031.
type ReleaseAge struct {
	Status      string
	PublishedAt time.Time
	Source      string
	WindowDays  int
	Until       time.Time
	AgeDays     int
}

// EvaluateReleaseAge evaluates the caller-selected version at the scan clock.
// An older version returns nil; missing or future times return unknown.
func (a *Analysis) EvaluateReleaseAge(now time.Time, window time.Duration) *ReleaseAge {
	if a == nil || a.Package == nil || a.Package.Version == "" || window <= 0 {
		return nil
	}
	result := &ReleaseAge{Status: "unknown", WindowDays: int(window / (24 * time.Hour))}
	parsed, err := purl.NewParser().Parse(a.OriginalPURL)
	if err != nil {
		identity := strings.SplitN(a.OriginalPURL, "?", 2)[0]
		identity = strings.SplitN(identity, "#", 2)[0]
		if strings.LastIndex(identity, "@") > strings.LastIndex(identity, "/") {
			return result
		}
		return nil
	}
	if parsed.Version() == "" {
		return nil
	}
	if parsed.Version() != a.Package.Version || !strings.EqualFold(parsed.Ecosystem(), a.Package.Ecosystem) {
		return result
	}
	var at time.Time
	if a.ReleaseHistory != nil && a.ReleaseHistory.Registry == RegistryNpm {
		at = a.ReleaseHistory.PublishedAt[a.Package.Version]
		if !at.IsZero() {
			result.Source = "npm"
		}
	}
	if at.IsZero() && !a.PyPIPublishedAt.IsZero() && parsed.Ecosystem() == "pypi" {
		at = a.PyPIPublishedAt
		result.Source = "pypi"
	}
	if at.IsZero() && a.ReleaseInfo != nil && a.ReleaseInfo.RequestedVersion != nil &&
		a.ReleaseInfo.RequestedVersion.Version == a.Package.Version {
		at = a.ReleaseInfo.RequestedVersion.PublishedAt
		if !at.IsZero() {
			result.Source = "deps.dev"
		}
	}
	if at.IsZero() || at.After(now) {
		result.Source = ""
		return result
	}
	if now.Sub(at) > window {
		return nil
	}
	result.Status = "young"
	result.PublishedAt = at
	result.Until = at.Add(window)
	result.AgeDays = int(now.Sub(at) / (24 * time.Hour))
	return result
}

// ReleaseAgeNotice returns the detailed notice for a young release when no
// dormant-release marker or malicious advisory already explains it.
func (a *Analysis) ReleaseAgeNotice() string {
	if a == nil || a.ReleaseAge == nil || a.ReleaseAge.Status != "young" || a.HasDormantRelease() || a.Malicious() {
		return ""
	}
	r := a.ReleaseAge
	age := fmt.Sprintf("%d days before this scan", r.AgeDays)
	if r.AgeDays == 0 {
		age = "less than a day before this scan"
	}
	return fmt.Sprintf("This version was published on %s, %s. Malicious-package records can take up to a week to appear, so a clean malicious-package check is not yet conclusive. If you can, wait until %s before adopting it, or check who published it.",
		r.PublishedAt.UTC().Format(time.DateOnly), age, r.Until.UTC().Format(time.DateOnly))
}
