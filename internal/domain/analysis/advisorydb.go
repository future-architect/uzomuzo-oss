package analysis

import (
	"slices"
	"strings"
	"time"
)

// UnmaintainedCooldown is how long an advisory must have been published before
// its unmaintained marker is admitted. A withdrawn advisory can be re-filed,
// and each re-file re-triggers downstream consumers that watch for a
// false-to-true transition of this fact. Nothing about a maintenance signal is
// urgent, so waiting costs little. See ADR-0025.
const UnmaintainedCooldown = 14 * 24 * time.Hour

// rustsecIDPrefix marks an advisory as curated by the RustSec advisory
// database. database_specific is source-defined, so the field is only
// trustworthy once the source is known.
const rustsecIDPrefix = "RUSTSEC-"

// InformationalUnmaintained is the database_specific.informational value that
// RustSec uses for "this crate is no longer maintained". The sibling values
// "unsound" and "notice" are different claims and are not admitted.
const InformationalUnmaintained = "unmaintained"

// AdvisoryRangeEvent is one event in an OSV affected range. Exactly one field
// is set per event, matching the OSV schema's one-key-per-event objects.
type AdvisoryRangeEvent struct {
	Introduced   string
	Fixed        string
	LastAffected string
	Limit        string
}

// AdvisoryRange is one version range of an OSV affected entry.
type AdvisoryRange struct {
	// Type is the OSV range type, e.g. "SEMVER", "ECOSYSTEM", "GIT".
	Type   string
	Events []AdvisoryRangeEvent
}

// AdvisoryAffected is one entry of an OSV advisory's affected list, reduced to
// the fields the maintenance classification reads.
type AdvisoryAffected struct {
	Ecosystem string
	Name      string
	Ranges    []AdvisoryRange
	// Versions is OSV's explicit enumeration of affected versions. A non-empty
	// list is never a package-wide claim.
	Versions []string
	// Informational carries database_specific.informational verbatim.
	Informational string
}

// AdvisoryRecord is one advisory reduced to the fields the maintenance
// classification reads. Infrastructure decodes OSV's wire shape onto this type;
// the type itself carries no serialization tags and no policy.
type AdvisoryRecord struct {
	ID      string
	Summary string
	// Reference is an advisory URL a human can open to verify the fact.
	Reference string
	// Published is zero when the advisory carried no publication date or the
	// date did not parse. A zero value never satisfies the cooldown.
	Published time.Time
	// Withdrawn is true when the advisory database has retracted the advisory.
	Withdrawn bool
	Affected  []AdvisoryAffected
}

// AdvisoryDBState captures package-level maintenance facts asserted by a
// third-party advisory database (today: RustSec, reached through OSV).
//
// It is deliberately NOT part of EOLStatus. An advisory curator is not a
// primary source: 35 of the 269 RustSec unmaintained advisories are explicitly
// third-party inferences drawn after the author did not respond, and a further
// 77 carry no attribution at all. Downstream consumers treat a primary-source
// EOL as authoritative enough to overrule a human judgement, which this fact
// must never do. See ADR-0025.
//
// A nil *AdvisoryDBState means the databases were not asked — the ecosystem is
// not one we query, the client is unwired, the PURL was unparseable, or the
// request failed. A non-nil value always means the database answered,
// including when nothing is flagged.
type AdvisoryDBState struct {
	// Unmaintained is true when an admitted advisory marks the whole package
	// as unmaintained.
	Unmaintained bool
	// AdvisoryID identifies the advisory the fact came from, empty when
	// Unmaintained is false.
	AdvisoryID string
	// Summary is the advisory's one-line summary, normalized for terminal
	// output. It is external text and may be empty.
	Summary string
	// Reference is an advisory URL where the fact can be verified.
	Reference string
	// Published is the advisory's publication date.
	Published time.Time
	// MarkerIDs lists every admitted advisory, sorted, AdvisoryID first. Nil
	// when Unmaintained is false. The assessor excludes each of them when
	// weighing vulnerabilities: a marker is filed as an advisory but is not one.
	MarkerIDs []string
}

// ClassifyUnmaintained decides whether any of recs marks the package
// (ecosystem, name) as unmaintained as a whole, at evaluation time now.
//
// Every admission rule is applied here rather than at the fetch site, because
// each one is a business rule: which curator is trusted, which marker value
// counts, whether a retracted advisory still speaks, whether the advisory
// covers the whole package, and how long a fresh filing must settle before it
// is believed. The clock is a parameter so the cooldown is deterministic.
//
// The returned state is always meaningful: a false Unmaintained means the
// records were read and nothing qualified, never "not asked".
func ClassifyUnmaintained(recs []AdvisoryRecord, ecosystem, name string, now time.Time) AdvisoryDBState {
	var best *AdvisoryRecord
	var markerIDs []string
	for i := range recs {
		rec := &recs[i]
		if !admitsUnmaintained(rec, ecosystem, name, now) {
			continue
		}
		markerIDs = append(markerIDs, rec.ID)
		// Deterministic across runs: the same input set always yields the same
		// evidence, whatever order the database returned it in.
		if best == nil || rec.ID < best.ID {
			best = rec
		}
	}
	if best == nil {
		return AdvisoryDBState{}
	}
	slices.Sort(markerIDs)
	return AdvisoryDBState{
		Unmaintained: true,
		AdvisoryID:   best.ID,
		Summary:      NormalizeSummary(best.Summary),
		Reference:    best.Reference,
		Published:    best.Published,
		MarkerIDs:    slices.Compact(markerIDs),
	}
}

// admitsUnmaintained reports whether one advisory may assert that the whole
// package is unmaintained.
func admitsUnmaintained(rec *AdvisoryRecord, ecosystem, name string, now time.Time) bool {
	if rec == nil || rec.Withdrawn {
		return false
	}
	if !strings.HasPrefix(rec.ID, rustsecIDPrefix) {
		return false
	}
	// A zero Published is a missing or unparseable date, not an aged one.
	if rec.Published.IsZero() || now.Sub(rec.Published) < UnmaintainedCooldown {
		return false
	}

	matched := 0
	for i := range rec.Affected {
		af := &rec.Affected[i]
		if !affectedMatchesPackage(af, ecosystem, name) {
			continue
		}
		matched++
		// Every entry naming this package must carry the marker and cover the
		// package as a whole. One bounded or differently-marked entry makes the
		// advisory's claim about this package narrower than the fact we emit.
		if af.Informational != InformationalUnmaintained {
			return false
		}
		if !coversWholePackage(af) {
			return false
		}
	}
	return matched > 0
}

// affectedMatchesPackage reports whether an affected entry names the queried
// package. Ecosystem and name are compared case-insensitively: OSV echoes the
// identifiers back as filed, and a case-variant PURL must not silently miss.
func affectedMatchesPackage(af *AdvisoryAffected, ecosystem, name string) bool {
	return strings.EqualFold(strings.TrimSpace(af.Ecosystem), strings.TrimSpace(ecosystem)) &&
		strings.EqualFold(strings.TrimSpace(af.Name), strings.TrimSpace(name))
}

// coversWholePackage reports whether an affected entry claims every version of
// the package, with no upper bound of any kind.
//
// The shape is checked as a universal over every range, and anything not
// recognized is rejected. An earlier draft checked only "introduced is zero and
// there is no fixed", which admitted ring's RUSTSEC-2025-0010 — an unmaintained
// marker bounded by `fixed: 0.17.0`, i.e. a claim about 0.16.x only. The
// resulting failure mode is a missed detection, never a false one, which is the
// standard ADR-0022 set.
func coversWholePackage(af *AdvisoryAffected) bool {
	if len(af.Versions) > 0 {
		return false
	}
	if len(af.Ranges) == 0 {
		return false
	}
	for _, r := range af.Ranges {
		if r.Type != "SEMVER" {
			return false
		}
		introduced := 0
		for _, ev := range r.Events {
			if ev.Fixed != "" || ev.LastAffected != "" || ev.Limit != "" {
				return false
			}
			if ev.Introduced == "" {
				continue
			}
			if ev.Introduced != "0" && ev.Introduced != "0.0.0-0" {
				return false
			}
			introduced++
		}
		if introduced != 1 {
			return false
		}
	}
	return true
}
