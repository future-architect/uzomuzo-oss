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
		{"unknown", "", "(registered 2026-05-07, after", false},
		{"ordinary", "NameCheap, Inc.", "(registered 2026-05-07 via NameCheap, Inc., after", false},
		{"drop catch", "DropCatch.com 538 LLC", "(registered 2026-05-07 via DropCatch.com 538 LLC (a drop-catch registrar), after", true},
	} {
		a := &Analysis{MaintainerDomain: &MaintainerDomainResult{Findings: []MaintainerDomainFinding{{Domain: "example.com", Kind: MaintainerDomainReregisteredBefore, FirstSeen: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), Registered: time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC), Registrar: tc.registrar, DropCatch: tc.drop}}}}
		notices := a.MaintainerDomainNotices()
		if len(notices) != 1 || !strings.Contains(notices[0], tc.want) {
			t.Errorf("%s: %q", tc.name, notices)
		}
	}
}

// TestMaintainerDomainNotices pins the full sentence for each notice kind.
func TestMaintainerDomainNotices(t *testing.T) {
	t.Parallel()
	first := time.Date(2020, 11, 11, 0, 0, 0, 0, time.UTC)
	// 23:00 at UTC-5 is the next day in UTC; both dates print in UTC.
	registered := time.Date(2026, 5, 6, 23, 0, 0, 0, time.FixedZone("EST", -5*60*60))
	tests := []struct {
		name string
		f    MaintainerDomainFinding
		want string
	}{
		{"reregistered before", MaintainerDomainFinding{Account: "atiertant", Domain: "atlantis-software.net", Kind: MaintainerDomainReregisteredBefore, FirstSeen: first, Registered: registered},
			"if maintainer atiertant still uses this address, npm password-reset mail may reach whoever now holds atlantis-software.net (registered 2026-05-07, after the address first appeared 2020-11-11). Ask the maintainer to confirm the account e-mail and 2FA."},
		{"lapsing", MaintainerDomainFinding{Account: "atiertant", Domain: "example.com", Kind: MaintainerDomainLapsing, FirstSeen: first},
			"if maintainer atiertant still uses this address, npm password-reset mail may reach whoever registers example.com next (the domain is being deleted). Ask the maintainer to confirm the account e-mail and 2FA."},
		{"unregistered without account", MaintainerDomainFinding{Domain: "example.com", Kind: MaintainerDomainUnregistered, FirstSeen: first},
			"if the maintainer still uses this address, npm password-reset mail may reach whoever registers example.com (RDAP has no registration). Ask the maintainer to confirm the account e-mail and 2FA."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := &Analysis{MaintainerDomain: &MaintainerDomainResult{Findings: []MaintainerDomainFinding{tt.f}}}
			got := a.MaintainerDomainNotices()
			if len(got) != 1 || got[0] != tt.want {
				t.Errorf("got %q\nwant %q", got, tt.want)
			}
		})
	}
	var nilAnalysis *Analysis
	if got := nilAnalysis.MaintainerDomainNotices(); got != nil {
		t.Errorf("nil analysis: %q", got)
	}
	none := &Analysis{MaintainerDomain: &MaintainerDomainResult{Findings: []MaintainerDomainFinding{{Kind: MaintainerDomainNormal}, {Kind: MaintainerDomainReregisteredAfter}}}}
	if got := none.MaintainerDomainNotices(); len(got) != 0 || none.HasMaintainerDomainNotice() {
		t.Errorf("no notice kinds: %q", got)
	}
}

// TestWarrantsNotice pins which kinds produce a notice.
func TestWarrantsNotice(t *testing.T) {
	t.Parallel()
	want := map[MaintainerDomainKind]bool{
		MaintainerDomainUnchecked: false, MaintainerDomainLookupFailed: false, MaintainerDomainUnregistered: true,
		MaintainerDomainLapsing: true, MaintainerDomainReregisteredAfter: false, MaintainerDomainReregisteredBefore: true,
		MaintainerDomainNormal: false,
	}
	for kind, w := range want {
		if got := kind.WarrantsNotice(); got != w {
			t.Errorf("%s: got %v, want %v", kind, got, w)
		}
	}
}

// TestHasDeletionStatus checks case-insensitive matching of deletion statuses.
func TestHasDeletionStatus(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		statuses []string
		want     bool
	}{
		{nil, false}, {[]string{"active"}, false}, {[]string{"active", "Redemption Period"}, true}, {[]string{"PENDING DELETE"}, true},
	} {
		if got := HasDeletionStatus(tc.statuses); got != tc.want {
			t.Errorf("%q: got %v, want %v", tc.statuses, got, tc.want)
		}
	}
}
