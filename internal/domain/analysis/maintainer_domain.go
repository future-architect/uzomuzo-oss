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

// MaintainerDomainReason explains why an address is MaintainerDomainUnchecked.
type MaintainerDomainReason string

const (
	// MaintainerDomainReasonNotInHistory means no dated version lists the address.
	MaintainerDomainReasonNotInHistory MaintainerDomainReason = "not_in_history"
	// MaintainerDomainReasonInvalidAddress means the address has no usable mail domain.
	MaintainerDomainReasonInvalidAddress MaintainerDomainReason = "invalid_address"
	// MaintainerDomainReasonSharedMail means the domain belongs to a shared mail
	// provider. The wire value "free_mail" is part of the JSON output.
	MaintainerDomainReasonSharedMail MaintainerDomainReason = "free_mail"
	// MaintainerDomainReasonPrivateSuffix means the domain sits under a private public suffix.
	MaintainerDomainReasonPrivateSuffix MaintainerDomainReason = "private_suffix"
	// MaintainerDomainReasonNoRDAP means no https RDAP service is known for the TLD.
	MaintainerDomainReasonNoRDAP MaintainerDomainReason = "no_rdap"
)

// WarrantsNotice reports whether a finding of this kind produces a notice.
// See ADR-0030.
func (k MaintainerDomainKind) WarrantsNotice() bool {
	return k == MaintainerDomainUnregistered || k == MaintainerDomainLapsing || k == MaintainerDomainReregisteredBefore
}

// RDAP domain statuses that mean the registration is being deleted.
const (
	RDAPStatusRedemptionPeriod = "redemption period"
	RDAPStatusPendingDelete    = "pending delete"
)

// HasDeletionStatus reports whether any RDAP status says the domain is being deleted.
func HasDeletionStatus(statuses []string) bool {
	for _, status := range statuses {
		if strings.EqualFold(status, RDAPStatusRedemptionPeriod) || strings.EqualFold(status, RDAPStatusPendingDelete) {
			return true
		}
	}
	return false
}

// NormalizeEmail matches maintainer addresses despite surrounding space or case.
func NormalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

// RDAPBootstrapSource names where the RDAP service list used by a run came from.
type RDAPBootstrapSource string

const (
	// RDAPBootstrapLive means the list was fetched from IANA during the run.
	RDAPBootstrapLive RDAPBootstrapSource = "live"
	// RDAPBootstrapCache means a cached copy of the IANA list was used.
	RDAPBootstrapCache RDAPBootstrapSource = "cache"
	// RDAPBootstrapEmbedded means the copy built into the binary was used.
	RDAPBootstrapEmbedded RDAPBootstrapSource = "embedded"
)

// RDAPBootstrapInfo identifies the IANA RDAP service list a run used. See ADR-0030.
type RDAPBootstrapInfo struct {
	Source RDAPBootstrapSource
	// Publication is the list's "publication" timestamp as IANA wrote it.
	Publication string
}

// DomainLookupStatus describes the outcome of an RDAP lookup.
type DomainLookupStatus int

const (
	// DomainFound means RDAP returned a domain record.
	DomainFound DomainLookupStatus = iota
	// DomainNotFound means RDAP returned a not-found response.
	DomainNotFound
	// DomainUnsupported means no https RDAP service is known for the TLD.
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
	default:
		return MaintainerDomainLookupFailed
	}
	// Checked before the registration date; see ADR-0030.
	if HasDeletionStatus(rec.Statuses) {
		return MaintainerDomainLapsing
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
	Account   string
	Domain    string
	Kind      MaintainerDomainKind
	FirstSeen time.Time
	// Registered is zero when RDAP gave no registration date.
	Registered time.Time
	// Reason is set only for MaintainerDomainUnchecked.
	Reason    MaintainerDomainReason
	Registrar string
	DropCatch bool
	// PublisherEmailEvidence is the npm publisher e-mail evidence, when collected.
	PublisherEmailEvidence *PublisherEmailEvidence
	// PackagePublish describes dated publishes by this account in this package.
	PackagePublish *PackagePublish
}

// IsDropCatchRegistrar identifies known drop-catch registrar names by prefix.
// See ADR-0030 for the source of the list.
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
	// AnalysedVersionPublisher describes the exact version being analysed.
	AnalysedVersionPublisher VersionPublisherKind
}

// Flagged returns findings that warrant a maintainer domain notice.
func (r *MaintainerDomainResult) Flagged() []MaintainerDomainFinding {
	if r == nil {
		return nil
	}
	var out []MaintainerDomainFinding
	for _, f := range r.Findings {
		if f.Kind.WarrantsNotice() {
			out = append(out, f)
		}
	}
	return out
}

// MaintainerDomainNotices formats one notice per notice-bearing finding, in
// finding order. The wording is described in ADR-0030.
func (a *Analysis) MaintainerDomainNotices() []string {
	if a == nil {
		return nil
	}
	flagged := a.MaintainerDomain.Flagged()
	notices := make([]string, 0, len(flagged))
	for _, f := range flagged {
		if notice := maintainerDomainNotice(f, a.MaintainerDomain.AnalysedVersionPublisher); notice != "" {
			notices = append(notices, notice)
		}
	}
	return notices
}

func maintainerDomainNotice(f MaintainerDomainFinding, versionPublisher VersionPublisherKind) string {
	// The notice states the RDAP fact first, then the npm record, then the
	// conditional risk and one action. The address itself is not printed:
	// the account name and domain are enough to act on, and the address
	// would otherwise land in terminals and CI logs. The action is one the
	// user can take alone: every user of a popular package sees the same
	// notice, so asking each of them to contact the maintainer would send the
	// maintainer the same question many times. Only the registry can see the
	// account's current e-mail.
	who := "a maintainer"
	if f.Account != "" {
		who = "maintainer " + f.Account
	}
	first := f.FirstSeen.UTC().Format(time.DateOnly)
	const action = " Before upgrading, check who published newer versions of this package."
	clause := publisherNoticeClause(f.PublisherEmailEvidence) + packagePublishNoticeClause(f, versionPublisher)
	switch f.Kind {
	case MaintainerDomainReregisteredBefore:
		registrar := ""
		if f.Registrar != "" {
			registrar = " (registrar: " + f.Registrar
			if f.DropCatch {
				registrar += ", a drop-catch registrar"
			}
			registrar += ")"
		}
		return fmt.Sprintf("RDAP shows %s registered on %s%s. That is after npm records first showed %s with an address at this domain (%s). If the account still uses that address, npm password-reset mail may reach whoever holds the domain now.%s%s", f.Domain, f.Registered.UTC().Format(time.DateOnly), registrar, who, first, action, clause)
	case MaintainerDomainLapsing:
		return fmt.Sprintf("RDAP shows %s is being deleted. npm records show %s with an address at this domain (first seen %s). If the account still uses that address, npm password-reset mail may reach whoever registers the domain next.%s%s", f.Domain, who, first, action, clause)
	case MaintainerDomainUnregistered:
		return fmt.Sprintf("RDAP has no registration for %s. npm records show %s with an address at this domain (first seen %s). If the account still uses that address, npm password-reset mail may reach whoever registers the domain.%s%s", f.Domain, who, first, action, clause)
	default:
		return ""
	}
}

func packagePublishNoticeClause(f MaintainerDomainFinding, versionPublisher VersionPublisherKind) string {
	var clause string
	if f.Account != "" && f.PackagePublish != nil {
		switch f.PackagePublish.Status {
		case PackagePublishFound:
			clause = " The last publish by this account found in this package's npm records was on " + f.PackagePublish.LastPublishedAt.UTC().Format(time.DateOnly) + "."
		case PackagePublishNotFound:
			clause = " The npm records retrieved for this package show no publish by this account."
		}
	}
	if versionPublisher == VersionPublisherTrustedPublishing {
		clause += " This version was published from GitHub Actions through npm trusted publishing."
	}
	return clause
}

func publisherNoticeClause(e *PublisherEmailEvidence) string {
	if e == nil {
		return ""
	}
	if e.Status != PublisherEmailObserved || e.LatestObserved == nil {
		return ""
	}
	if e.NewerPublishUnknown {
		return " A newer publish exists whose publisher is not visible."
	}
	day := e.LatestObserved.PublishedAt.UTC().Format(time.DateOnly)
	switch e.DomainRelation {
	case PublisherDomainSame:
		return " The account's latest publish seen in this scan (" + day + ") still used this domain."
	case PublisherDomainDifferent:
		return " The account's latest publish seen in this scan (" + day + ") used another domain."
	default:
		return ""
	}
}
