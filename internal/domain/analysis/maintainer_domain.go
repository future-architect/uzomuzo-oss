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
	Registrar  string
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
		// Deletion status is useful even when RDAP omits the registration event.
	default:
		return MaintainerDomainLookupFailed
	}
	for _, status := range rec.Statuses {
		if strings.EqualFold(status, "redemption period") || strings.EqualFold(status, "pending delete") {
			return MaintainerDomainLapsing
		}
	}
	if rec.Registered.IsZero() {
		return MaintainerDomainLookupFailed
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
	Registrar  string
	DropCatch  bool
}

// IsDropCatchRegistrar identifies known drop-catch registrar names.
// Source: https://www.usenix.org/conference/usenixsecurity17/technical-sessions/presentation/lauinger
func IsDropCatchRegistrar(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, prefix := range []string{"dropcatch.com", "snapnames", "namejet", "pheenix"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
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
	// The notice states the risk, the evidence, and the action. It stays
	// conditional because packument e-mails can be stale (npm documents this).
	who := "the maintainer"
	if f.Account != "" {
		who = "maintainer " + f.Account
	}
	const action = " Ask the maintainer to confirm the account e-mail and 2FA."
	switch f.Kind {
	case MaintainerDomainReregisteredBefore:
		registrar := ""
		if f.Registrar != "" {
			registrar = " via " + f.Registrar
			if f.DropCatch {
				registrar += " (a drop-catch registrar)"
			}
		}
		return fmt.Sprintf("if %s still uses this address, npm password-reset mail may reach whoever now holds %s (registered %s%s, after the address first appeared %s).%s", who, f.Domain, f.Registered.Format(time.DateOnly), registrar, f.FirstSeen.Format(time.DateOnly), action)
	case MaintainerDomainLapsing:
		return fmt.Sprintf("if %s still uses this address, npm password-reset mail may reach whoever registers %s next (the domain is being deleted).%s", who, f.Domain, action)
	case MaintainerDomainUnregistered:
		return fmt.Sprintf("if %s still uses this address, npm password-reset mail may reach whoever registers %s (RDAP has no registration).%s", who, f.Domain, action)
	default:
		return ""
	}
}
