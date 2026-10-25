package analysis

import (
	"fmt"
	"strings"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
)

// ReleaseAgeStatus names the state recorded in a ReleaseAge.
type ReleaseAgeStatus string

const (
	// ReleaseAgeYoung means the version was published within the window.
	ReleaseAgeYoung ReleaseAgeStatus = "young"
	// ReleaseAgeUnknown means no trustworthy publish time was available.
	ReleaseAgeUnknown ReleaseAgeStatus = "unknown"
)

// ReleaseAgeSource names where a young version's publish time came from.
type ReleaseAgeSource string

const (
	// ReleaseAgeSourceNpm is the npm packument's time[version].
	ReleaseAgeSourceNpm ReleaseAgeSource = "npm"
	// ReleaseAgeSourcePyPI is the earliest file upload in a PyPI response.
	ReleaseAgeSourcePyPI ReleaseAgeSource = "pypi"
	// ReleaseAgeSourceDepsDev is deps.dev's requested-version release time.
	ReleaseAgeSourceDepsDev ReleaseAgeSource = "deps.dev"
)

// ReleaseAge records whether the analysed version is within the configured
// publish window or has no trustworthy publish time. A nil pointer means the
// version is older than the window, or the input names no version. See ADR-0031.
type ReleaseAge struct {
	// Status is ReleaseAgeYoung or ReleaseAgeUnknown.
	Status ReleaseAgeStatus
	// PublishedAt is the publish time. Zero when Status is ReleaseAgeUnknown.
	PublishedAt time.Time
	// Source names where PublishedAt came from. Empty when Status is ReleaseAgeUnknown.
	Source ReleaseAgeSource
	// WindowDays is the configured window in whole days.
	WindowDays int
	// Until is PublishedAt plus the window. Zero when Status is ReleaseAgeUnknown.
	Until time.Time
	// AgeDays is the whole days from PublishedAt to the scan clock. Zero when
	// Status is ReleaseAgeUnknown.
	AgeDays int
}

// EvaluateReleaseAge evaluates the caller-selected version at the scan clock.
// It returns nil for an older version, an input without a version, or a
// non-positive window. It returns unknown when no publish time is available,
// the time is in the future, or the versioned input does not identify the
// analysed package (different ecosystem, namespace, name, or version, or an
// input that cannot be parsed).
func (a *Analysis) EvaluateReleaseAge(now time.Time, window time.Duration) *ReleaseAge {
	if a == nil || a.Package == nil || window <= 0 {
		return nil
	}
	result := &ReleaseAge{Status: ReleaseAgeUnknown, WindowDays: int(window / (24 * time.Hour))}
	parsed, err := purl.NewParser().Parse(a.OriginalPURL)
	if err != nil {
		if hasVersionSegment(a.OriginalPURL) {
			return result
		}
		return nil
	}
	if parsed.Version() == "" {
		return nil
	}
	if !identifiesPackage(parsed, a.Package) {
		return result
	}
	var at time.Time
	if a.ReleaseHistory != nil && a.ReleaseHistory.Registry == RegistryNpm {
		at = a.ReleaseHistory.PublishedAt[a.Package.Version]
		if !at.IsZero() {
			result.Source = ReleaseAgeSourceNpm
		}
	}
	if at.IsZero() && !a.PyPIPublishedAt.IsZero() && parsed.Ecosystem() == "pypi" {
		at = a.PyPIPublishedAt
		result.Source = ReleaseAgeSourcePyPI
	}
	if at.IsZero() && a.HasRequestedVersionInfo() && a.ReleaseInfo.RequestedVersion.Version == a.Package.Version {
		at = a.ReleaseInfo.RequestedVersion.PublishedAt
		if !at.IsZero() {
			result.Source = ReleaseAgeSourceDepsDev
		}
	}
	if at.IsZero() || at.After(now) {
		result.Source = ""
		return result
	}
	if now.Sub(at) > window {
		return nil
	}
	result.Status = ReleaseAgeYoung
	result.PublishedAt = at
	result.Until = at.Add(window)
	result.AgeDays = daysBetween(at, now)
	return result
}

// ReleaseAgeNotice returns the detailed notice for a young release when no
// dormant-release marker or malicious advisory already explains it.
func (a *Analysis) ReleaseAgeNotice() string {
	if a == nil || a.ReleaseAge == nil || a.ReleaseAge.Status != ReleaseAgeYoung || a.HasDormantRelease() || a.Malicious() {
		return ""
	}
	r := a.ReleaseAge
	var age string
	switch r.AgeDays {
	case 0:
		age = "less than a day before this scan"
	case 1:
		age = "1 day before this scan"
	default:
		age = fmt.Sprintf("%d days before this scan", r.AgeDays)
	}
	return fmt.Sprintf("This version was published on %s, %s. Most malicious-package records appear within a week of publication, so a clean malicious-package check is not yet conclusive. If you can, wait until %s before adopting it, or check who published it.",
		r.PublishedAt.UTC().Format(time.DateOnly), age, r.Until.UTC().Format(time.DateOnly))
}

// identifiesPackage reports whether the parsed caller input names the same
// release as pkg. PyPI names compare after PEP 503 normalization; other names
// compare case-insensitively.
func identifiesPackage(input *purl.ParsedPURL, pkg *Package) bool {
	if input.Version() != pkg.Version || !strings.EqualFold(input.Ecosystem(), pkg.Ecosystem) {
		return false
	}
	analysed, err := purl.NewParser().Parse(pkg.PURL)
	if err != nil || analysed.Version() != pkg.Version || !strings.EqualFold(analysed.Ecosystem(), input.Ecosystem()) {
		return false
	}
	normalize := strings.ToLower
	if strings.EqualFold(input.Ecosystem(), "pypi") {
		normalize = NormalizePyPIName
	}
	return normalize(input.Namespace()) == normalize(analysed.Namespace()) &&
		normalize(input.Name()) == normalize(analysed.Name())
}

// hasVersionSegment reports whether an input the PURL parser rejected still
// carries "@version" after its last path segment, so a scoped npm name such as
// "@scope/name" without a version does not count.
func hasVersionSegment(raw string) bool {
	identity, _, _ := strings.Cut(raw, "?")
	identity, _, _ = strings.Cut(identity, "#")
	return strings.LastIndex(identity, "@") > strings.LastIndex(identity, "/")
}
