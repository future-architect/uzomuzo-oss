package github

import (
	"strings"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// enrichProjectLicenseFromGitHub applies GitHub licenseInfo as a fallback.
//
// Rules:
//   - Only modifies when current license is empty OR came from a non-standard deps.dev value.
//   - Prefers SPDX identifier; falls back to name only if it normalizes to a canonical SPDX.
//   - Ignores empty values and NOASSERTION.
//
// Returns (possibly updated license, changed).
func enrichProjectLicenseFromGitHub(current domain.ResolvedLicense, license *LicenseInfo) (domain.ResolvedLicense, bool) {
	if license == nil {
		return current, false
	}

	// If already have a usable SPDX expression, keep it. NOASSERTION is
	// recognized but not usable, so fall through to allow GitHub to fill in.
	if current.IsUsableSPDX() {
		return current, false
	}

	// SPDX normalization helper (reject empty / NOASSERTION). GitHub's
	// licenseInfo.spdxId is documented as a single SPDX ID, so the leaf-only
	// NormalizeLicenseIdentifier is the right tool here — using
	// NormalizeExpression would add unnecessary parser overhead.
	tryNormalize := func(raw string) (string, bool) {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.EqualFold(raw, "NOASSERTION") {
			return "", false
		}
		norm, is := domain.NormalizeLicenseIdentifier(raw)
		if !is || norm == "" || strings.EqualFold(norm, "NOASSERTION") {
			return "", false
		}
		return norm, true
	}

	// 1. Prefer spdxId
	if expr, ok := tryNormalize(license.SpdxID); ok {
		if current.Expression == "" || current.IsNonStandard() {
			return domain.ResolvedLicense{Expression: expr, Raw: license.SpdxID, Source: domain.LicenseSourceGitHubProjectSPDX}, true
		}
		return current, false
	}
	// 2. Next try name
	if expr, ok := tryNormalize(license.Name); ok {
		if current.Expression == "" || current.IsNonStandard() {
			return domain.ResolvedLicense{Expression: expr, Raw: license.Name, Source: domain.LicenseSourceGitHubProjectSPDX}, true
		}
		return current, false
	}

	// 3. Capture non-SPDX raw (name preferred, else spdxId if it had some string) when we still have no SPDX
	pickRaw := func(v string) string {
		v = strings.TrimSpace(v)
		if v == "" || strings.EqualFold(v, "NOASSERTION") {
			return ""
		}
		return v
	}
	rawCandidate := pickRaw(license.Name)
	if rawCandidate == "" {
		rawCandidate = pickRaw(license.SpdxID)
	}
	if rawCandidate == "" { // nothing to record
		return current, false
	}

	if current.IsZero() || current.IsNonStandard() {
		if current.IsNonStandard() && current.Raw == rawCandidate { // identical already
			return current, false
		}
		return domain.ResolvedLicense{Expression: "", Raw: rawCandidate, Source: domain.LicenseSourceGitHubProjectNonStandard}, true
	}
	return current, false
}
