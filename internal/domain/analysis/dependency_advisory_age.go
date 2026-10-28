package analysis

import (
	"fmt"
	"sort"
	"time"
)

// DependencyAdvisoryAgeStatus says why a dormant package whose HIGH advisories
// are all in dependencies is, or is not, EOL-Effective. See ADR-0032.
type DependencyAdvisoryAgeStatus string

// DependencyAdvisoryAgeStatus values, from the one that wins to the one that
// loses when a package has several dependency advisories.
const (
	// DependencyAdvisoryAged: published more than EolInactivityDays ago, and
	// the database rates it HIGH or above (or gives no rating). EOL-Effective.
	DependencyAdvisoryAged DependencyAdvisoryAgeStatus = "aged"
	// DependencyAdvisoryUnknown: the OSV lookup failed, so the label might
	// have been EOL-Effective.
	DependencyAdvisoryUnknown DependencyAdvisoryAgeStatus = "unknown"
	// DependencyAdvisoryNotAged: published too recently; EligibleOn is the
	// first day it would count.
	DependencyAdvisoryNotAged DependencyAdvisoryAgeStatus = "not_aged"
	// DependencyAdvisoryRatedBelowHigh: HIGH by CVSS3, but the database rates
	// it LOW, MODERATE or MEDIUM.
	DependencyAdvisoryRatedBelowHigh DependencyAdvisoryAgeStatus = "rated_below_high"
	// DependencyAdvisoryRatingUnrecognized: the database gives a rating this
	// tool does not know.
	DependencyAdvisoryRatingUnrecognized DependencyAdvisoryAgeStatus = "rating_unrecognized"
	// DependencyAdvisoryUndated: OSV has no record, or the record has no
	// publication date.
	DependencyAdvisoryUndated DependencyAdvisoryAgeStatus = "undated"
	// DependencyAdvisoryWithdrawn: the database has withdrawn the advisory.
	DependencyAdvisoryWithdrawn DependencyAdvisoryAgeStatus = "withdrawn"
	// DependencyAdvisoryNotChecked: no OSV lookup was made (for example a
	// library caller without an OSV client).
	DependencyAdvisoryNotChecked DependencyAdvisoryAgeStatus = "not_checked"
)

// DependencyAdvisoryAge is the dependency advisory that decided the outcome,
// and what was found about it.
type DependencyAdvisoryAge struct {
	Status     DependencyAdvisoryAgeStatus
	AdvisoryID string
	// Dependency is "name@version" of the dependency the advisory is against.
	Dependency string
	// Published is the OSV publication date. Zero when unknown.
	Published time.Time
	// EligibleOn is the first UTC day the advisory counts as aged. Set only
	// for DependencyAdvisoryNotAged.
	EligibleOn time.Time
	// ReviewedSeverity is the database's rating, set for
	// DependencyAdvisoryRatedBelowHigh.
	ReviewedSeverity string
}

// statusRank orders the statuses: a lower rank wins.
var statusRank = map[DependencyAdvisoryAgeStatus]int{
	DependencyAdvisoryAged:               0,
	DependencyAdvisoryUnknown:            1,
	DependencyAdvisoryNotAged:            2,
	DependencyAdvisoryRatedBelowHigh:     3,
	DependencyAdvisoryRatingUnrecognized: 4,
	DependencyAdvisoryUndated:            5,
	DependencyAdvisoryWithdrawn:          6,
	DependencyAdvisoryNotChecked:         7,
}

// ClassifyDependencyAdvisoryAge picks the deciding advisory among vd's
// dependency advisories with a known CVSS3 score at or above threshold.
// Returns nil when there is none.
//
// Ages are counted in whole UTC days, so the time of day of the run does not
// matter: an advisory exactly eolDays old is not aged yet. Among aged
// advisories the oldest wins; among not-aged ones the one that ages first;
// otherwise the lowest ID, so the result does not depend on slice order.
func ClassifyDependencyAdvisoryAge(vd *VersionDetail, threshold float64, eolDays int, now time.Time) *DependencyAdvisoryAge {
	if vd == nil {
		return nil
	}
	today := utcDay(now)
	var found []DependencyAdvisoryAge
	for _, adv := range vd.Advisories {
		if adv.Relation != AdvisoryRelationTransitive || !IsKnownHighCVSS3(adv.CVSS3Score, threshold) {
			continue
		}
		found = append(found, classifyOne(adv, eolDays, today))
	}
	if len(found) == 0 {
		return nil
	}
	sort.SliceStable(found, func(i, j int) bool {
		a, b := found[i], found[j]
		if statusRank[a.Status] != statusRank[b.Status] {
			return statusRank[a.Status] < statusRank[b.Status]
		}
		if (a.Status == DependencyAdvisoryAged || a.Status == DependencyAdvisoryNotAged) && !a.Published.Equal(b.Published) {
			return a.Published.Before(b.Published)
		}
		return a.AdvisoryID < b.AdvisoryID
	})
	return &found[0]
}

// classifyOne gives one advisory its status. The lookup outcome is read
// before the date, so a withdrawn record never ages.
func classifyOne(adv Advisory, eolDays int, today time.Time) DependencyAdvisoryAge {
	out := DependencyAdvisoryAge{AdvisoryID: adv.ID, Dependency: adv.DependencyName}
	switch adv.RecordLookup {
	case AdvisoryRecordNotLookedUp:
		out.Status = DependencyAdvisoryNotChecked
		return out
	case AdvisoryRecordLookupFailed:
		out.Status = DependencyAdvisoryUnknown
		return out
	case AdvisoryRecordWithdrawn:
		out.Status = DependencyAdvisoryWithdrawn
		return out
	case AdvisoryRecordNotFound:
		out.Status = DependencyAdvisoryUndated
		return out
	case AdvisoryRecordFound:
	default:
		out.Status = DependencyAdvisoryNotChecked
		return out
	}
	switch adv.ReviewedSeverity {
	case "", "HIGH", "CRITICAL":
	case "LOW", "MODERATE", "MEDIUM":
		out.Status = DependencyAdvisoryRatedBelowHigh
		out.ReviewedSeverity = adv.ReviewedSeverity
		return out
	default:
		out.Status = DependencyAdvisoryRatingUnrecognized
		return out
	}
	if adv.Published.IsZero() {
		out.Status = DependencyAdvisoryUndated
		return out
	}
	out.Published = adv.Published
	eligible := utcDay(adv.Published).AddDate(0, 0, eolDays+1)
	if !today.Before(eligible) {
		out.Status = DependencyAdvisoryAged
		return out
	}
	out.Status = DependencyAdvisoryNotAged
	out.EligibleOn = eligible
	return out
}

// Reason is the reason text for the outcome: what was found, the dates, and
// the advisory, so the reader can check it.
func (d *DependencyAdvisoryAge) Reason(eolDays int) string {
	where := d.AdvisoryID
	if d.Dependency != "" {
		where += " in " + d.Dependency
	}
	switch d.Status {
	case DependencyAdvisoryAged:
		return fmt.Sprintf("HIGH dependency advisory published %s, over %d days ago (%s)", day(d.Published), eolDays, where)
	case DependencyAdvisoryUnknown:
		return fmt.Sprintf("HIGH advisories only in dependencies; age not verified, OSV lookup failed (%s)", where)
	case DependencyAdvisoryNotAged:
		return fmt.Sprintf("HIGH advisories only in dependencies; oldest published %s, counts from %s (%s)", day(d.Published), day(d.EligibleOn), where)
	case DependencyAdvisoryRatedBelowHigh:
		return fmt.Sprintf("dependency advisories HIGH by CVSS3 only, rated %s by the advisory database (%s)", d.ReviewedSeverity, where)
	case DependencyAdvisoryRatingUnrecognized:
		return fmt.Sprintf("HIGH advisories only in dependencies; unrecognized advisory database rating (%s)", where)
	case DependencyAdvisoryUndated:
		return fmt.Sprintf("HIGH advisories only in dependencies; no publication date in OSV (%s)", where)
	case DependencyAdvisoryWithdrawn:
		return fmt.Sprintf("dependency advisory withdrawn by its database (%s)", where)
	default:
		return "HIGH advisories only in dependencies"
	}
}

func utcDay(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func day(t time.Time) string { return t.UTC().Format("2006-01-02") }
