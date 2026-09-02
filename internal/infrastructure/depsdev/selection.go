package depsdev

import (
	"log/slog"
	"strings"

	"github.com/Masterminds/semver/v3"
	pep440 "github.com/aquasecurity/go-pep440-version"

	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
)

// depsDevYankedReason is the deps.dev deprecatedReason value marking a cargo
// release the registry has withdrawn. See ADR-0024.
const depsDevYankedReason = "yanked"

// pickStableDevAndMax selects Stable, Dev, and the maximum SemVer version from the list.
//
// Versions the registry has withdrawn are excluded from Stable selection before
// any rule below applies; see withdrawnFromStable. Dev and Max are unaffected.
//
// preferredStable is the version the package's own registry presents as current,
// empty when unavailable. It is an upper bound on Stable:
//
//	Stable:
//	 1. preferredStable matches a version exactly -> that version
//	 2. preferredStable set and PEP 440 parseable -> greatest version
//	    <= preferredStable, ties broken by latest PublishedAt then version string.
//	    No such version leaves Stable empty; the bound is never exceeded.
//	 3. preferredStable empty or not PEP 440 parseable -> latest IsDefault=true by
//	    PublishedAt, else latest purl.IsStableVersion by PublishedAt
//
// Dev:
//   - Latest among non-stable by PublishedAt
//
// Max:
//   - Highest SemVer using Masterminds semver; if none are valid SemVer, fallback to latest by PublishedAt
//
// See ADR-0023 and ADR-0024.
func pickStableDevAndMax(versions []Version, preferredStable string) (stable Version, dev Version, max Version) {
	if len(versions) == 0 {
		return Version{}, Version{}, Version{}
	}

	var defaults, stables, nonStables []Version
	var semverCandidates []Version
	stableEligible := make([]Version, 0, len(versions))

	for _, v := range versions {
		isStable := purl.IsStableVersion(v.VersionKey.Version)
		if !withdrawnFromStable(v) {
			stableEligible = append(stableEligible, v)
			if v.IsDefault {
				defaults = append(defaults, v)
			}
			if isStable {
				stables = append(stables, v)
			}
		}
		if !isStable {
			nonStables = append(nonStables, v)
		}

		if _, err := semver.NewVersion(v.VersionKey.Version); err == nil {
			semverCandidates = append(semverCandidates, v)
		}
	}

	// Stable selection. A zero picked with governs=true is intended: the bound
	// applies and excludes every candidate, so Stable stays empty.
	if picked, governs := pickByRegistryStable(stableEligible, preferredStable); governs {
		stable = picked
	} else if len(defaults) > 0 {
		stable = latestByPublishedAt(defaults)
	} else if len(stables) > 0 {
		stable = latestByPublishedAt(stables)
	}

	// Dev selection
	if len(nonStables) > 0 {
		dev = latestByPublishedAt(nonStables)
	}

	// Max SemVer selection
	if len(semverCandidates) > 0 {
		max = maxBySemver(semverCandidates)
	} else {
		max = latestByPublishedAt(versions)
	}

	return stable, dev, max
}

// pickByRegistryStable applies the bound. governs=false means the bound does not
// apply — no hint was supplied, or it is not PEP 440 parseable — and the caller
// must use the deps.dev rules. governs=true with a zero Version means the bound
// applies and excludes every candidate, so Stable is left empty.
func pickByRegistryStable(versions []Version, preferredStable string) (picked Version, governs bool) {
	if preferredStable == "" {
		return Version{}, false
	}

	var exact Version
	exactFound := false
	for _, v := range versions {
		if v.VersionKey.Version != preferredStable {
			continue
		}
		if !exactFound || v.PublishedAt.After(exact.PublishedAt) {
			exact, exactFound = v, true
		}
	}
	if exactFound {
		return exact, true
	}

	bound, err := pep440.Parse(preferredStable)
	if err != nil {
		return Version{}, false
	}

	var best stableCandidate
	found := false
	for _, v := range versions {
		parsed, err := pep440.Parse(v.VersionKey.Version)
		if err != nil {
			// Unparseable version strings cannot be ordered against the bound.
			// They can still win the exact-match pass above.
			continue
		}
		if parsed.GreaterThan(bound) {
			continue
		}
		cur := stableCandidate{parsed: parsed, raw: v}
		if !found || cur.outranks(best) {
			best, found = cur, true
		}
	}
	return best.raw, true
}

// withdrawnFromStable reports whether the registry has withdrawn v, making it
// ineligible as Stable.
//
// Only cargo is covered: deps.dev reports a cargo yank as IsDeprecated with a
// deprecatedReason of depsDevYankedReason. An unrecognised reason on a
// deprecated cargo release is logged and treated as not withdrawn.
// See ADR-0024.
func withdrawnFromStable(v Version) bool {
	if !v.IsDeprecated || !strings.EqualFold(v.VersionKey.System, "cargo") {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(v.DeprecatedReason), depsDevYankedReason) {
		return true
	}
	slog.Warn("depsdev_cargo_unknown_deprecated_reason",
		"name", v.VersionKey.Name,
		"version", v.VersionKey.Version,
		"deprecated_reason", v.DeprecatedReason)
	return false
}

// stableCandidate pairs a deps.dev version with its parsed PEP 440 form so the
// two cannot be transposed at a call site.
type stableCandidate struct {
	parsed pep440.Version
	raw    Version
}

// outranks reports whether c beats other: the greater PEP 440 version wins, then
// the more recently published, then the greater version string so the result does
// not depend on input order.
func (c stableCandidate) outranks(other stableCandidate) bool {
	if cmp := c.parsed.Compare(other.parsed); cmp != 0 {
		return cmp > 0
	}
	if !c.raw.PublishedAt.Equal(other.raw.PublishedAt) {
		return c.raw.PublishedAt.After(other.raw.PublishedAt)
	}
	return c.raw.VersionKey.Version > other.raw.VersionKey.Version
}

func latestByPublishedAt(vs []Version) Version {
	var best Version
	for _, v := range vs {
		if best.VersionKey.Version == "" || v.PublishedAt.After(best.PublishedAt) {
			best = v
		}
	}
	return best
}

func maxBySemver(vs []Version) Version {
	var best Version
	var bestV *semver.Version
	for _, v := range vs {
		cur, err := semver.NewVersion(v.VersionKey.Version)
		if err != nil {
			continue
		}
		if bestV == nil || cur.GreaterThan(bestV) {
			bestV = cur
			best = v
		}
	}
	return best
}
