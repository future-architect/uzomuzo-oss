package analysis

import (
	"fmt"
	"time"

	cfg "github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

// DormantReleaseKind names which rule flagged a return from silence.
type DormantReleaseKind string

const (
	// DormantReleaseBurstAcrossLines means the release came out in a burst that
	// touched at least DormantBurstMinLines release lines (ADR-0026).
	DormantReleaseBurstAcrossLines DormantReleaseKind = "burst_across_lines"
	// DormantReleaseFresh means the release is a recent return from silence on
	// any number of lines (ADR-0028).
	DormantReleaseFresh DormantReleaseKind = "fresh"
)

// DormantRelease records that the analysed version is a release that followed
// a long silence. It is carried next to the lifecycle label, whatever label the
// decision tree chose. A nil pointer means no rule matched or no release
// history was available. See ADR-0028.
type DormantRelease struct {
	// Kind names the rule that matched. A burst across lines takes precedence
	// over a fresh release when both match.
	Kind DormantReleaseKind
	// Burst describes the release and the burst it belongs to.
	Burst *DormantBurst
	// Until is when the marker clears: the publish time plus the period the
	// matching rule keeps a release flagged.
	Until time.Time
}

// EvaluateDormantRelease applies the ADR-0026 and ADR-0028 rules to the
// analysed version at now, with the thresholds from c. Returns nil when the
// analysis, its package or its release history is missing, or no rule matches.
func (a *Analysis) EvaluateDormantRelease(now time.Time, c cfg.LifecycleAssessmentConfig) *DormantRelease {
	if a == nil || a.Package == nil {
		return nil
	}
	cfg.NormalizeLifecycleConfig(&c)
	silence := time.Duration(c.DormantSilenceDays) * 24 * time.Hour
	flag := time.Duration(c.DormantFlagDays) * 24 * time.Hour
	if b := DetectDormantBurst(a.ReleaseHistory, a.Package.Version, now, silence, flag); b != nil {
		return &DormantRelease{Kind: DormantReleaseBurstAcrossLines, Burst: b, Until: b.PublishedAt.Add(flag)}
	}
	if b := DetectFreshDormantRelease(a.ReleaseHistory, a.Package.Version, now, silence, flag); b != nil {
		return &DormantRelease{Kind: DormantReleaseFresh, Burst: b, Until: b.PublishedAt.Add(flag)}
	}
	return nil
}

// DormantReleaseNotice returns the text to show for the dormant-release marker,
// or "" when there is nothing to add: no marker, a malicious advisory already
// names the version, or the lifecycle result already explains the burst
// because ADR-0026 turned its label into Review Needed.
func (a *Analysis) DormantReleaseNotice() string {
	if a == nil || a.DormantRelease == nil || a.DormantRelease.Burst == nil || a.Malicious() {
		return ""
	}
	if lr := a.GetLifecycleResult(); lr != nil {
		for _, s := range lr.Signals {
			if s.Name == SignalDormantReleaseBurst {
				return ""
			}
		}
	}
	d := a.DormantRelease
	if d.Kind == DormantReleaseBurstAcrossLines {
		return dormantBurstReason(d.Burst)
	}
	b := d.Burst
	reason := fmt.Sprintf("This version came out %s, after %d days with no release. "+
		"Takeovers of npm packages that had gone quiet looked like this, and so does a maintainer who ships again after a break.",
		b.PublishedAt.UTC().Format(time.DateOnly), b.SilentDays)
	if a.MaliciousState != nil {
		switch a.MaliciousState.Status {
		case MaliciousStatusClean:
			reason += " No OSV malicious-package record names it yet."
		case MaliciousStatusLookupFailed:
			reason += " The OSV malicious-package check did not complete."
		}
	}
	if b.Removed {
		reason += " npm has since removed this version."
	} else {
		reason += " Before using it, check who published it (npm view <package>@<version> _npmUser)."
	}
	return reason + fmt.Sprintf(" This marker clears on %s.", d.Until.UTC().Format(time.DateOnly))
}
