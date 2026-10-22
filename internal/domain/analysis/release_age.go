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
	parsed, err := purl.NewParser().Parse(a.OriginalPURL)
	if err != nil || parsed.Version() == "" || parsed.Version() != a.Package.Version || !strings.EqualFold(parsed.Ecosystem(), a.Package.Ecosystem) {
		return nil
	}
	result := &ReleaseAge{Status: "unknown", WindowDays: int(window / (24 * time.Hour))}
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
	return fmt.Sprintf("This version was published on %s, %d days before this scan. Malicious-package records usually appear within a week of publication, so none could have named it yet. If you can, wait until %s before adopting it, or check who published it.",
		r.PublishedAt.UTC().Format(time.DateOnly), r.AgeDays, r.Until.UTC().Format(time.DateOnly))
}
