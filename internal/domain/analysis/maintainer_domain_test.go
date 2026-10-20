package analysis

import (
	"strings"
	"testing"
	"time"
)

// TestClassifyMaintainerDomain checks each registration boundary.
func TestClassifyMaintainerDomain(t *testing.T) {
	t.Parallel()
	first := time.Date(2020, 11, 11, 0, 0, 0, 0, time.UTC)
	registered := time.Date(2026, 5, 7, 11, 49, 33, 0, time.UTC)
	base := time.Date(2026, 5, 14, 14, 25, 30, 0, time.UTC)
	tests := []struct {
		name string
		rec  DomainRecord
		want MaintainerDomainKind
	}{
		{"node-ipc", DomainRecord{Status: DomainFound, Registered: registered}, MaintainerDomainReregisteredBefore},
		{"same as first", DomainRecord{Status: DomainFound, Registered: first}, MaintainerDomainNormal},
		{"old", DomainRecord{Status: DomainFound, Registered: time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)}, MaintainerDomainNormal},
		{"after base", DomainRecord{Status: DomainFound, Registered: base.Add(24 * time.Hour)}, MaintainerDomainReregisteredAfter},
		{"at base", DomainRecord{Status: DomainFound, Registered: base}, MaintainerDomainReregisteredAfter},
		{"not found", DomainRecord{Status: DomainNotFound}, MaintainerDomainUnregistered},
		{"failed", DomainRecord{Status: DomainLookupFailed}, MaintainerDomainLookupFailed},
		{"unsupported", DomainRecord{Status: DomainUnsupported}, MaintainerDomainUnchecked},
		{"zero registration", DomainRecord{Status: DomainFound}, MaintainerDomainLookupFailed},
		{"redemption", DomainRecord{Status: DomainFound, Registered: first, Statuses: []string{"Redemption Period"}}, MaintainerDomainLapsing},
		{"pending", DomainRecord{Status: DomainFound, Registered: first, Statuses: []string{"pending delete"}}, MaintainerDomainLapsing},
		{"undated pending", DomainRecord{Status: DomainFound, Statuses: []string{"pending delete"}}, MaintainerDomainLapsing},
		{"undated redemption", DomainRecord{Status: DomainFound, Statuses: []string{"Redemption Period"}}, MaintainerDomainLapsing},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyMaintainerDomain(first, base, tt.rec); got != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
	if got := ClassifyMaintainerDomain(time.Time{}, base, DomainRecord{Status: DomainFound, Registered: registered}); got != MaintainerDomainUnchecked {
		t.Errorf("undated address=%s, want unchecked", got)
	}
}

// TestIsDropCatchRegistrar checks the fixed evidence prefixes.
func TestIsDropCatchRegistrar(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{"DropCatch.com 538 LLC", true}, {"DropCatch.com 1387 LLC", true},
		{"SnapNames registrar", true}, {"NameJet registrar", true}, {"Pheenix registrar", true},
		{"GoDaddy.com, LLC", false}, {"NameCheap, Inc.", false}, {"", false},
	} {
		if got := IsDropCatchRegistrar(tc.name); got != tc.want {
			t.Errorf("%q: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestMaintainerDomainNoticeRegistrar checks optional registrar wording.
func TestMaintainerDomainNoticeRegistrar(t *testing.T) {
	for _, tc := range []struct {
		name, registrar, want string
		drop                  bool
	}{
		{"unknown", "", "registered 2026-05-07, after", false},
		{"ordinary", "NameCheap, Inc.", "registered 2026-05-07 through NameCheap, Inc., after", false},
		{"drop catch", "DropCatch.com 538 LLC", "registered 2026-05-07 through DropCatch.com 538 LLC (a drop-catch registrar), after", true},
	} {
		a := &Analysis{MaintainerDomain: &MaintainerDomainResult{Findings: []MaintainerDomainFinding{{Domain: "example.com", Kind: MaintainerDomainReregisteredBefore, FirstSeen: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), Registered: time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC), Registrar: tc.registrar, DropCatch: tc.drop}}}}
		if got := a.MaintainerDomainNotice(); !strings.Contains(got, tc.want) {
			t.Errorf("%s: %q", tc.name, got)
		}
	}
}
