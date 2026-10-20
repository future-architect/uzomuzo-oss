package analysis

import (
	"sort"
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
	Latest            map[string][]PublisherPublish
	HiddenPublishedAt []time.Time
}

// PublisherLatestObserved identifies the latest visible publish without an e-mail local part.
type PublisherLatestObserved struct {
	PublishedAt time.Time `json:"published_at"`
	Package     string    `json:"package"`
	Version     string    `json:"version"`
	EmailDomain string    `json:"email_domain,omitempty"`
}

// PublisherEmailStatus describes whether the latest visible publisher e-mail is usable.
type PublisherEmailStatus string

const (
	PublisherEmailNotObserved PublisherEmailStatus = "not_observed"
	PublisherEmailObserved    PublisherEmailStatus = "observed"
	PublisherEmailMissing     PublisherEmailStatus = "email_missing"
	PublisherEmailConflicting PublisherEmailStatus = "conflicting"
)

// PublisherDomainRelation compares the visible e-mail domain with the finding domain.
type PublisherDomainRelation string

const (
	PublisherDomainUnknown   PublisherDomainRelation = "unknown"
	PublisherDomainSame      PublisherDomainRelation = "same"
	PublisherDomainDifferent PublisherDomainRelation = "different"
)

// PublisherRegistrationRelation compares the visible publish with registration.
type PublisherRegistrationRelation string

const (
	PublisherRegistrationUnknown PublisherRegistrationRelation = "unknown"
	PublisherRegistrationBefore  PublisherRegistrationRelation = "before"
	PublisherRegistrationEqual   PublisherRegistrationRelation = "equal"
	PublisherRegistrationAfter   PublisherRegistrationRelation = "after"
)

// PublisherEmailEvidence describes limited publisher e-mail evidence from this scan.
type PublisherEmailEvidence struct {
	Status                 PublisherEmailStatus          `json:"status"`
	Source                 string                        `json:"source"`
	Scope                  string                        `json:"scope"`
	AccountHistoryComplete bool                          `json:"account_history_complete"`
	LatestObserved         *PublisherLatestObserved      `json:"latest_observed,omitempty"`
	DomainRelation         PublisherDomainRelation       `json:"domain_relation"`
	RegistrationRelation   PublisherRegistrationRelation `json:"registration_relation"`
	NewerPublishUnknown    bool                          `json:"newer_publish_unknown"`
}

// AddPublisherPublish retains only greatest-time candidates for an account.
func (h *PublisherPackageHistory) AddPublisherPublish(account string, p PublisherPublish) {
	if account == "" || p.PublishedAt.IsZero() {
		return
	}
	if h.Latest == nil {
		h.Latest = make(map[string][]PublisherPublish)
	}
	current := h.Latest[account]
	if len(current) == 0 || p.PublishedAt.After(current[0].PublishedAt) {
		h.Latest[account] = []PublisherPublish{p}
	} else if p.PublishedAt.Equal(current[0].PublishedAt) {
		h.Latest[account] = append(current, p)
	}
}

// PublisherEmailEvidenceFor evaluates one finding without changing its classification.
func PublisherEmailEvidenceFor(f MaintainerDomainFinding, source PublisherPackageHistory, histories []PublisherPackageHistory) *PublisherEmailEvidence {
	e := &PublisherEmailEvidence{Status: PublisherEmailNotObserved, Source: "versions._npmUser", Scope: "scan_packuments", DomainRelation: PublisherDomainUnknown, RegistrationRelation: PublisherRegistrationUnknown}
	var latest []PublisherPublish
	for _, h := range histories {
		for _, p := range h.Latest[f.Account] {
			if len(latest) == 0 || p.PublishedAt.After(latest[0].PublishedAt) {
				latest = []PublisherPublish{p}
			} else if p.PublishedAt.Equal(latest[0].PublishedAt) {
				latest = append(latest, p)
			}
		}
	}
	if len(latest) == 0 {
		return e
	}
	sort.Slice(latest, func(i, j int) bool {
		if latest[i].Package == latest[j].Package {
			return latest[i].Version < latest[j].Version
		}
		return latest[i].Package < latest[j].Package
	})
	domains := map[string]bool{}
	for _, p := range latest {
		domains[p.EmailDomain] = true
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
