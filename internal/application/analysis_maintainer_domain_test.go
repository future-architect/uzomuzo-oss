package application

import (
	"context"
	"strings"
	"testing"
	"time"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

type fakeMaintainerDomainChecker struct {
	calls      int
	address    domain.MaintainerAddress
	registered time.Time
	onCheck    func()
}

// Check returns a fixed domain classification for application tests.
func (f *fakeMaintainerDomainChecker) Check(_ context.Context, address domain.MaintainerAddress, base time.Time) domain.MaintainerDomainFinding {
	f.calls++
	f.address = address
	if f.onCheck != nil {
		f.onCheck()
	}
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

	// Cancelled during the first of two addresses: no partial result is kept.
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	fake = &fakeMaintainerDomainChecker{onCheck: cancel}
	s = &AnalysisService{maintainerDomainChecker: fake}
	partial := map[string]*domain.Analysis{"npm": {ReleaseHistory: &domain.ReleaseHistory{Registry: domain.RegistryNpm, Maintainers: []domain.MaintainerAddress{address, address}}}}
	s.checkMaintainerDomains(ctx, partial, now)
	if fake.calls != 1 || partial["npm"].MaintainerDomain != nil {
		t.Errorf("cancelled mid-package: calls=%d result=%+v", fake.calls, partial["npm"].MaintainerDomain)
	}
}

type refreshingChecker struct {
	fakeMaintainerDomainChecker
	refreshes int
}

// RefreshBootstrap counts refreshes for application tests.
func (r *refreshingChecker) RefreshBootstrap(context.Context) domain.RDAPBootstrapInfo {
	r.refreshes++
	return domain.RDAPBootstrapInfo{Source: domain.RDAPBootstrapLive, Publication: "2026-10-20T00:00:00Z"}
}

// TestRDAPBootstrapRefreshedOnceWhenNeeded checks that the service list is
// refreshed once, and only when an npm package is checked.
func TestRDAPBootstrapRefreshedOnceWhenNeeded(t *testing.T) {
	now := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	address := domain.MaintainerAddress{Account: "atiertant", Email: "a.tiertant@atlantis-software.net", FirstSeen: time.Date(2020, 11, 11, 0, 0, 0, 0, time.UTC)}
	checker := &refreshingChecker{}
	s := &AnalysisService{maintainerDomainChecker: checker}

	s.checkMaintainerDomains(context.Background(), map[string]*domain.Analysis{"pypi": {ReleaseHistory: &domain.ReleaseHistory{Registry: "pypi"}}}, now)
	if checker.refreshes != 0 || s.RDAPBootstrap() != nil {
		t.Fatalf("no npm package: refreshes=%d info=%+v", checker.refreshes, s.RDAPBootstrap())
	}
	npm := func() map[string]*domain.Analysis {
		return map[string]*domain.Analysis{
			"a": {ReleaseHistory: &domain.ReleaseHistory{Registry: domain.RegistryNpm, Maintainers: []domain.MaintainerAddress{address}}},
			"b": {ReleaseHistory: &domain.ReleaseHistory{Registry: domain.RegistryNpm, Maintainers: []domain.MaintainerAddress{address}}},
		}
	}
	s.checkMaintainerDomains(context.Background(), npm(), now)
	s.checkMaintainerDomains(context.Background(), npm(), now)
	if checker.refreshes != 1 {
		t.Errorf("refreshes=%d, want 1 across batches", checker.refreshes)
	}
	if got := s.RDAPBootstrap(); got == nil || got.Source != domain.RDAPBootstrapLive || got.Publication != "2026-10-20T00:00:00Z" {
		t.Errorf("info=%+v", got)
	}
	if off := (&AnalysisService{}); off.RDAPBootstrap() != nil {
		t.Errorf("check off: info=%+v, want nil", off.RDAPBootstrap())
	}
}

func TestMaintainerDomainPublisherEvidenceUsesAllFetchedPackuments(t *testing.T) {
	first := time.Date(2020, 11, 11, 0, 0, 0, 0, time.UTC)
	registered := time.Date(2026, 5, 7, 11, 49, 33, 0, time.UTC)
	older := time.Date(2024, 8, 12, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC)
	purl := "pkg:npm/node-ipc@12.0.0"
	otherPURL := "pkg:npm/example@1.0.0"
	a := &domain.Analysis{Package: &domain.Package{PURL: purl, Ecosystem: "npm", Version: "12.0.0"}, ReleaseHistory: &domain.ReleaseHistory{Registry: domain.RegistryNpm, Maintainers: []domain.MaintainerAddress{{Account: "atiertant", Email: "a.tiertant@atlantis-software.net", FirstSeen: first}}, PublisherHistory: domain.PublisherPackageHistory{Latest: map[string][]domain.PublisherPublish{"atiertant": {{Package: "node-ipc", Version: "12.0.0", PublishedAt: older, EmailDomain: "atlantis-software.net", RegistrableDomain: "atlantis-software.net"}}}}}}
	other := &domain.Analysis{Package: &domain.Package{PURL: otherPURL, Ecosystem: "npm", Version: "1.0.0"}, ReleaseHistory: &domain.ReleaseHistory{Registry: domain.RegistryNpm, PublisherHistory: domain.PublisherPackageHistory{Latest: map[string][]domain.PublisherPublish{"atiertant": {{Package: "example", Version: "1.0.0", PublishedAt: newer, EmailDomain: "example.com", RegistrableDomain: "example.com"}}}}}}
	s := NewAnalysisService(&fakeAnalysisSource{analyses: map[string]*domain.Analysis{purl: a, otherPURL: other}})
	factory, _ := newFakeEOLFactory(map[string]domain.EOLStatus{})
	s.newEOLEvaluator = factory
	s.maintainerDomainChecker = &fakeMaintainerDomainChecker{registered: registered}
	if _, err := s.ProcessBatchPURLs(context.Background(), []string{purl, otherPURL}); err != nil {
		t.Fatal(err)
	}
	if a.MaintainerDomain == nil || len(a.MaintainerDomain.Flagged()) != 1 {
		t.Fatalf("findings=%+v", a.MaintainerDomain)
	}
	e := a.MaintainerDomain.Flagged()[0].PublisherEmailEvidence
	if e == nil || e.Status != domain.PublisherEmailObserved || e.DomainRelation != domain.PublisherDomainDifferent || e.RegistrationRelation != domain.PublisherRegistrationAfter || e.LatestObserved == nil || e.LatestObserved.Package != "example" {
		t.Fatalf("evidence=%+v", e)
	}
	publish := a.MaintainerDomain.Flagged()[0].PackagePublish
	if publish == nil || publish.Status != domain.PackagePublishFound || !publish.LastPublishedAt.Equal(older) || publish.Version != "12.0.0" {
		t.Fatalf("package publish=%+v", publish)
	}
	if got := a.MaintainerDomain.AnalysedVersionPublisher; got != domain.VersionPublisherUnknown {
		t.Errorf("version publisher=%q", got)
	}
}

func TestMaintainerDomainPublisherEvidenceUsesHiddenFindingSource(t *testing.T) {
	first := time.Date(2020, 11, 11, 0, 0, 0, 0, time.UTC)
	visible := time.Date(2024, 8, 12, 0, 0, 0, 0, time.UTC)
	purl := "pkg:npm/node-ipc@12.0.0"
	otherPURL := "pkg:npm/example@1.0.0"
	a := &domain.Analysis{Package: &domain.Package{PURL: purl, Ecosystem: "npm", Version: "12.0.0"}, ReleaseHistory: &domain.ReleaseHistory{Registry: domain.RegistryNpm, Maintainers: []domain.MaintainerAddress{{Account: "atiertant", Email: "a@atlantis-software.net", FirstSeen: first}}, PublisherHistory: domain.PublisherPackageHistory{HiddenPublishedAt: []time.Time{visible.Add(time.Hour)}}}}
	other := &domain.Analysis{Package: &domain.Package{PURL: otherPURL, Ecosystem: "npm", Version: "1.0.0"}, ReleaseHistory: &domain.ReleaseHistory{Registry: domain.RegistryNpm, PublisherHistory: domain.PublisherPackageHistory{Latest: map[string][]domain.PublisherPublish{"atiertant": {{Package: "example", Version: "1.0.0", PublishedAt: visible, EmailDomain: "atlantis-software.net", RegistrableDomain: "atlantis-software.net"}}}}}}
	s := NewAnalysisService(&fakeAnalysisSource{analyses: map[string]*domain.Analysis{purl: a, otherPURL: other}})
	factory, _ := newFakeEOLFactory(map[string]domain.EOLStatus{})
	s.newEOLEvaluator = factory
	s.maintainerDomainChecker = &fakeMaintainerDomainChecker{registered: first.Add(time.Hour)}
	if _, err := s.ProcessBatchPURLs(context.Background(), []string{purl, otherPURL}); err != nil {
		t.Fatal(err)
	}
	findings := a.MaintainerDomain.Flagged()
	if len(findings) != 1 || findings[0].PublisherEmailEvidence == nil || !findings[0].PublisherEmailEvidence.NewerPublishUnknown {
		t.Fatalf("findings=%+v", findings)
	}
}

func TestMaintainerDomainUsesAnalysedVersionPublisher(t *testing.T) {
	at := time.Date(2025, 3, 1, 10, 0, 0, 0, time.UTC)
	address := domain.MaintainerAddress{Account: "alice", Email: "alice@example.test", FirstSeen: at}
	h := &domain.ReleaseHistory{Registry: domain.RegistryNpm, Maintainers: []domain.MaintainerAddress{address}, PublisherKinds: map[string]domain.VersionPublisherKind{"1.0.0": domain.VersionPublisherOther, "2.0.0": domain.VersionPublisherTrustedPublishing}}
	a := &domain.Analysis{Package: &domain.Package{Version: "1.0.0"}, ReleaseHistory: h}
	s := &AnalysisService{maintainerDomainChecker: &fakeMaintainerDomainChecker{registered: at.Add(time.Hour)}}
	s.checkMaintainerDomains(context.Background(), map[string]*domain.Analysis{"a": a}, at.Add(2*time.Hour))
	if a.MaintainerDomain == nil || a.MaintainerDomain.AnalysedVersionPublisher != domain.VersionPublisherOther || len(a.MaintainerDomain.Findings) != 1 {
		t.Fatalf("result=%+v", a.MaintainerDomain)
	}
	if got := a.MaintainerDomainNotices(); len(got) != 1 || strings.Contains(got[0], "trusted publishing") {
		t.Errorf("notice=%q", got)
	}
	a.Package.Version = "2.0.0"
	s.checkMaintainerDomains(context.Background(), map[string]*domain.Analysis{"a": a}, at.Add(2*time.Hour))
	if a.MaintainerDomain.AnalysedVersionPublisher != domain.VersionPublisherTrustedPublishing {
		t.Errorf("version publisher=%q", a.MaintainerDomain.AnalysedVersionPublisher)
	}
}

// TestMaintainerDomainPackagePublishEdges covers an undated publish, an
// unnamed account and a missing analysed package.
func TestMaintainerDomainPackagePublishEdges(t *testing.T) {
	at := time.Date(2025, 3, 1, 10, 0, 0, 0, time.UTC)
	check := func(t *testing.T, a *domain.Analysis) domain.MaintainerDomainFinding {
		t.Helper()
		s := &AnalysisService{maintainerDomainChecker: &fakeMaintainerDomainChecker{registered: at.Add(time.Hour)}}
		s.checkMaintainerDomains(context.Background(), map[string]*domain.Analysis{"a": a}, at.Add(2*time.Hour))
		if a.MaintainerDomain == nil || len(a.MaintainerDomain.Flagged()) != 1 {
			t.Fatalf("result=%+v", a.MaintainerDomain)
		}
		return a.MaintainerDomain.Flagged()[0]
	}
	t.Run("undated publish is not_found", func(t *testing.T) {
		h := &domain.ReleaseHistory{Registry: domain.RegistryNpm, Maintainers: []domain.MaintainerAddress{{Account: "alice", Email: "alice@example.test", FirstSeen: at}}, PublisherKinds: map[string]domain.VersionPublisherKind{"1.0.0": domain.VersionPublisherOther}}
		a := &domain.Analysis{Package: &domain.Package{Version: "1.0.0"}, ReleaseHistory: h}
		f := check(t, a)
		if f.PackagePublish == nil || f.PackagePublish.Status != domain.PackagePublishNotFound {
			t.Fatalf("package publish=%+v", f.PackagePublish)
		}
		if got := a.MaintainerDomainNotices(); len(got) != 1 || !strings.Contains(got[0], "show no dated publish by this account.") {
			t.Errorf("notice=%q", got)
		}
	})
	t.Run("unnamed account has no package publish", func(t *testing.T) {
		h := &domain.ReleaseHistory{Registry: domain.RegistryNpm, Maintainers: []domain.MaintainerAddress{{Email: "x@example.test", FirstSeen: at}}}
		f := check(t, &domain.Analysis{Package: &domain.Package{Version: "1.0.0"}, ReleaseHistory: h})
		if f.PackagePublish != nil {
			t.Errorf("package publish=%+v", f.PackagePublish)
		}
	})
	t.Run("nil package is unknown", func(t *testing.T) {
		h := &domain.ReleaseHistory{Registry: domain.RegistryNpm, Maintainers: []domain.MaintainerAddress{{Account: "alice", Email: "alice@example.test", FirstSeen: at}}, PublisherKinds: map[string]domain.VersionPublisherKind{"1.0.0": domain.VersionPublisherTrustedPublishing}}
		a := &domain.Analysis{ReleaseHistory: h}
		check(t, a)
		if got := a.MaintainerDomain.AnalysedVersionPublisher; got != domain.VersionPublisherUnknown {
			t.Errorf("version publisher=%q", got)
		}
	})
}
