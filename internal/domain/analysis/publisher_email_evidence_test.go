package analysis

import (
	"strings"
	"testing"
	"time"
)

func TestPublisherEmailEvidenceLatestAndHidden(t *testing.T) {
	old := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	latest := old.Add(time.Hour)
	hidden := latest.Add(time.Hour)
	for _, tc := range []struct {
		name, emailDomain, wantStatus, wantRelation string
		hidden                                      bool
	}{
		{"same", "atlantis-software.net", "observed", "same", false},
		{"different", "example.com", "observed", "different", false},
		{"hidden", "atlantis-software.net", "observed", "same", true},
		{"missing email", "", "email_missing", "unknown", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := PublisherPackageHistory{Latest: map[string][]PublisherPublish{"atiertant": {{Package: "node-ipc", Version: "12.0.0", PublishedAt: latest, EmailDomain: tc.emailDomain, RegistrableDomain: tc.emailDomain}}}}
			if tc.hidden {
				h.HiddenPublishedAt = []time.Time{hidden}
			}
			f := MaintainerDomainFinding{Account: "atiertant", Domain: "atlantis-software.net", Kind: MaintainerDomainReregisteredBefore, Registered: old}
			e := PublisherEmailEvidenceFor(f, []PublisherPackageHistory{h})
			if e.Status != tc.wantStatus || e.DomainRelation != tc.wantRelation || e.RegistrationRelation != "after" || e.NewerPublishUnknown != tc.hidden {
				t.Fatalf("evidence=%+v", e)
			}
		})
	}
}

func TestPublisherEvidenceNeverChangesClassification(t *testing.T) {
	first := time.Date(2020, 11, 11, 0, 0, 0, 0, time.UTC)
	base := time.Date(2026, 5, 14, 14, 25, 30, 0, time.UTC)
	history := PublisherPackageHistory{Latest: map[string][]PublisherPublish{"atiertant": {{Package: "node-ipc", Version: "12.0.0", PublishedAt: first, EmailDomain: "atlantis-software.net", RegistrableDomain: "atlantis-software.net"}}}}
	for _, same := range []bool{false, true} {
		for _, before := range []bool{false, true} {
			for _, hidden := range []bool{false, true} {
				h := history
				if hidden {
					h.HiddenPublishedAt = []time.Time{base.Add(-time.Hour)}
				}
				registered := first.Add(-time.Hour)
				if before {
					registered = first.Add(time.Hour)
				}
				kind := ClassifyMaintainerDomain(first, base, DomainRecord{Status: DomainFound, Registered: registered})
				f := MaintainerDomainFinding{Account: "atiertant", Domain: "atlantis-software.net", Kind: kind, Registered: registered}
				if !same {
					h.Latest = map[string][]PublisherPublish{"atiertant": {{Package: "node-ipc", Version: "12.0.0", PublishedAt: first, EmailDomain: "example.com", RegistrableDomain: "example.com"}}}
				}
				baselineFlagged := len((&MaintainerDomainResult{Findings: []MaintainerDomainFinding{f}}).Flagged())
				f.PublisherEmailEvidence = PublisherEmailEvidenceFor(f, []PublisherPackageHistory{h})
				if f.Kind != kind || len((&MaintainerDomainResult{Findings: []MaintainerDomainFinding{f}}).Flagged()) != baselineFlagged {
					t.Fatalf("same=%v before=%v hidden=%v finding=%+v", same, before, hidden, f)
				}
			}
		}
	}
}

func TestPublisherNoticeClauseIsNeutral(t *testing.T) {
	at := time.Date(2024, 8, 12, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		status, relation, want string
		hidden                 bool
	}{
		{"observed", "same", " still used this domain.", false},
		{"observed", "different", " used another domain.", false},
		{"observed", "same", " A newer publish exists whose publisher is not visible.", true},
		{"email_missing", "unknown", "", false},
		{"email_missing", "unknown", "", true},
		{"conflicting", "unknown", "", false},
		{"not_observed", "unknown", "", false},
	} {
		e := &PublisherEmailEvidence{Status: tc.status, DomainRelation: tc.relation, NewerPublishUnknown: tc.hidden, LatestObserved: &PublisherLatestObserved{PublishedAt: at}}
		clause := publisherNoticeClause(e)
		if tc.want == "" && clause != "" || tc.want != "" && !strings.Contains(clause, tc.want) {
			t.Fatalf("evidence=%+v clause=%q", e, clause)
		}
		notice := maintainerDomainNotice(MaintainerDomainFinding{Account: "atiertant", Domain: "atlantis-software.net", Kind: MaintainerDomainUnregistered, PublisherEmailEvidence: e})
		for _, forbidden := range []string{"safe", "cleared", "resolved", "stale", "fixed", "no risk"} {
			if strings.Contains(strings.ToLower(notice), forbidden) {
				t.Fatalf("notice contains %q: %q", forbidden, notice)
			}
		}
	}
}

func TestPublisherEmailEvidenceConflictingAndUnobserved(t *testing.T) {
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	f := MaintainerDomainFinding{Account: "account-a", Domain: "example.com"}
	h := PublisherPackageHistory{Latest: map[string][]PublisherPublish{"account-a": {
		{Package: "example", Version: "1", PublishedAt: at, EmailDomain: "example.com", RegistrableDomain: "example.com"},
		{Package: "example", Version: "2", PublishedAt: at, EmailDomain: "other.example", RegistrableDomain: "other.example"},
	}}}
	if got := PublisherEmailEvidenceFor(f, []PublisherPackageHistory{h}); got.Status != "conflicting" || got.DomainRelation != "unknown" {
		t.Fatalf("conflicting=%+v", got)
	}
	f.Account = "absent"
	if got := PublisherEmailEvidenceFor(f, []PublisherPackageHistory{h}); got.Status != "not_observed" || got.LatestObserved != nil {
		t.Fatalf("unobserved=%+v", got)
	}
}

func TestPublisherEmailEvidenceEqualRegistrationAndRegistrableDomain(t *testing.T) {
	at := time.Date(2026, 5, 7, 11, 49, 33, 0, time.UTC)
	f := MaintainerDomainFinding{Account: "account-a", Domain: "example.co.uk", Kind: MaintainerDomainReregisteredBefore, Registered: at}
	h := PublisherPackageHistory{Latest: map[string][]PublisherPublish{"account-a": {{Package: "example", Version: "1", PublishedAt: at, EmailDomain: "mail.example.co.uk", RegistrableDomain: "example.co.uk"}}}}
	e := PublisherEmailEvidenceFor(f, []PublisherPackageHistory{h})
	if e.Status != "observed" || e.DomainRelation != "same" || e.RegistrationRelation != "equal" {
		t.Fatalf("evidence=%+v", e)
	}
}
