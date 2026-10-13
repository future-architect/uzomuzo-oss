package application

import (
	"context"
	"testing"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// TestProcessBatchPURLs_SetsDormantReleaseMarker pins that the service puts the
// ADR-0028 marker on the analyses it returns, including one whose deps.dev
// lookup failed: the release history comes from the registry, not deps.dev.
func TestProcessBatchPURLs_SetsDormantReleaseMarker(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name     string
		version  string
		err      error
		wantKind domain.DormantReleaseKind
	}{
		{name: "fresh return is marked", version: "1.0.1", wantKind: domain.DormantReleaseFresh},
		{name: "marked even when deps.dev failed", version: "1.0.1", err: common.NewResourceNotFoundError("package not found in deps.dev"), wantKind: domain.DormantReleaseFresh},
		{name: "release before the silence is not marked", version: "1.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			purl := "pkg:npm/quiet@" + tt.version
			a := &domain.Analysis{
				Package: &domain.Package{PURL: purl, Ecosystem: "npm", Version: tt.version},
				Error:   tt.err,
				ReleaseHistory: &domain.ReleaseHistory{
					Registry: domain.RegistryNpm,
					PublishedAt: map[string]time.Time{
						"1.0.0": now.AddDate(0, 0, -402),
						"1.0.1": now.AddDate(0, 0, -2),
					},
				},
			}
			factory, _ := newFakeEOLFactory(map[string]domain.EOLStatus{})
			svc := NewAnalysisService(&fakeAnalysisSource{analyses: map[string]*domain.Analysis{purl: a}})
			svc.newEOLEvaluator = factory

			results, err := svc.ProcessBatchPURLs(context.Background(), []string{purl})
			if err != nil {
				t.Fatalf("ProcessBatchPURLs() unexpected error: %v", err)
			}
			got := results[purl]
			if got == nil {
				t.Fatalf("no result for %q", purl)
			}
			if tt.wantKind == "" {
				if got.DormantRelease != nil {
					t.Fatalf("DormantRelease = %+v, want nil", got.DormantRelease)
				}
				return
			}
			if got.DormantRelease == nil {
				t.Fatal("DormantRelease = nil, want a marker")
			}
			if got.DormantRelease.Kind != tt.wantKind || got.DormantRelease.Burst == nil || got.DormantRelease.Burst.SilentDays != 400 {
				t.Fatalf("DormantRelease = %+v, want kind %q with 400 silent days", got.DormantRelease, tt.wantKind)
			}
		})
	}
}
