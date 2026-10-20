package analysis

import (
	"testing"
	"time"
)

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
