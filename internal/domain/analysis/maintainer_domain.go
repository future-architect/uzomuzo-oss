package analysis

import (
	"fmt"
	"strings"
	"time"
)

// MaintainerDomainKind describes the registration state of a maintainer's mail domain.
type MaintainerDomainKind string

const (
	// MaintainerDomainUnchecked means the address or domain cannot be evaluated.
	MaintainerDomainUnchecked MaintainerDomainKind = "unchecked"
	// MaintainerDomainLookupFailed means RDAP did not provide a usable answer.
	MaintainerDomainLookupFailed MaintainerDomainKind = "lookup_failed"
	// MaintainerDomainUnregistered means RDAP returned no domain record.
	MaintainerDomainUnregistered MaintainerDomainKind = "unregistered"
	// MaintainerDomainLapsing means RDAP reports a deletion-related status.
	MaintainerDomainLapsing MaintainerDomainKind = "lapsing"
	// MaintainerDomainReregisteredAfter means registration is at or after the evaluation time.
	MaintainerDomainReregisteredAfter MaintainerDomainKind = "reregistered_after"
	// MaintainerDomainReregisteredBefore means registration follows the address's first appearance.
	MaintainerDomainReregisteredBefore MaintainerDomainKind = "reregistered_before"
	// MaintainerDomainNormal means registration predates or matches the address's first appearance.
	MaintainerDomainNormal MaintainerDomainKind = "normal"
)

// DomainLookupStatus describes the outcome of an RDAP lookup.
type DomainLookupStatus int

const (
	// DomainFound means RDAP returned a domain record.
	DomainFound DomainLookupStatus = iota
	// DomainNotFound means RDAP returned a not-found response.
	DomainNotFound
	// DomainUnsupported means no RDAP service is known for the TLD.
	DomainUnsupported
	// DomainLookupFailed means the lookup did not yield a usable record.
	DomainLookupFailed
)

// DomainRecord holds the RDAP fields used for domain classification.
type DomainRecord struct {
	Status     DomainLookupStatus
	Registered time.Time
	Statuses   []string
}

// ClassifyMaintainerDomain applies the registration rule without I/O.
func ClassifyMaintainerDomain(firstSeen, base time.Time, rec DomainRecord) MaintainerDomainKind {
	if firstSeen.IsZero() {
		return MaintainerDomainUnchecked
	}
	switch rec.Status {
	case DomainUnsupported:
		return MaintainerDomainUnchecked
	case DomainNotFound:
		return MaintainerDomainUnregistered
	case DomainLookupFailed:
		return MaintainerDomainLookupFailed
	case DomainFound:
		if rec.Registered.IsZero() {
			return MaintainerDomainLookupFailed
		}
	default:
		return MaintainerDomainLookupFailed
	}
	for _, status := range rec.Statuses {
		if strings.EqualFold(status, "redemption period") || strings.EqualFold(status, "pending delete") {
			return MaintainerDomainLapsing
		}
	}
	if !rec.Registered.Before(base) {
		return MaintainerDomainReregisteredAfter
	}
	if rec.Registered.After(firstSeen) {
		return MaintainerDomainReregisteredBefore
	}
	return MaintainerDomainNormal
}

// MaintainerAddress identifies a maintainer address and its first appearance.
type MaintainerAddress struct {
	Account   string
	Email     string
	FirstSeen time.Time
}

// MaintainerDomainFinding records the classification for one address.
type MaintainerDomainFinding struct {
	Account    string
	Domain     string
	Kind       MaintainerDomainKind
	FirstSeen  time.Time
	Registered time.Time
	Reason     string
}

// MaintainerDomainResult holds findings for a package's maintainers.
type MaintainerDomainResult struct {
	Findings []MaintainerDomainFinding
}

// Flagged returns findings that warrant a maintainer domain notice.
func (r *MaintainerDomainResult) Flagged() []MaintainerDomainFinding {
	if r == nil {
		return nil
	}
	var out []MaintainerDomainFinding
	for _, f := range r.Findings {
		if f.Kind == MaintainerDomainUnregistered || f.Kind == MaintainerDomainLapsing || f.Kind == MaintainerDomainReregisteredBefore {
			out = append(out, f)
		}
	}
	return out
}

// HasMaintainerDomainNotice reports whether any finding warrants a notice.
func (a *Analysis) HasMaintainerDomainNotice() bool {
	return a != nil && len(a.MaintainerDomain.Flagged()) > 0
}

// MaintainerDomainNotice formats the first notice-bearing finding.
func (a *Analysis) MaintainerDomainNotice() string {
	if !a.HasMaintainerDomainNotice() {
		return ""
	}
	f := a.MaintainerDomain.Flagged()[0]
	switch f.Kind {
	case MaintainerDomainReregisteredBefore:
		return fmt.Sprintf("maintainer e-mail domain %s registered %s, after the address first appeared %s", f.Domain, f.Registered.Format(time.DateOnly), f.FirstSeen.Format(time.DateOnly))
	case MaintainerDomainLapsing:
		return fmt.Sprintf("maintainer e-mail domain %s is lapsing", f.Domain)
	case MaintainerDomainUnregistered:
		return fmt.Sprintf("maintainer e-mail domain %s is unregistered", f.Domain)
	default:
		return ""
	}
}
