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

// Check returns a fixed domain classification for application tests.
func (f *fakeMaintainerDomainChecker) Check(_ context.Context, address domain.MaintainerAddress, base time.Time) domain.MaintainerDomainFinding {
	f.calls++
	f.address = address
	return domain.MaintainerDomainFinding{Account: address.Account, Domain: "atlantis-software.net", FirstSeen: address.FirstSeen, Registered: f.registered, Kind: domain.ClassifyMaintainerDomain(address.FirstSeen, base, domain.DomainRecord{Status: domain.DomainFound, Registered: f.registered})}
}

// TestMaintainerDomainIsScanOnly checks the opt-in scan path.
func TestMaintainerDomainIsScanOnly(t *testing.T) {
	first := time.Date(2020, 11, 11, 0, 0, 0, 0, time.UTC)
	registered := time.Date(2026, 5, 7, 11, 49, 33, 0, time.UTC)
	purl := "pkg:npm/node-ipc@12.0.0"
	newService := func() (*AnalysisService, *domain.Analysis) {
		a := &domain.Analysis{Package: &domain.Package{PURL: purl, Ecosystem: "npm", Version: "12.0.0"}, ReleaseHistory: &domain.ReleaseHistory{Registry: domain.RegistryNpm, Maintainers: []domain.MaintainerAddress{{Account: "atiertant", Email: "a.tiertant@atlantis-software.net", FirstSeen: first}}}}
		s := NewAnalysisService(&fakeAnalysisSource{analyses: map[string]*domain.Analysis{purl: a}})
		factory, _ := newFakeEOLFactory(map[string]domain.EOLStatus{}) // call counter unused
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

// TestCheckMaintainerDomainsSkips checks which analyses the check leaves alone.
func TestCheckMaintainerDomainsSkips(t *testing.T) {
	now := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	address := domain.MaintainerAddress{Account: "atiertant", Email: "a.tiertant@atlantis-software.net", FirstSeen: time.Date(2020, 11, 11, 0, 0, 0, 0, time.UTC)}
	analyses := map[string]*domain.Analysis{
		"nil":            nil,
		"no history":     {},
		"other registry": {ReleaseHistory: &domain.ReleaseHistory{Registry: "pypi", Maintainers: []domain.MaintainerAddress{address}}},
		"npm":            {ReleaseHistory: &domain.ReleaseHistory{Registry: domain.RegistryNpm, Maintainers: []domain.MaintainerAddress{address}}},
	}
	fake := &fakeMaintainerDomainChecker{registered: time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)}
	s := &AnalysisService{maintainerDomainChecker: fake}
	s.checkMaintainerDomains(context.Background(), analyses, now)
	if fake.calls != 1 {
		t.Errorf("calls=%d, want 1", fake.calls)
	}
	if analyses["no history"].MaintainerDomain != nil || analyses["other registry"].MaintainerDomain != nil {
		t.Errorf("non-npm analyses got a result")
	}
	if analyses["npm"].MaintainerDomain == nil || len(analyses["npm"].MaintainerDomain.Findings) != 1 {
		t.Errorf("npm result=%+v", analyses["npm"].MaintainerDomain)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fake = &fakeMaintainerDomainChecker{}
	s = &AnalysisService{maintainerDomainChecker: fake}
	cancelled := map[string]*domain.Analysis{"npm": {ReleaseHistory: &domain.ReleaseHistory{Registry: domain.RegistryNpm, Maintainers: []domain.MaintainerAddress{address}}}}
	s.checkMaintainerDomains(ctx, cancelled, now)
	if fake.calls != 0 || cancelled["npm"].MaintainerDomain != nil {
		t.Errorf("cancelled run: calls=%d result=%+v", fake.calls, cancelled["npm"].MaintainerDomain)
	}
}
