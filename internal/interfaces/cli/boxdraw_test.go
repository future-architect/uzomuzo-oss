package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	domainaudit "github.com/future-architect/uzomuzo-oss/internal/domain/audit"
	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
)

func TestBoxTitle(t *testing.T) {
	tests := []struct {
		name  string
		entry domainaudit.AuditEntry
		want  string
	}{
		{
			name:  "direct PURL",
			entry: domainaudit.AuditEntry{PURL: "pkg:npm/express@4.18.2"},
			want:  "pkg:npm/express@4.18.2",
		},
		{
			name:  "action source",
			entry: domainaudit.AuditEntry{PURL: "https://github.com/actions/checkout", Source: domainaudit.SourceActions},
			want:  "[action] https://github.com/actions/checkout",
		},
		{
			name:  "action-transitive source",
			entry: domainaudit.AuditEntry{PURL: "https://github.com/actions/cache", Source: domainaudit.SourceActionsTransitive},
			want:  "[action-transitive] https://github.com/actions/cache",
		},
		{
			name:  "SBOM transitive",
			entry: domainaudit.AuditEntry{PURL: "pkg:npm/body-parser@1.20.0", Relation: depparser.RelationTransitive},
			want:  "pkg:npm/body-parser@1.20.0 (transitive)",
		},
		{
			name:  "SBOM direct (no annotation)",
			entry: domainaudit.AuditEntry{PURL: "pkg:npm/express@4.18.2", Relation: depparser.RelationDirect},
			want:  "pkg:npm/express@4.18.2",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := boxTitle(&tt.entry)
			if got != tt.want {
				t.Errorf("boxTitle() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVerdictIcon(t *testing.T) {
	tests := []struct {
		verdict domainaudit.Verdict
		want    string
	}{
		{domainaudit.VerdictOK, "✅"},
		{domainaudit.VerdictCaution, "⚠️"},
		{domainaudit.VerdictReplace, "🔴"},
		{domainaudit.VerdictReview, "🔍"},
	}
	for _, tt := range tests {
		got := verdictIcon(tt.verdict)
		if got != tt.want {
			t.Errorf("verdictIcon(%q) = %q, want %q", tt.verdict, got, tt.want)
		}
	}
}

func TestWriteTopBar(t *testing.T) {
	var buf bytes.Buffer
	entry := &domainaudit.AuditEntry{PURL: "pkg:npm/express@4.18.2"}
	ctx := newBoxContext(&buf, entry, 60)
	if err := writeTopBar(ctx); err != nil {
		t.Fatalf("writeTopBar() error = %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "──") {
		t.Error("top bar missing ── prefix")
	}
	if !strings.Contains(output, "pkg:npm/express@4.18.2") {
		t.Error("top bar missing PURL title")
	}
}

func TestWriteLine(t *testing.T) {
	var buf bytes.Buffer
	entry := &domainaudit.AuditEntry{PURL: "test"}
	ctx := newBoxContext(&buf, entry, 60)
	if err := writeLine(ctx, "Score: %d/10", 8); err != nil {
		t.Fatalf("writeLine() error = %v", err)
	}
	output := buf.String()
	if !strings.HasPrefix(output, "│ ") {
		t.Errorf("line should start with '│ ', got %q", output)
	}
	if !strings.Contains(output, "Score: 8/10") {
		t.Error("line missing content")
	}
}

func TestWriteBoxOrigin_DirectPURL(t *testing.T) {
	var buf bytes.Buffer
	entry := &domainaudit.AuditEntry{PURL: "pkg:npm/express@4.18.2"}
	ctx := newBoxContext(&buf, entry, 60)
	if err := writeBoxOrigin(ctx); err != nil {
		t.Fatalf("writeBoxOrigin() error = %v", err)
	}
	if buf.Len() != 0 {
		t.Error("writeBoxOrigin should produce no output for direct PURL with no relation")
	}
}

func TestWriteBoxOrigin_DirectRelation(t *testing.T) {
	var buf bytes.Buffer
	entry := &domainaudit.AuditEntry{
		PURL:     "pkg:npm/express@4.18.2",
		Relation: depparser.RelationDirect,
	}
	ctx := newBoxContext(&buf, entry, 60)
	if err := writeBoxOrigin(ctx); err != nil {
		t.Fatalf("writeBoxOrigin() error = %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("writeBoxOrigin should produce no output for direct relation, got %q", buf.String())
	}
}

func TestWriteBoxOrigin_ActionTransitive(t *testing.T) {
	var buf bytes.Buffer
	entry := &domainaudit.AuditEntry{
		PURL:   "https://github.com/actions/cache",
		Source: domainaudit.SourceActionsTransitive,
		Via:    "https://github.com/aquasecurity/trivy-action",
	}
	ctx := newBoxContext(&buf, entry, 60)
	if err := writeBoxOrigin(ctx); err != nil {
		t.Fatalf("writeBoxOrigin() error = %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "├─ Origin") {
		t.Error("missing Origin section header")
	}
	if !strings.Contains(output, "Source: action-transitive") {
		t.Error("missing source line")
	}
	if !strings.Contains(output, "Via: https://github.com/aquasecurity/trivy-action") {
		t.Error("missing via line")
	}
}

func TestWriteBoxOrigin_SBOMTransitive(t *testing.T) {
	var buf bytes.Buffer
	entry := &domainaudit.AuditEntry{
		PURL:       "pkg:npm/body-parser@1.20.0",
		Relation:   depparser.RelationTransitive,
		ViaParents: []string{"express", "lodash"},
	}
	ctx := newBoxContext(&buf, entry, 60)
	if err := writeBoxOrigin(ctx); err != nil {
		t.Fatalf("writeBoxOrigin() error = %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "├─ Origin") {
		t.Error("missing Origin section header")
	}
	if !strings.Contains(output, "transitive (express, lodash)") {
		t.Error("missing relation with via parents")
	}
}

func TestWriteBoxVerdict(t *testing.T) {
	tests := []struct {
		name    string
		verdict domainaudit.Verdict
		label   analysis.MaintenanceStatus
		icon    string
	}{
		{"ok", domainaudit.VerdictOK, analysis.LabelActive, "✅"},
		{"caution", domainaudit.VerdictCaution, analysis.LabelStalled, "⚠️"},
		{"replace", domainaudit.VerdictReplace, analysis.LabelEOLConfirmed, "🔴"},
		{"review", domainaudit.VerdictReview, "", "🔍"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			a := &analysis.Analysis{}
			if tt.label != "" {
				a.AxisResults = map[analysis.AssessmentAxis]*analysis.AssessmentResult{
					analysis.LifecycleAxis: {Label: tt.label, Reason: "test reason"},
				}
			}
			entry := &domainaudit.AuditEntry{
				PURL:     "pkg:npm/test@1.0.0",
				Verdict:  tt.verdict,
				Analysis: a,
			}
			ctx := newBoxContext(&buf, entry, 60)
			if err := writeBoxVerdict(ctx); err != nil {
				t.Fatalf("writeBoxVerdict() error = %v", err)
			}
			output := buf.String()
			if !strings.Contains(output, tt.icon) {
				t.Errorf("missing verdict icon %q in output", tt.icon)
			}
			if !strings.Contains(output, "├─ Status") {
				t.Error("missing Status section header")
			}
		})
	}
}

func TestWriteBoxEOL_WithWarnings(t *testing.T) {
	scheduledDate := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	var buf bytes.Buffer
	entry := &domainaudit.AuditEntry{
		PURL:    "pkg:npm/test@1.0.0",
		Verdict: domainaudit.VerdictCaution,
		Analysis: &analysis.Analysis{
			EOL: analysis.EOLStatus{
				State:       analysis.EOLScheduled,
				ScheduledAt: &scheduledDate,
				Successor:   "pytest",
				Reason:      "End of support",
				Evidences: []analysis.EOLEvidence{
					{Source: "Catalog", Summary: "No longer maintained", Confidence: 0.95},
				},
			},
		},
	}
	ctx := newBoxContext(&buf, entry, 60)
	if err := writeBoxEOL(ctx); err != nil {
		t.Fatalf("writeBoxEOL() error = %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "⚠️ Scheduled EOL: 2025-06-01") {
		t.Error("missing scheduled EOL with warning emoji")
	}
	if !strings.Contains(output, "➡️ Successor: pytest") {
		t.Error("missing successor with arrow emoji")
	}
	if !strings.Contains(output, "Evidence (1)") {
		t.Error("missing evidence count")
	}
}

func TestWriteBoxHealth_Archived(t *testing.T) {
	var buf bytes.Buffer
	entry := &domainaudit.AuditEntry{
		PURL:    "pkg:npm/test@1.0.0",
		Verdict: domainaudit.VerdictReplace,
		Analysis: &analysis.Analysis{
			RepoURL: "github.com/test/repo",
			RepoState: &analysis.RepoState{
				IsArchived: true,
			},
			Repository: &analysis.Repository{
				StarsCount: 987,
			},
		},
	}
	ctx := newBoxContext(&buf, entry, 60)
	if err := writeBoxHealth(ctx); err != nil {
		t.Fatalf("writeBoxHealth() error = %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "📦 Archived") {
		t.Error("missing archived emoji")
	}
	if !strings.Contains(output, "987 stars") {
		t.Error("missing star count")
	}
}

func TestWriteBoxReleases_Advisories(t *testing.T) {
	var buf bytes.Buffer
	advisories := []analysis.Advisory{
		{ID: "CVE-2024-9999", Source: "CVE", CVSS3Score: 9.8, Severity: "CRITICAL", Title: "Remote code execution"},
		{ID: "GHSA-xxxx-yyyy", Source: "GHSA", CVSS3Score: 7.5, Severity: "HIGH", Title: "SQL Injection"},
		{ID: "CVE-2024-5678", Source: "CVE", CVSS3Score: 7.2, Severity: "HIGH", Title: "Path traversal"},
		{ID: "CVE-2024-1111", Source: "CVE", CVSS3Score: 4.3, Severity: "MEDIUM", Title: "Info disclosure"},
		{ID: "CVE-2024-0000", Source: "CVE", CVSS3Score: 0, Title: ""},
	}
	entry := &domainaudit.AuditEntry{
		PURL:    "pkg:npm/test@1.0.0",
		Verdict: domainaudit.VerdictCaution,
		Analysis: &analysis.Analysis{
			EffectivePURL: "pkg:npm/test@1.0.0",
			Package:       &analysis.Package{Ecosystem: "npm", PURL: "pkg:npm/test", Version: "1.0.0"},
			ReleaseInfo: &analysis.ReleaseInfo{
				StableVersion: &analysis.VersionDetail{
					Version:     "1.0.0",
					PublishedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					Advisories:  advisories,
				},
			},
		},
	}
	ctx := newBoxContext(&buf, entry, 60)
	if err := writeBoxReleases(ctx); err != nil {
		t.Fatalf("writeBoxReleases() error = %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "⚠️ Advisories: 5") {
		t.Error("missing advisory warning count")
	}
	if !strings.Contains(output, "max: CRITICAL 9.8") {
		t.Error("missing severity summary")
	}
	// Top 3 by severity should be shown (CRITICAL, HIGH, HIGH)
	if !strings.Contains(output, "CVE-2024-9999") {
		t.Error("missing highest severity advisory")
	}
	if !strings.Contains(output, "CRITICAL") {
		t.Error("missing CRITICAL severity label")
	}
	if !strings.Contains(output, "Remote code execution") {
		t.Error("missing advisory title")
	}
	// Should show 3 advisory lines, then truncation
	if !strings.Contains(output, "... and 2 more") {
		t.Error("missing truncation message")
	}
	if !strings.Contains(output, "deps.dev/npm/test/1.0.0") {
		t.Error("missing deps.dev version URL")
	}
}

func TestWriteBoxReleases_FewAdvisories(t *testing.T) {
	var buf bytes.Buffer
	entry := &domainaudit.AuditEntry{
		PURL:    "pkg:npm/test@1.0.0",
		Verdict: domainaudit.VerdictCaution,
		Analysis: &analysis.Analysis{
			EffectivePURL: "pkg:npm/test@1.0.0",
			Package:       &analysis.Package{Ecosystem: "npm", PURL: "pkg:npm/test", Version: "1.0.0"},
			ReleaseInfo: &analysis.ReleaseInfo{
				StableVersion: &analysis.VersionDetail{
					Version:     "1.0.0",
					PublishedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					Advisories: []analysis.Advisory{
						{ID: "GHSA-xxxx-yyyy", Source: "GHSA", CVSS3Score: 5.3, Severity: "MEDIUM", Title: "XSS vulnerability"},
					},
				},
			},
		},
	}
	ctx := newBoxContext(&buf, entry, 60)
	if err := writeBoxReleases(ctx); err != nil {
		t.Fatalf("writeBoxReleases() error = %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "GHSA-xxxx-yyyy") {
		t.Error("missing advisory ID")
	}
	if !strings.Contains(output, "MEDIUM") {
		t.Error("missing severity label")
	}
	if !strings.Contains(output, "XSS vulnerability") {
		t.Error("missing advisory title")
	}
	if strings.Contains(output, "... and") {
		t.Error("should not have truncation for <= 3 advisories")
	}
	// deps.dev link should still appear
	if !strings.Contains(output, "deps.dev/npm/test/1.0.0") {
		t.Error("missing deps.dev link")
	}
}

func TestWriteBoxReleases_UnknownSeverity(t *testing.T) {
	var buf bytes.Buffer
	entry := &domainaudit.AuditEntry{
		PURL:    "pkg:npm/test@1.0.0",
		Verdict: domainaudit.VerdictCaution,
		Analysis: &analysis.Analysis{
			EffectivePURL: "pkg:npm/test@1.0.0",
			Package:       &analysis.Package{Ecosystem: "npm", PURL: "pkg:npm/test", Version: "1.0.0"},
			ReleaseInfo: &analysis.ReleaseInfo{
				StableVersion: &analysis.VersionDetail{
					Version:     "1.0.0",
					PublishedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					Advisories: []analysis.Advisory{
						{ID: "CVE-2024-1234", Source: "CVE"},
						{ID: "CVE-2024-5678", Source: "CVE"},
					},
				},
			},
		},
	}
	ctx := newBoxContext(&buf, entry, 60)
	if err := writeBoxReleases(ctx); err != nil {
		t.Fatalf("writeBoxReleases() error = %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "CVE-2024-1234") {
		t.Error("missing advisory ID")
	}
	// No severity labels should appear
	if strings.Contains(output, "HIGH") || strings.Contains(output, "CRITICAL") {
		t.Error("should not show severity for unknown advisories")
	}
}

func TestAdvisorySeveritySummary(t *testing.T) {
	tests := []struct {
		name string
		vd   *analysis.VersionDetail
		want string
	}{
		{"nil", nil, ""},
		{"no advisories", &analysis.VersionDetail{}, ""},
		{"all known", &analysis.VersionDetail{
			Advisories: []analysis.Advisory{
				{CVSS3Score: 9.8}, {CVSS3Score: 7.5},
			},
		}, " (max: CRITICAL 9.8)"},
		{"mixed known/unknown", &analysis.VersionDetail{
			Advisories: []analysis.Advisory{
				{CVSS3Score: 7.5}, {CVSS3Score: 0},
			},
		}, " (max: HIGH 7.5, 1 unknown)"},
		{"all unknown", &analysis.VersionDetail{
			Advisories: []analysis.Advisory{
				{CVSS3Score: 0}, {CVSS3Score: 0},
			},
		}, " (2 unknown)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := advisorySeveritySummary(tt.vd)
			if got != tt.want {
				t.Errorf("advisorySeveritySummary() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWriteBoxReleases_Deprecated(t *testing.T) {
	var buf bytes.Buffer
	entry := &domainaudit.AuditEntry{
		PURL:    "pkg:npm/test@1.0.0",
		Verdict: domainaudit.VerdictCaution,
		Analysis: &analysis.Analysis{
			ReleaseInfo: &analysis.ReleaseInfo{
				RequestedVersion: &analysis.VersionDetail{
					Version:      "1.0.0",
					PublishedAt:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					IsDeprecated: true,
				},
			},
		},
	}
	ctx := newBoxContext(&buf, entry, 60)
	if err := writeBoxReleases(ctx); err != nil {
		t.Fatalf("writeBoxReleases() error = %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "⚠️ [DEPRECATED]") {
		t.Error("missing deprecated warning")
	}
}

func TestWriteBoxLinks(t *testing.T) {
	var buf bytes.Buffer
	entry := &domainaudit.AuditEntry{
		PURL:    "pkg:npm/express@4.18.2",
		Verdict: domainaudit.VerdictOK,
		Analysis: &analysis.Analysis{
			EffectivePURL: "pkg:npm/express@4.18.2",
			RepoURL:       "github.com/expressjs/express",
			PackageLinks: &analysis.PackageLinks{
				HomepageURL: "https://expressjs.com",
				RegistryURL: "https://www.npmjs.com/package/express",
			},
		},
	}
	ctx := newBoxContext(&buf, entry, 60)
	if err := writeBoxLinks(ctx); err != nil {
		t.Fatalf("writeBoxLinks() error = %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Homepage: https://expressjs.com") {
		t.Error("missing homepage link")
	}
	if !strings.Contains(output, "Repository: https://github.com/expressjs/express") {
		t.Error("missing repository link")
	}
	if !strings.Contains(output, "Registry: https://www.npmjs.com/package/express") {
		t.Error("missing registry link")
	}
	if !strings.Contains(output, "deps.dev: https://deps.dev/npm/express") {
		t.Error("missing deps.dev link")
	}
	if strings.Contains(output, "Scorecard") {
		t.Error("scorecard link should not be shown")
	}
}

func TestRenderBoxEntry_FullEntry(t *testing.T) {
	var buf bytes.Buffer
	entry := &domainaudit.AuditEntry{
		PURL:    "pkg:npm/express@4.18.2",
		Verdict: domainaudit.VerdictOK,
		Analysis: &analysis.Analysis{
			EffectivePURL: "pkg:npm/express@4.18.2",
			RepoURL:       "github.com/expressjs/express",
			AxisResults: map[analysis.AssessmentAxis]*analysis.AssessmentResult{
				analysis.LifecycleAxis: {Label: analysis.LabelActive, Reason: "Regular releases"},
			},
		},
	}
	if err := renderBoxEntry(&buf, entry); err != nil {
		t.Fatalf("renderBoxEntry() error = %v", err)
	}
	output := buf.String()

	// Structural assertions
	if !strings.Contains(output, "──") {
		t.Error("missing top bar")
	}
	if !strings.Contains(output, "├─ Status") {
		t.Error("missing Status section")
	}
	if !strings.Contains(output, "└─") {
		t.Error("missing bottom bar")
	}
	if !strings.Contains(output, "✅") {
		t.Error("missing OK verdict icon")
	}
	if !strings.Contains(output, "pkg:npm/express@4.18.2") {
		t.Error("missing PURL in output")
	}
}

func TestRenderBoxEntry_NilAnalysis(t *testing.T) {
	var buf bytes.Buffer
	entry := &domainaudit.AuditEntry{
		PURL:     "pkg:npm/unknown@1.0.0",
		Verdict:  domainaudit.VerdictReview,
		ErrorMsg: "package not found on deps.dev",
	}
	if err := renderBoxEntry(&buf, entry); err != nil {
		t.Fatalf("renderBoxEntry() error = %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "pkg:npm/unknown@1.0.0") {
		t.Error("missing PURL")
	}
	if !strings.Contains(output, "🔍") {
		t.Error("missing review icon")
	}
	if !strings.Contains(output, "package not found on deps.dev") {
		t.Error("missing error message")
	}
	if !strings.Contains(output, "└─") {
		t.Error("missing bottom bar")
	}
}

func TestRenderScanDetailed_BoxFormat(t *testing.T) {
	var buf bytes.Buffer
	entries := makeTestEntries()
	if err := renderScanDetailed(&buf, entries); err != nil {
		t.Fatalf("renderScanDetailed() error = %v", err)
	}
	output := buf.String()

	// Machine-parseable markers preserved
	if !strings.Contains(output, MarkerSummaryTableBegin) {
		t.Error("missing summary table marker")
	}
	if !strings.Contains(output, MarkerDetailedReportBegin) {
		t.Error("missing detailed report marker")
	}
	if !strings.Contains(output, "--- PURL 1 ---") {
		t.Error("missing PURL 1 marker")
	}
	if !strings.Contains(output, "--- PURL 3 ---") {
		t.Error("missing PURL 3 marker")
	}

	// Box-drawing characters present
	if !strings.Contains(output, "──") {
		t.Error("missing top bar")
	}
	if !strings.Contains(output, "├─") {
		t.Error("missing section separator")
	}
	if !strings.Contains(output, "└─") {
		t.Error("missing bottom bar")
	}
	if !strings.Contains(output, "│ ") {
		t.Error("missing content line prefix")
	}

	// Verdict icons present
	if !strings.Contains(output, "✅") {
		t.Error("missing OK verdict icon")
	}
	if !strings.Contains(output, "🔴") {
		t.Error("missing Replace verdict icon")
	}
	if !strings.Contains(output, "🔍") {
		t.Error("missing Review verdict icon")
	}
}

func TestShortenLicenseSource(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"depsdev-project-spdx", analysis.LicenseSourceDepsDevProjectSPDX, "depsdev"},
		{"depsdev-project-nonstandard", analysis.LicenseSourceDepsDevProjectNonStandard, "depsdev"},
		{"depsdev-version-spdx", analysis.LicenseSourceDepsDevVersionSPDX, "depsdev"},
		{"depsdev-version-raw", analysis.LicenseSourceDepsDevVersionRaw, "depsdev"},
		{"github-project-spdx", analysis.LicenseSourceGitHubProjectSPDX, "github"},
		{"github-project-nonstandard", analysis.LicenseSourceGitHubProjectNonStandard, "github"},
		{"github-version-spdx", analysis.LicenseSourceGitHubVersionSPDX, "github"},
		{"github-version-raw", analysis.LicenseSourceGitHubVersionRaw, "github"},
		{"project-fallback", analysis.LicenseSourceProjectFallback, "fallback"},
		{"derived-from-version", analysis.LicenseSourceDerivedFromVersion, "derived"},
		{"unknown-source", "unknown-source", "unknown-source"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shortenLicenseSource(tt.input)
			if got != tt.want {
				t.Errorf("shortenLicenseSource(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestWriteBoxLicense_SourceDisplay(t *testing.T) {
	t.Run("same_source_collapsed", func(t *testing.T) {
		var buf bytes.Buffer
		entry := &domainaudit.AuditEntry{
			PURL:    "pkg:npm/test@1.0.0",
			Verdict: domainaudit.VerdictOK,
			Analysis: &analysis.Analysis{
				ProjectLicense: analysis.ResolvedLicense{
					Identifier: "MIT", Source: analysis.LicenseSourceDepsDevProjectSPDX, IsSPDX: true,
				},
				RequestedVersionLicenses: []analysis.ResolvedLicense{
					{Identifier: "MIT", Source: analysis.LicenseSourceDepsDevVersionSPDX, IsSPDX: true},
				},
			},
		}
		ctx := newBoxContext(&buf, entry, 60)
		if err := writeBoxLicenses(ctx); err != nil {
			t.Fatalf("writeBoxLicense() error = %v", err)
		}
		output := buf.String()
		if !strings.Contains(output, "MIT (depsdev)") {
			t.Errorf("expected collapsed single source, got:\n%s", output)
		}
		if strings.Contains(output, "/") {
			t.Errorf("expected no slash separator when sources match, got:\n%s", output)
		}
	})

	t.Run("different_sources_labeled", func(t *testing.T) {
		var buf bytes.Buffer
		entry := &domainaudit.AuditEntry{
			PURL:    "pkg:npm/test@1.0.0",
			Verdict: domainaudit.VerdictOK,
			Analysis: &analysis.Analysis{
				ProjectLicense: analysis.ResolvedLicense{
					Identifier: "MIT", Source: analysis.LicenseSourceDepsDevProjectSPDX, IsSPDX: true,
				},
				RequestedVersionLicenses: []analysis.ResolvedLicense{
					{Identifier: "MIT", Source: analysis.LicenseSourceProjectFallback, IsSPDX: true},
				},
			},
		}
		ctx := newBoxContext(&buf, entry, 60)
		if err := writeBoxLicenses(ctx); err != nil {
			t.Fatalf("writeBoxLicense() error = %v", err)
		}
		output := buf.String()
		if !strings.Contains(output, "MIT (project: depsdev / version: fallback)") {
			t.Errorf("expected labeled sources, got:\n%s", output)
		}
	})
}

func TestWriteBoxReleases_VersionDeduplication(t *testing.T) {
	t.Run("stable_equals_requested", func(t *testing.T) {
		var buf bytes.Buffer
		entry := &domainaudit.AuditEntry{
			PURL:    "pkg:npm/test@1.0.0",
			Verdict: domainaudit.VerdictOK,
			Analysis: &analysis.Analysis{
				ReleaseInfo: &analysis.ReleaseInfo{
					StableVersion: &analysis.VersionDetail{
						Version:     "1.0.0",
						PublishedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					},
					RequestedVersion: &analysis.VersionDetail{
						Version:     "1.0.0",
						PublishedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					},
				},
			},
		}
		ctx := newBoxContext(&buf, entry, 60)
		if err := writeBoxReleases(ctx); err != nil {
			t.Fatalf("writeBoxReleases() error = %v", err)
		}
		output := buf.String()
		if !strings.Contains(output, "Stable: 1.0.0") {
			t.Error("missing Stable version")
		}
		if strings.Contains(output, "Requested:") {
			t.Error("Requested should be suppressed when equal to Stable")
		}
	})

	t.Run("prerelease_equals_stable", func(t *testing.T) {
		var buf bytes.Buffer
		entry := &domainaudit.AuditEntry{
			PURL:    "pkg:npm/test@1.0.0",
			Verdict: domainaudit.VerdictOK,
			Analysis: &analysis.Analysis{
				ReleaseInfo: &analysis.ReleaseInfo{
					StableVersion: &analysis.VersionDetail{
						Version:     "1.0.0",
						PublishedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					},
					PreReleaseVersion: &analysis.VersionDetail{
						Version:     "1.0.0",
						PublishedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					},
				},
			},
		}
		ctx := newBoxContext(&buf, entry, 60)
		if err := writeBoxReleases(ctx); err != nil {
			t.Fatalf("writeBoxReleases() error = %v", err)
		}
		output := buf.String()
		if strings.Contains(output, "Pre-release:") {
			t.Error("Pre-release should be suppressed when equal to Stable")
		}
	})

	t.Run("highest_equals_prerelease", func(t *testing.T) {
		var buf bytes.Buffer
		entry := &domainaudit.AuditEntry{
			PURL:    "pkg:npm/test@1.0.0",
			Verdict: domainaudit.VerdictOK,
			Analysis: &analysis.Analysis{
				ReleaseInfo: &analysis.ReleaseInfo{
					StableVersion: &analysis.VersionDetail{
						Version:     "1.0.0",
						PublishedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					},
					PreReleaseVersion: &analysis.VersionDetail{
						Version:     "2.0.0-beta.1",
						PublishedAt: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
					},
					MaxSemverVersion: &analysis.VersionDetail{
						Version:     "2.0.0-beta.1",
						PublishedAt: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
					},
				},
			},
		}
		ctx := newBoxContext(&buf, entry, 60)
		if err := writeBoxReleases(ctx); err != nil {
			t.Fatalf("writeBoxReleases() error = %v", err)
		}
		output := buf.String()
		if !strings.Contains(output, "Pre-release: 2.0.0-beta.1") {
			t.Error("missing Pre-release version")
		}
		if strings.Contains(output, "Highest (SemVer):") {
			t.Error("Highest should be suppressed when equal to Pre-release")
		}
	})

	t.Run("all_different", func(t *testing.T) {
		var buf bytes.Buffer
		entry := &domainaudit.AuditEntry{
			PURL:    "pkg:npm/test@0.9.0",
			Verdict: domainaudit.VerdictOK,
			Analysis: &analysis.Analysis{
				ReleaseInfo: &analysis.ReleaseInfo{
					StableVersion: &analysis.VersionDetail{
						Version:     "1.0.0",
						PublishedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					},
					PreReleaseVersion: &analysis.VersionDetail{
						Version:     "2.0.0-beta.1",
						PublishedAt: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
					},
					MaxSemverVersion: &analysis.VersionDetail{
						Version:     "2.0.0-rc.1",
						PublishedAt: time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC),
					},
					RequestedVersion: &analysis.VersionDetail{
						Version:     "0.9.0",
						PublishedAt: time.Date(2023, 6, 1, 0, 0, 0, 0, time.UTC),
					},
				},
			},
		}
		ctx := newBoxContext(&buf, entry, 60)
		if err := writeBoxReleases(ctx); err != nil {
			t.Fatalf("writeBoxReleases() error = %v", err)
		}
		output := buf.String()
		if !strings.Contains(output, "Stable: 1.0.0") {
			t.Error("missing Stable")
		}
		if !strings.Contains(output, "Pre-release: 2.0.0-beta.1") {
			t.Error("missing Pre-release")
		}
		if !strings.Contains(output, "Highest (SemVer): 2.0.0-rc.1") {
			t.Error("missing Highest")
		}
		if !strings.Contains(output, "Requested: 0.9.0") {
			t.Error("missing Requested")
		}
	})
}

func TestWriteBoxReleases_ZeroAdvisoriesHidden(t *testing.T) {
	var buf bytes.Buffer
	entry := &domainaudit.AuditEntry{
		PURL:    "pkg:npm/test@1.0.0",
		Verdict: domainaudit.VerdictOK,
		Analysis: &analysis.Analysis{
			ReleaseInfo: &analysis.ReleaseInfo{
				StableVersion: &analysis.VersionDetail{
					Version:     "1.0.0",
					PublishedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				},
			},
		},
	}
	ctx := newBoxContext(&buf, entry, 60)
	if err := writeBoxReleases(ctx); err != nil {
		t.Fatalf("writeBoxReleases() error = %v", err)
	}
	output := buf.String()
	if strings.Contains(output, "Advisories") {
		t.Error("Advisories: 0 should not be displayed")
	}
}

func TestWriteBoxHealth_NormalState(t *testing.T) {
	var buf bytes.Buffer
	entry := &domainaudit.AuditEntry{
		PURL:    "pkg:npm/test@1.0.0",
		Verdict: domainaudit.VerdictOK,
		Analysis: &analysis.Analysis{
			RepoURL: "github.com/test/repo",
			RepoState: &analysis.RepoState{
				IsArchived: false,
				IsDisabled: false,
				IsFork:     false,
			},
			Repository: &analysis.Repository{
				StarsCount: 500,
			},
		},
	}
	ctx := newBoxContext(&buf, entry, 60)
	if err := writeBoxHealth(ctx); err != nil {
		t.Fatalf("writeBoxHealth() error = %v", err)
	}
	output := buf.String()
	if strings.Contains(output, "Normal") {
		t.Error("Normal state should not be displayed")
	}
	if strings.Contains(output, "GitHub:") {
		t.Error("GitHub: label should not be displayed for normal repos")
	}
	if !strings.Contains(output, "500 stars") {
		t.Error("missing star count")
	}
}

func TestWriteBoxHealth_NilRepoState(t *testing.T) {
	var buf bytes.Buffer
	entry := &domainaudit.AuditEntry{
		PURL:    "pkg:npm/test@1.0.0",
		Verdict: domainaudit.VerdictOK,
		Analysis: &analysis.Analysis{
			RepoURL:   "github.com/test/repo",
			RepoState: nil,
			Repository: &analysis.Repository{
				StarsCount: 200,
			},
		},
	}
	ctx := newBoxContext(&buf, entry, 60)
	if err := writeBoxHealth(ctx); err != nil {
		t.Fatalf("writeBoxHealth() error = %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "200 stars") {
		t.Error("missing star count with nil RepoState")
	}
	if strings.Contains(output, "Normal") || strings.Contains(output, "GitHub:") {
		t.Error("should not show Normal/GitHub with nil RepoState")
	}
}

// NOTE: Unit tests for BuildDepsDevURL/BuildDepsDevVersionURL live in
// internal/common/links/depsdev_test.go. The CLI tests above verify that
// box output renders deps.dev links correctly (integration-level coverage).
