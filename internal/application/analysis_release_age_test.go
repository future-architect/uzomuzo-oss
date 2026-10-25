package application

import (
	"context"
	"testing"
	"time"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

func TestProcessBatchPURLsReleaseAgeUsesConfiguredWindow(t *testing.T) {
	now := time.Now().UTC()
	makeAnalysis := func(purl string, at time.Time) *domain.Analysis {
		return &domain.Analysis{
			OriginalPURL:   purl,
			Package:        &domain.Package{PURL: purl, Ecosystem: "npm", Version: "1.0.0"},
			ReleaseHistory: &domain.ReleaseHistory{Registry: domain.RegistryNpm, PublishedAt: map[string]time.Time{"1.0.0": at}},
		}
	}
	analyses := map[string]*domain.Analysis{
		"pkg:npm/recent@1.0.0":  makeAnalysis("pkg:npm/recent@1.0.0", now.Add(-12*time.Hour)),
		"pkg:npm/older@1.0.0":   makeAnalysis("pkg:npm/older@1.0.0", now.Add(-36*time.Hour)),
		"pkg:npm/unknown@1.0.0": makeAnalysis("pkg:npm/unknown@1.0.0", time.Time{}),
	}
	cfg := config.NewConfigWithDefaults()
	cfg.Lifecycle.YoungReleaseDays = 1
	svc := NewAnalysisService(&fakeAnalysisSource{analyses: analyses})
	svc.cfg = cfg
	factory, _ := newFakeEOLFactory(map[string]domain.EOLStatus{})
	svc.newEOLEvaluator = factory
	got, err := svc.ProcessBatchPURLs(context.Background(), []string{"pkg:npm/recent@1.0.0", "pkg:npm/older@1.0.0", "pkg:npm/unknown@1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if r := got["pkg:npm/recent@1.0.0"].ReleaseAge; r == nil || r.Status != domain.ReleaseAgeYoung || r.WindowDays != 1 ||
		r.Source != domain.ReleaseAgeSourceNpm || !r.PublishedAt.Equal(now.Add(-12*time.Hour)) {
		t.Fatalf("recent: %+v", r)
	}
	if r := got["pkg:npm/older@1.0.0"].ReleaseAge; r != nil {
		t.Fatalf("old: %+v", r)
	}
	if r := got["pkg:npm/unknown@1.0.0"].ReleaseAge; r == nil || r.Status != domain.ReleaseAgeUnknown || r.WindowDays != 1 {
		t.Fatalf("unknown: %+v", r)
	}
}
