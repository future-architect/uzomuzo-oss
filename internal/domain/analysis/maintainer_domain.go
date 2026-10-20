package analysis

import (
	"fmt"
	"strings"
	"time"
)

// MaintainerDomainKind describes the registration state of a maintainer's mail domain.
type MaintainerDomainKind string

const (
	MaintainerDomainUnchecked          MaintainerDomainKind = "unchecked"
	MaintainerDomainLookupFailed       MaintainerDomainKind = "lookup_failed"
	MaintainerDomainUnregistered       MaintainerDomainKind = "unregistered"
	MaintainerDomainLapsing            MaintainerDomainKind = "lapsing"
	MaintainerDomainReregisteredAfter  MaintainerDomainKind = "reregistered_after"
	MaintainerDomainReregisteredBefore MaintainerDomainKind = "reregistered_before"
	MaintainerDomainNormal             MaintainerDomainKind = "normal"
)

type DomainLookupStatus int

const (
	DomainFound DomainLookupStatus = iota
	DomainNotFound
	DomainUnsupported
	DomainLookupFailed
)

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

type MaintainerAddress struct {
	Account   string
	Email     string
	FirstSeen time.Time
}

type MaintainerDomainFinding struct {
	Account    string
	Domain     string
	Kind       MaintainerDomainKind
	FirstSeen  time.Time
	Registered time.Time
	Reason     string
}

type MaintainerDomainResult struct {
	Findings []MaintainerDomainFinding
}

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

func (a *Analysis) HasMaintainerDomainNotice() bool {
	return a != nil && len(a.MaintainerDomain.Flagged()) > 0
}

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
