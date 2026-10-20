package analysis

import (
	"cmp"
	"slices"
	"time"
)

// PublisherPublish is one visible npm publish by an account.
type PublisherPublish struct {
	Package, Version  string
	PublishedAt       time.Time
	EmailDomain       string
	RegistrableDomain string
}

// PublisherPackageHistory keeps latest visible publishes per account in one packument.
type PublisherPackageHistory struct {
	// Latest maps an account name to its publishes sharing the latest time.
	Latest map[string][]PublisherPublish
	// HiddenPublishedAt holds the times of versions whose manifest was removed.
	HiddenPublishedAt []time.Time
}

// PublisherLatestObserved identifies the latest visible publish without an e-mail local part.
type PublisherLatestObserved struct {
	PublishedAt time.Time
	Package     string
	Version     string
	EmailDomain string
}

// PublisherEmailStatus describes whether the latest visible publisher e-mail is usable.
type PublisherEmailStatus string

const (
	// PublisherEmailNotObserved means the account has no visible publish in scope.
	PublisherEmailNotObserved PublisherEmailStatus = "not_observed"
	// PublisherEmailObserved means the latest visible publish has a usable domain.
	PublisherEmailObserved PublisherEmailStatus = "observed"
	// PublisherEmailMissing means the latest visible publish has no usable domain.
	PublisherEmailMissing PublisherEmailStatus = "email_missing"
	// PublisherEmailConflicting means publishes tied for latest disagree on the
	// registrable domain. A tie between a publish with a domain and one without
	// counts, since the one without may have used another domain.
	PublisherEmailConflicting PublisherEmailStatus = "conflicting"
)

// PublisherDomainRelation compares the visible e-mail domain with the finding domain.
type PublisherDomainRelation string

const (
	// PublisherDomainUnknown is used when either domain is unknown.
	PublisherDomainUnknown PublisherDomainRelation = "unknown"
	// PublisherDomainSame means the registrable domains match.
	PublisherDomainSame PublisherDomainRelation = "same"
	// PublisherDomainDifferent means the registrable domains differ.
	PublisherDomainDifferent PublisherDomainRelation = "different"
)

// PublisherRegistrationRelation compares the visible publish with registration.
type PublisherRegistrationRelation string

const (
	// PublisherRegistrationUnknown is used when the finding has no re-registration date.
	PublisherRegistrationUnknown PublisherRegistrationRelation = "unknown"
	// PublisherRegistrationBefore means the publish predates the re-registration.
	PublisherRegistrationBefore PublisherRegistrationRelation = "before"
	// PublisherRegistrationEqual means the publish has the re-registration time.
	PublisherRegistrationEqual PublisherRegistrationRelation = "equal"
	// PublisherRegistrationAfter means the publish follows the re-registration.
	PublisherRegistrationAfter PublisherRegistrationRelation = "after"
)

// PublisherEvidenceScope names the packuments the evidence was drawn from.
type PublisherEvidenceScope string

const (
	// PublisherScopeScanPackuments is every npm packument fetched by one scan run.
	PublisherScopeScanPackuments PublisherEvidenceScope = "scan_packuments"
	// PublisherScopeIncidentPackument is one packument, limited to publishes
	// before the incident time.
	PublisherScopeIncidentPackument PublisherEvidenceScope = "incident_packument_before_incident"
)

// PublisherEvidenceSource is the packument field the evidence reads.
const PublisherEvidenceSource = "versions._npmUser"

// PublisherEmailEvidence describes limited publisher e-mail evidence from the
// packuments named by Scope.
type PublisherEmailEvidence struct {
	Status PublisherEmailStatus
	Source string
	Scope  PublisherEvidenceScope
	// AccountHistoryComplete is always false: neither scope enumerates every
	// package of the account. It is reported so a reader does not assume so.
	AccountHistoryComplete bool
	LatestObserved         *PublisherLatestObserved
	DomainRelation         PublisherDomainRelation
	RegistrationRelation   PublisherRegistrationRelation
	NewerPublishUnknown    bool
}

// AddPublisherPublish retains only greatest-time candidates for an account.
func (h *PublisherPackageHistory) AddPublisherPublish(account string, p PublisherPublish) {
	if account == "" || p.PublishedAt.IsZero() {
		return
	}
	if h.Latest == nil {
		h.Latest = make(map[string][]PublisherPublish)
	}
	h.Latest[account] = keepLatest(h.Latest[account], p)
}

// keepLatest adds p to current, which holds only the publishes sharing the
// greatest time so far.
func keepLatest(current []PublisherPublish, p PublisherPublish) []PublisherPublish {
	switch {
	case len(current) == 0 || p.PublishedAt.After(current[0].PublishedAt):
		return []PublisherPublish{p}
	case p.PublishedAt.Equal(current[0].PublishedAt):
		return append(current, p)
	default:
		return current
	}
}

// PublisherEmailEvidenceFor evaluates one finding without changing its
// classification. scope names the packuments that histories cover; histories
// should include source, the packument that raised the finding. A removed
// version in source counts even when the account has no visible publish, while
// one in another packument counts only where the account published visibly.
func PublisherEmailEvidenceFor(f MaintainerDomainFinding, scope PublisherEvidenceScope, source PublisherPackageHistory, histories []PublisherPackageHistory) *PublisherEmailEvidence {
	e := &PublisherEmailEvidence{Status: PublisherEmailNotObserved, Source: PublisherEvidenceSource, Scope: scope, DomainRelation: PublisherDomainUnknown, RegistrationRelation: PublisherRegistrationUnknown}
	var latest []PublisherPublish
	for _, h := range histories {
		for _, p := range h.Latest[f.Account] {
			latest = keepLatest(latest, p)
		}
	}
	if len(latest) == 0 {
		e.NewerPublishUnknown = len(source.HiddenPublishedAt) > 0
		return e
	}
	slices.SortFunc(latest, func(a, b PublisherPublish) int {
		return cmp.Or(cmp.Compare(a.Package, b.Package), cmp.Compare(a.Version, b.Version))
	})
	// Hosts under one registrable domain (mail.example.co.uk, example.co.uk)
	// do not conflict.
	domains := map[string]bool{}
	for _, p := range latest {
		domains[cmp.Or(p.RegistrableDomain, p.EmailDomain)] = true
	}
	if len(domains) > 1 {
		e.Status = PublisherEmailConflicting
	} else {
		e.LatestObserved = &PublisherLatestObserved{PublishedAt: latest[0].PublishedAt, Package: latest[0].Package, Version: latest[0].Version, EmailDomain: latest[0].EmailDomain}
		if latest[0].EmailDomain == "" {
			e.Status = PublisherEmailMissing
		} else {
			e.Status = PublisherEmailObserved
			if latest[0].RegistrableDomain != "" && f.Domain != "" {
				if latest[0].RegistrableDomain == f.Domain {
					e.DomainRelation = PublisherDomainSame
				} else {
					e.DomainRelation = PublisherDomainDifferent
				}
			}
		}
	}
	if f.Kind == MaintainerDomainReregisteredBefore && !f.Registered.IsZero() {
		switch {
		case latest[0].PublishedAt.Before(f.Registered):
			e.RegistrationRelation = PublisherRegistrationBefore
		case latest[0].PublishedAt.After(f.Registered):
			e.RegistrationRelation = PublisherRegistrationAfter
		default:
			e.RegistrationRelation = PublisherRegistrationEqual
		}
	}
	for _, at := range source.HiddenPublishedAt {
		if at.After(latest[0].PublishedAt) {
			e.NewerPublishUnknown = true
			return e
		}
	}
	for _, h := range histories {
		if len(h.Latest[f.Account]) == 0 {
			continue
		}
		for _, at := range h.HiddenPublishedAt {
			if at.After(latest[0].PublishedAt) {
				e.NewerPublishUnknown = true
				return e
			}
		}
	}
	return e
}
