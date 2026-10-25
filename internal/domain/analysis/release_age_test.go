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
		Package:      &Package{PURL: "pkg:npm/example@2.0.0-beta.1", Ecosystem: "npm", Version: "2.0.0-beta.1"},
		ReleaseInfo:  &ReleaseInfo{RequestedVersion: &VersionDetail{Version: "2.0.0-beta.1", PublishedAt: now.Add(-time.Hour)}},
	}
	tests := []struct {
		name        string
		at          time.Time
		wantStatus  ReleaseAgeStatus
		wantAgeDays int
	}{
		{"exact window", now.Add(-window), ReleaseAgeYoung, 7},
		{"47 hours", now.Add(-47 * time.Hour), ReleaseAgeYoung, 1},
		{"one second older", now.Add(-window - time.Second), "", 0},
		{"future publish time", now.Add(time.Second), ReleaseAgeUnknown, 0},
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
			if got == nil {
				t.Fatalf("got nil, want status %s", tc.wantStatus)
			}
			if got.Status != tc.wantStatus || got.WindowDays != 7 || got.AgeDays != tc.wantAgeDays {
				t.Fatalf("got %+v, want status %s, window 7, age %d", got, tc.wantStatus, tc.wantAgeDays)
			}
			if tc.wantStatus == ReleaseAgeYoung && (got.Source != ReleaseAgeSourceNpm || !got.PublishedAt.Equal(tc.at) || !got.Until.Equal(tc.at.Add(window))) {
				t.Fatalf("young fields: %+v", got)
			}
			if tc.wantStatus == ReleaseAgeUnknown && (got.Source != "" || !got.PublishedAt.IsZero() || !got.Until.IsZero()) {
				t.Fatalf("unknown fields: %+v", got)
			}
		})
	}
}

func TestEvaluateReleaseAgeFallbackAndIdentity(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	window := 7 * 24 * time.Hour
	newAnalysis := func() *Analysis {
		return &Analysis{
			OriginalPURL: "pkg:pypi/example@1.0.0",
			Package:      &Package{PURL: "pkg:pypi/example@1.0.0", Ecosystem: "pypi", Version: "1.0.0"},
			ReleaseInfo:  &ReleaseInfo{RequestedVersion: &VersionDetail{Version: "1.0.0", PublishedAt: now.Add(-3 * time.Hour)}},
		}
	}
	a := newAnalysis()
	a.PyPIPublishedAt = now.Add(-2 * time.Hour)
	if got := a.EvaluateReleaseAge(now, window); got == nil || got.Source != ReleaseAgeSourcePyPI || !got.PublishedAt.Equal(a.PyPIPublishedAt) {
		t.Fatalf("PyPI precedence: %+v", got)
	}
	a.PyPIPublishedAt = time.Time{}
	if got := a.EvaluateReleaseAge(now, window); got == nil || got.Source != ReleaseAgeSourceDepsDev {
		t.Fatalf("deps.dev fallback: %+v", got)
	}
	a.ReleaseInfo = nil
	if got := a.EvaluateReleaseAge(now, window); got == nil || got.Status != ReleaseAgeUnknown || !got.PublishedAt.IsZero() {
		t.Fatalf("unknown time: %+v", got)
	}

	t.Run("normalized names match", func(t *testing.T) {
		for _, tc := range []struct{ input, analysed string }{
			{"pkg:pypi/Typing_Extensions@1.0.0", "pkg:pypi/typing-extensions@1.0.0"},
			{"pkg:npm/%40Scope/Name@1.0.0", "pkg:npm/%40scope/name@1.0.0"},
		} {
			m := newAnalysis()
			m.OriginalPURL = tc.input
			m.Package.PURL = tc.analysed
			m.Package.Ecosystem = strings.TrimPrefix(strings.SplitN(tc.analysed, "/", 2)[0], "pkg:")
			if got := m.EvaluateReleaseAge(now, window); got == nil || got.Status != ReleaseAgeYoung {
				t.Fatalf("%s vs %s: %+v", tc.input, tc.analysed, got)
			}
		}
	})

	for _, tc := range []struct {
		name      string
		purl      string
		pkgPURL   string
		version   string
		ecosystem string
	}{
		{"version mismatch", "pkg:pypi/example@2.0.0", "pkg:pypi/example@1.0.0", "1.0.0", "pypi"},
		{"ecosystem mismatch", "pkg:npm/example@1.0.0", "pkg:pypi/example@1.0.0", "1.0.0", "pypi"},
		{"name mismatch", "pkg:pypi/alpha@1.0.0", "pkg:pypi/beta@1.0.0", "1.0.0", "pypi"},
		{"namespace mismatch", "pkg:npm/%40alpha/lib@1.0.0", "pkg:npm/%40beta/lib@1.0.0", "1.0.0", "npm"},
		{"analysed PURL unparsable", "pkg:pypi/example@1.0.0", "", "1.0.0", "pypi"},
		{"invalid versioned PURL", "pkg:bad type/example@1.0.0", "pkg:bad type/example@1.0.0", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mismatch := newAnalysis()
			mismatch.OriginalPURL = tc.purl
			mismatch.Package = &Package{PURL: tc.pkgPURL, Ecosystem: tc.ecosystem, Version: tc.version}
			got := mismatch.EvaluateReleaseAge(now, window)
			if got == nil || got.Status != ReleaseAgeUnknown || !got.PublishedAt.IsZero() || got.Source != "" || got.WindowDays != 7 {
				t.Fatalf("mismatched identity: %+v", got)
			}
		})
	}
	a.OriginalPURL = "pkg:pypi/example"
	if got := a.EvaluateReleaseAge(now, window); got != nil {
		t.Fatalf("unversioned input: %+v", got)
	}
	a.OriginalPURL = "https://github.com/owner/repo"
	if got := a.EvaluateReleaseAge(now, window); got != nil {
		t.Fatalf("unparsable unversioned input: %+v", got)
	}
	a.OriginalPURL = "pkg:pypi/example@1.0.0"
	a.Package = nil
	if got := a.EvaluateReleaseAge(now, window); got != nil {
		t.Fatalf("no identity: %+v", got)
	}
}

func TestReleaseAgeNotice(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		ageDays int
		want    string
	}{
		{0, "less than a day before this scan"},
		{1, "1 day before this scan"},
		{5, "5 days before this scan"},
	} {
		a := &Analysis{ReleaseAge: &ReleaseAge{Status: ReleaseAgeYoung, PublishedAt: now.Add(-2 * time.Hour), Until: now.Add(7*24*time.Hour - 2*time.Hour), WindowDays: 7, AgeDays: tc.ageDays}}
		notice := a.ReleaseAgeNotice()
		want := "This version was published on 2026-10-22, " + tc.want + ". Most malicious-package records appear within a week of publication, so a clean malicious-package check is not yet conclusive. If you can, wait until 2026-10-29 before adopting it, or check who published it."
		if notice != want {
			t.Fatalf("notice:\n got %q\nwant %q", notice, want)
		}
	}
}

func TestReleaseAgeNoticeSuppression(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	a := &Analysis{ReleaseAge: &ReleaseAge{Status: ReleaseAgeYoung, PublishedAt: now.Add(-2 * time.Hour), Until: now.Add(7*24*time.Hour - 2*time.Hour), WindowDays: 7}}
	a.DormantRelease = &DormantRelease{Burst: &DormantBurst{}}
	if notice := a.ReleaseAgeNotice(); notice != "" {
		t.Fatalf("dormant notice: %q", notice)
	}
	a.DormantRelease = nil
	a.MaliciousState = &MaliciousState{Status: MaliciousStatusFlagged}
	if notice := a.ReleaseAgeNotice(); notice != "" {
		t.Fatalf("malicious notice: %q", notice)
	}
	a.MaliciousState = nil
	a.ReleaseAge = &ReleaseAge{Status: ReleaseAgeUnknown, WindowDays: 7}
	if notice := a.ReleaseAgeNotice(); notice != "" {
		t.Fatalf("unknown notice: %q", notice)
	}
}
