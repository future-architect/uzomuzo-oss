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
		name, emailDomain string
		wantStatus        PublisherEmailStatus
		wantRelation      PublisherDomainRelation
		hidden            bool
	}{
		{"same", "atlantis-software.net", PublisherEmailObserved, PublisherDomainSame, false},
		{"different", "example.com", PublisherEmailObserved, PublisherDomainDifferent, false},
		{"hidden", "atlantis-software.net", PublisherEmailObserved, PublisherDomainSame, true},
		{"missing email", "", PublisherEmailMissing, PublisherDomainUnknown, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := PublisherPackageHistory{Latest: map[string][]PublisherPublish{"atiertant": {{Package: "node-ipc", Version: "12.0.0", PublishedAt: latest, EmailDomain: tc.emailDomain, RegistrableDomain: tc.emailDomain}}}}
			if tc.hidden {
				h.HiddenPublishedAt = []time.Time{hidden}
			}
			f := MaintainerDomainFinding{Account: "atiertant", Domain: "atlantis-software.net", Kind: MaintainerDomainReregisteredBefore, Registered: old}
			e := PublisherEmailEvidenceFor(f, PublisherScopeScanPackuments, h, []PublisherPackageHistory{h})
			if e.Status != tc.wantStatus || e.DomainRelation != tc.wantRelation || e.RegistrationRelation != PublisherRegistrationAfter || e.NewerPublishUnknown != tc.hidden {
				t.Fatalf("evidence=%+v", e)
			}
		})
	}
}

func TestPublisherEmailEvidenceUsesFindingSourceHiddenTimes(t *testing.T) {
	t1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	f := MaintainerDomainFinding{Account: "atiertant", Domain: "atlantis-software.net", Kind: MaintainerDomainUnregistered}
	other := PublisherPackageHistory{Latest: map[string][]PublisherPublish{"atiertant": {{Package: "example", Version: "1", PublishedAt: t1, EmailDomain: "atlantis-software.net", RegistrableDomain: "atlantis-software.net"}}}}
	for _, tc := range []struct {
		name   string
		hidden time.Time
		want   bool
	}{
		{name: "newer hidden source version", hidden: t1.Add(time.Hour), want: true},
		{name: "older hidden source version", hidden: t1.Add(-time.Hour), want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := PublisherPackageHistory{HiddenPublishedAt: []time.Time{tc.hidden}}
			e := PublisherEmailEvidenceFor(f, PublisherScopeScanPackuments, source, []PublisherPackageHistory{source, other})
			if e.NewerPublishUnknown != tc.want {
				t.Fatalf("newer_publish_unknown=%v, want %v", e.NewerPublishUnknown, tc.want)
			}
			clause := publisherNoticeClause(e)
			if tc.want && clause != " A newer publish exists whose publisher is not visible." {
				t.Fatalf("clause=%q", clause)
			}
			if !tc.want && !strings.Contains(clause, "still used this domain.") {
				t.Fatalf("clause=%q", clause)
			}
		})
	}
}

func TestPublisherEmailEvidenceUnobservedAccountUsesSourceHiddenTimes(t *testing.T) {
	f := MaintainerDomainFinding{Account: "account-a", Domain: "example.com", Kind: MaintainerDomainUnregistered}
	for _, tc := range []struct {
		name   string
		hidden []time.Time
		want   bool
	}{
		{name: "hidden source publish", hidden: []time.Time{time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)}, want: true},
		{name: "no hidden source publish", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := PublisherPackageHistory{HiddenPublishedAt: tc.hidden}
			e := PublisherEmailEvidenceFor(f, PublisherScopeScanPackuments, source, []PublisherPackageHistory{source})
			if e.Status != PublisherEmailNotObserved || e.NewerPublishUnknown != tc.want {
				t.Fatalf("evidence=%+v, want status=%q newer_publish_unknown=%v", e, PublisherEmailNotObserved, tc.want)
			}
			if clause := publisherNoticeClause(e); clause != "" {
				t.Fatalf("notice clause=%q, want empty", clause)
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
				baseline := maintainerDomainNotice(f)
				f.PublisherEmailEvidence = PublisherEmailEvidenceFor(f, PublisherScopeScanPackuments, h, []PublisherPackageHistory{h})
				got := maintainerDomainNotice(f)
				// A kind without a notice gains none; otherwise the evidence
				// appends one sentence and keeps the notice it extends.
				if kind.WarrantsNotice() {
					if !strings.HasPrefix(got, baseline) || len(got) == len(baseline) {
						t.Errorf("same=%v before=%v hidden=%v: notice %q does not extend %q", same, before, hidden, got, baseline)
					}
				} else if got != "" || baseline != "" {
					t.Errorf("same=%v before=%v hidden=%v: kind %s gave notice %q", same, before, hidden, kind, got)
				}
			}
		}
	}
}

func TestPublisherNoticeClauseIsNeutral(t *testing.T) {
	at := time.Date(2024, 8, 12, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		status   PublisherEmailStatus
		relation PublisherDomainRelation
		want     string
		hidden   bool
	}{
		{PublisherEmailObserved, PublisherDomainSame, " still used this domain.", false},
		{PublisherEmailObserved, PublisherDomainDifferent, " used another domain.", false},
		{PublisherEmailObserved, PublisherDomainSame, " A newer publish exists whose publisher is not visible.", true},
		{PublisherEmailMissing, PublisherDomainUnknown, "", false},
		{PublisherEmailMissing, PublisherDomainUnknown, "", true},
		{PublisherEmailConflicting, PublisherDomainUnknown, "", false},
		{PublisherEmailNotObserved, PublisherDomainUnknown, "", false},
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
	if got := PublisherEmailEvidenceFor(f, PublisherScopeScanPackuments, h, []PublisherPackageHistory{h}); got.Status != PublisherEmailConflicting || got.DomainRelation != PublisherDomainUnknown {
		t.Fatalf("conflicting=%+v", got)
	}
	f.Account = "absent"
	if got := PublisherEmailEvidenceFor(f, PublisherScopeScanPackuments, h, []PublisherPackageHistory{h}); got.Status != PublisherEmailNotObserved || got.LatestObserved != nil {
		t.Fatalf("unobserved=%+v", got)
	}
}

func TestPublisherEmailEvidenceEqualRegistrationAndRegistrableDomain(t *testing.T) {
	at := time.Date(2026, 5, 7, 11, 49, 33, 0, time.UTC)
	f := MaintainerDomainFinding{Account: "account-a", Domain: "example.co.uk", Kind: MaintainerDomainReregisteredBefore, Registered: at}
	h := PublisherPackageHistory{Latest: map[string][]PublisherPublish{"account-a": {{Package: "example", Version: "1", PublishedAt: at, EmailDomain: "mail.example.co.uk", RegistrableDomain: "example.co.uk"}}}}
	e := PublisherEmailEvidenceFor(f, PublisherScopeScanPackuments, h, []PublisherPackageHistory{h})
	if e.Status != PublisherEmailObserved || e.DomainRelation != PublisherDomainSame || e.RegistrationRelation != PublisherRegistrationEqual {
		t.Fatalf("evidence=%+v", e)
	}
}

// TestPublisherEmailEvidenceSameRegistrableDomainDoesNotConflict checks that
// tied publishes from two hosts of one registrable domain are not conflicting.
func TestPublisherEmailEvidenceSameRegistrableDomainDoesNotConflict(t *testing.T) {
	t.Parallel()
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	f := MaintainerDomainFinding{Account: "account-a", Domain: "example.co.uk"}
	h := PublisherPackageHistory{Latest: map[string][]PublisherPublish{"account-a": {
		{Package: "example", Version: "1", PublishedAt: at, EmailDomain: "mail.example.co.uk", RegistrableDomain: "example.co.uk"},
		{Package: "example", Version: "2", PublishedAt: at, EmailDomain: "example.co.uk", RegistrableDomain: "example.co.uk"},
	}}}
	e := PublisherEmailEvidenceFor(f, PublisherScopeIncidentPackument, h, []PublisherPackageHistory{h})
	if e.Status != PublisherEmailObserved || e.DomainRelation != PublisherDomainSame {
		t.Fatalf("evidence=%+v", e)
	}
	if e.Scope != PublisherScopeIncidentPackument || e.Source != PublisherEvidenceSource {
		t.Errorf("scope=%q source=%q", e.Scope, e.Source)
	}
}

// TestPublisherEmailEvidenceTieWithMissingEmailConflicts pins that a publish
// without a domain tied with one that has a domain is not read as observed.
func TestPublisherEmailEvidenceTieWithMissingEmailConflicts(t *testing.T) {
	t.Parallel()
	at := time.Date(2024, 8, 12, 0, 0, 0, 0, time.UTC)
	f := MaintainerDomainFinding{Account: "atiertant", Domain: "atlantis-software.net"}
	h := PublisherPackageHistory{Latest: map[string][]PublisherPublish{"atiertant": {
		{Package: "node-ipc", Version: "2", PublishedAt: at, EmailDomain: "atlantis-software.net", RegistrableDomain: "atlantis-software.net"},
		{Package: "node-ipc", Version: "3", PublishedAt: at},
	}}}
	e := PublisherEmailEvidenceFor(f, PublisherScopeScanPackuments, h, []PublisherPackageHistory{h})
	if e.Status != PublisherEmailConflicting || e.LatestObserved != nil || e.DomainRelation != PublisherDomainUnknown {
		t.Errorf("evidence=%+v, want conflicting with no latest observed", e)
	}
}
