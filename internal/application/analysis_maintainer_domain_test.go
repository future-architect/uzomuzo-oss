package application

import (
	"context"
	"testing"
	"time"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

type fakeMaintainerDomainChecker struct {
	calls      int
	address    domain.MaintainerAddress
	registered time.Time
}

func (f *fakeMaintainerDomainChecker) Check(_ context.Context, address domain.MaintainerAddress, base time.Time) domain.MaintainerDomainFinding {
	f.calls++
	f.address = address
	return domain.MaintainerDomainFinding{Account: address.Account, Domain: "atlantis-software.net", FirstSeen: address.FirstSeen, Registered: f.registered, Kind: domain.ClassifyMaintainerDomain(address.FirstSeen, base, domain.DomainRecord{Status: domain.DomainFound, Registered: f.registered})}
}

func TestMaintainerDomainIsScanOnly(t *testing.T) {
	first := time.Date(2020, 11, 11, 0, 0, 0, 0, time.UTC)
	registered := time.Date(2026, 5, 7, 11, 49, 33, 0, time.UTC)
	purl := "pkg:npm/node-ipc@12.0.0"
	newService := func() (*AnalysisService, *domain.Analysis) {
		a := &domain.Analysis{Package: &domain.Package{PURL: purl, Ecosystem: "npm", Version: "12.0.0"}, ReleaseHistory: &domain.ReleaseHistory{Registry: domain.RegistryNpm, Maintainers: []domain.MaintainerAddress{{Account: "atiertant", Email: "a.tiertant@atlantis-software.net", FirstSeen: first}}}}
		s := NewAnalysisService(&fakeAnalysisSource{analyses: map[string]*domain.Analysis{purl: a}})
		factory, _ := newFakeEOLFactory(map[string]domain.EOLStatus{})
		s.newEOLEvaluator = factory
		return s, a
	}
	s, a := newService()
	if _, err := s.ProcessBatchPURLs(context.Background(), []string{purl}); err != nil {
		t.Fatal(err)
	}
	if a.MaintainerDomain != nil {
		t.Fatalf("flag off: result=%+v", a.MaintainerDomain)
	}
	s, a = newService()
	fake := &fakeMaintainerDomainChecker{registered: registered}
	s.maintainerDomainChecker = fake
	if _, err := s.ProcessBatchPURLs(context.Background(), []string{purl}); err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 || fake.address.Account != "atiertant" || !fake.address.FirstSeen.Equal(first) {
		t.Errorf("calls=%d address=%+v", fake.calls, fake.address)
	}
	if a.MaintainerDomain == nil || len(a.MaintainerDomain.Flagged()) != 1 || a.MaintainerDomain.Flagged()[0].Kind != domain.MaintainerDomainReregisteredBefore {
		t.Errorf("result=%+v", a.MaintainerDomain)
	}
}
