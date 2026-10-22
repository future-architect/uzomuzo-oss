package analysis

import (
	"strings"
	"testing"
	"time"
)

func TestEvaluateReleaseAgeSourcesAndBoundary(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	window := 7 * 24 * time.Hour
	base := &Analysis{
		OriginalPURL: "pkg:npm/example@2.0.0-beta.1",
		Package:      &Package{Ecosystem: "npm", Version: "2.0.0-beta.1"},
		ReleaseInfo:  &ReleaseInfo{RequestedVersion: &VersionDetail{Version: "2.0.0-beta.1", PublishedAt: now.Add(-time.Hour)}},
	}
	tests := []struct {
		name       string
		at         time.Time
		wantStatus string
	}{
		{"exact window", now.Add(-window), "young"},
		{"one second older", now.Add(-window - time.Second), ""},
		{"future publish time", now.Add(time.Second), "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := *base
			a.ReleaseHistory = &ReleaseHistory{Registry: RegistryNpm, PublishedAt: map[string]time.Time{"2.0.0-beta.1": tc.at}}
			got := a.EvaluateReleaseAge(now, window)
			if tc.wantStatus == "" {
				if got != nil {
					t.Fatalf("got %+v, want nil", got)
				}
				return
			}
			if got == nil || got.Status != tc.wantStatus || (tc.wantStatus == "young" && got.Source != "npm") {
				t.Fatalf("got %+v, want status %s from npm", got, tc.wantStatus)
			}
			if tc.wantStatus == "young" && (!got.PublishedAt.Equal(tc.at) || !got.Until.Equal(tc.at.Add(window))) {
				t.Fatalf("publish bounds: %+v", got)
			}
		})
	}
}

func TestEvaluateReleaseAgeFallbackAndIdentity(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	newAnalysis := func() *Analysis {
		return &Analysis{OriginalPURL: "pkg:pypi/example@1.0.0", Package: &Package{Ecosystem: "pypi", Version: "1.0.0"}}
	}
	a := newAnalysis()
	a.ReleaseInfo = &ReleaseInfo{RequestedVersion: &VersionDetail{Version: "1.0.0", PublishedAt: now.Add(-3 * time.Hour)}}
	a.PyPIPublishedAt = now.Add(-2 * time.Hour)
	if got := a.EvaluateReleaseAge(now, 7*24*time.Hour); got == nil || got.Source != "pypi" || !got.PublishedAt.Equal(a.PyPIPublishedAt) {
		t.Fatalf("PyPI precedence: %+v", got)
	}
	a.PyPIPublishedAt = time.Time{}
	if got := a.EvaluateReleaseAge(now, 7*24*time.Hour); got == nil || got.Source != "deps.dev" {
		t.Fatalf("deps.dev fallback: %+v", got)
	}
	a.ReleaseInfo = nil
	if got := a.EvaluateReleaseAge(now, 7*24*time.Hour); got == nil || got.Status != "unknown" || !got.PublishedAt.IsZero() {
		t.Fatalf("unknown time: %+v", got)
	}
	a.OriginalPURL = "pkg:pypi/example"
	if got := a.EvaluateReleaseAge(now, 7*24*time.Hour); got != nil {
		t.Fatalf("unversioned input: %+v", got)
	}
	a.OriginalPURL = "pkg:pypi/example@1.0.0"
	a.Package = nil
	if got := a.EvaluateReleaseAge(now, 7*24*time.Hour); got != nil {
		t.Fatalf("no identity: %+v", got)
	}
}

func TestReleaseAgeNoticeSuppression(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	a := &Analysis{ReleaseAge: &ReleaseAge{Status: "young", PublishedAt: now.Add(-2 * time.Hour), Until: now.Add(7*24*time.Hour - 2*time.Hour), WindowDays: 7, AgeDays: 0}}
	if notice := a.ReleaseAgeNotice(); !strings.Contains(notice, "0 days") || !strings.Contains(notice, "2026-10-29") {
		t.Fatalf("notice: %q", notice)
	}
	a.DormantRelease = &DormantRelease{Burst: &DormantBurst{}}
	if notice := a.ReleaseAgeNotice(); notice != "" {
		t.Fatalf("dormant notice: %q", notice)
	}
	a.DormantRelease = nil
	a.MaliciousState = &MaliciousState{Status: MaliciousStatusFlagged}
	if notice := a.ReleaseAgeNotice(); notice != "" {
		t.Fatalf("malicious notice: %q", notice)
	}
}
