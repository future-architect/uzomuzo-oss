package depsdev

import (
	"github.com/Masterminds/semver/v3"
	"github.com/future-architect/uzomuzo/internal/common/purl"
)

// pickStableDevAndMax selects Stable, Dev, and the maximum SemVer version from the list.
// Stable:
//  1. Prefer IsDefault=true (latest by PublishedAt)
//  2. If none, latest IsStableVersion=true by PublishedAt
//
// Dev:
//   - Latest among non-stable by PublishedAt
//
// Max:
//   - Highest SemVer using Masterminds semver; if none are valid SemVer, fallback to latest by PublishedAt
func pickStableDevAndMax(versions []Version) (stable Version, dev Version, max Version) {
	if len(versions) == 0 {
		return Version{}, Version{}, Version{}
	}

	var defaults, stables, nonStables []Version
	var semverCandidates []Version

	for _, v := range versions {
		if v.IsDefault {
			defaults = append(defaults, v)
		}
		if purl.IsStableVersion(v.VersionKey.Version) {
			stables = append(stables, v)
		} else {
			nonStables = append(nonStables, v)
		}

		if _, err := semver.NewVersion(v.VersionKey.Version); err == nil {
			semverCandidates = append(semverCandidates, v)
		}
	}

	// Stable selection
	if len(defaults) > 0 {
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
