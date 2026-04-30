package integration

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/maven"
)

func TestNeedsManifestLicense(t *testing.T) {
	tests := []struct {
		name string
		in   *domain.Analysis
		want bool
	}{
		{name: "nil_analysis", in: nil, want: false},
		{name: "no_package", in: &domain.Analysis{}, want: false},
		{name: "empty_purl", in: &domain.Analysis{Package: &domain.Package{}}, want: false},
		{
			name: "project_zero_no_versions",
			in: &domain.Analysis{
				Package: &domain.Package{PURL: "pkg:maven/g/a@1"},
			},
			want: true,
		},
		{
			name: "project_nonstandard_no_versions",
			in: &domain.Analysis{
				Package:        &domain.Package{PURL: "pkg:maven/g/a@1"},
				ProjectLicense: domain.ResolvedLicense{Source: domain.LicenseSourceDepsDevProjectNonStandard, Raw: "Custom"},
			},
			want: true,
		},
		{
			name: "project_spdx_version_spdx_skip",
			in: &domain.Analysis{
				Package:                 &domain.Package{PURL: "pkg:maven/g/a@1"},
				ProjectLicense:          domain.ResolvedLicense{Expression: "MIT", Raw: "MIT", Source: domain.LicenseSourceDepsDevProjectSPDX},
				RequestedVersionLicense: domain.ResolvedLicense{Expression: "MIT", Raw: "MIT", Source: domain.LicenseSourceDepsDevVersionSPDX},
			},
			want: false,
		},
		{
			name: "project_spdx_but_version_raw_needs_fetch",
			in: &domain.Analysis{
				Package:                 &domain.Package{PURL: "pkg:maven/g/a@1"},
				ProjectLicense:          domain.ResolvedLicense{Expression: "MIT", Raw: "MIT", Source: domain.LicenseSourceDepsDevProjectSPDX},
				RequestedVersionLicense: domain.ResolvedLicense{Source: domain.LicenseSourceDepsDevVersionRaw, Raw: "Proprietary"},
			},
			want: true,
		},
		{
			name: "project_spdx_compound_version_skip",
			in: &domain.Analysis{
				Package:                 &domain.Package{PURL: "pkg:maven/g/a@1"},
				ProjectLicense:          domain.ResolvedLicense{Expression: "MIT", Raw: "MIT", Source: domain.LicenseSourceDepsDevProjectSPDX},
				RequestedVersionLicense: domain.ResolvedLicense{Expression: "MIT OR Apache-2.0", Raw: "MIT OR Apache-2.0", Source: domain.LicenseSourceDepsDevVersionSPDX},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := needsManifestLicense(tt.in); got != tt.want {
				t.Errorf("needsManifestLicense() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplyManifestLicenses(t *testing.T) {
	spdx := func(id, src string) domain.ResolvedLicense {
		return domain.ResolvedLicense{Expression: id, Raw: id, Source: src}
	}
	nonStd := func(raw, src string) domain.ResolvedLicense {
		return domain.ResolvedLicense{Raw: raw, Source: src}
	}

	tests := []struct {
		name           string
		seedProject    domain.ResolvedLicense
		seedVersion    domain.ResolvedLicense
		manifest       []domain.ResolvedLicense
		wantProjectExp string
		wantProjectSrc string
		wantVersionExp string
	}{
		{
			name:           "all_zero_replaced_by_spdx_manifest",
			manifest:       []domain.ResolvedLicense{spdx("Apache-2.0", domain.LicenseSourceMavenPOMSPDX)},
			wantProjectExp: "Apache-2.0",
			wantProjectSrc: domain.LicenseSourceMavenPOMSPDX,
			wantVersionExp: "Apache-2.0",
		},
		{
			name:           "nonstandard_project_replaced_by_spdx",
			seedProject:    nonStd("Custom", domain.LicenseSourceDepsDevProjectNonStandard),
			manifest:       []domain.ResolvedLicense{spdx("MIT", domain.LicenseSourceMavenPOMSPDX)},
			wantProjectExp: "MIT",
			wantProjectSrc: domain.LicenseSourceMavenPOMSPDX,
			wantVersionExp: "MIT",
		},
		{
			name:           "canonical_spdx_project_kept_disagreement_logged_only",
			seedProject:    spdx("MIT", domain.LicenseSourceDepsDevProjectSPDX),
			seedVersion:    spdx("MIT", domain.LicenseSourceDepsDevVersionSPDX),
			manifest:       []domain.ResolvedLicense{spdx("Apache-2.0", domain.LicenseSourceMavenPOMSPDX)},
			wantProjectExp: "MIT",
			wantProjectSrc: domain.LicenseSourceDepsDevProjectSPDX,
			wantVersionExp: "MIT",
		},
		{
			name:           "version_nonspdx_replaced_by_spdx",
			seedProject:    nonStd("garbage", domain.LicenseSourceDepsDevProjectNonStandard),
			seedVersion:    nonStd("garbage-v", domain.LicenseSourceDepsDevVersionRaw),
			manifest:       []domain.ResolvedLicense{spdx("BSD-3-Clause", domain.LicenseSourceMavenPOMSPDX)},
			wantProjectExp: "BSD-3-Clause",
			wantProjectSrc: domain.LicenseSourceMavenPOMSPDX,
			wantVersionExp: "BSD-3-Clause",
		},
		{
			name:           "multi_license_or_joined_into_one_expression",
			manifest:       []domain.ResolvedLicense{spdx("CDDL-1.1", domain.LicenseSourceMavenPOMSPDX), spdx("GPL-2.0-with-classpath-exception", domain.LicenseSourceMavenPOMSPDX)},
			wantProjectExp: "CDDL-1.1",
			wantProjectSrc: domain.LicenseSourceMavenPOMSPDX,
			wantVersionExp: "CDDL-1.1 OR GPL-2.0-with-classpath-exception",
		},
		{
			name:           "manifest_only_nonstandard_writes_when_project_zero",
			manifest:       []domain.ResolvedLicense{nonStd("Acme Internal", domain.LicenseSourceMavenPOMNonStandard)},
			wantProjectExp: "",
			wantProjectSrc: domain.LicenseSourceMavenPOMNonStandard,
			wantVersionExp: "",
		},
		{
			name:           "version_nonspdx_kept_when_manifest_also_nonspdx",
			seedProject:    nonStd("Custom", domain.LicenseSourceDepsDevProjectNonStandard),
			seedVersion:    nonStd("Custom-v", domain.LicenseSourceDepsDevVersionRaw),
			manifest:       []domain.ResolvedLicense{nonStd("Acme Internal", domain.LicenseSourceMavenPOMNonStandard)},
			wantProjectExp: "",
			wantProjectSrc: domain.LicenseSourceDepsDevProjectNonStandard,
			wantVersionExp: "",
		},
		{
			name:           "version_canonical_spdx_kept",
			seedProject:    spdx("MIT", domain.LicenseSourceDepsDevProjectSPDX),
			seedVersion:    spdx("MIT", domain.LicenseSourceDepsDevVersionSPDX),
			manifest:       []domain.ResolvedLicense{spdx("MIT", domain.LicenseSourceMavenPOMSPDX)},
			wantProjectExp: "MIT",
			wantProjectSrc: domain.LicenseSourceDepsDevProjectSPDX,
			wantVersionExp: "MIT",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &domain.Analysis{
				Package:                 &domain.Package{PURL: "pkg:maven/g/a@1"},
				ProjectLicense:          tt.seedProject,
				RequestedVersionLicense: tt.seedVersion,
			}
			applyManifestLicenses(a, tt.manifest)
			if a.ProjectLicense.Expression != tt.wantProjectExp {
				t.Errorf("ProjectLicense.Expression = %q, want %q", a.ProjectLicense.Expression, tt.wantProjectExp)
			}
			if a.ProjectLicense.Source != tt.wantProjectSrc {
				t.Errorf("ProjectLicense.Source = %q, want %q", a.ProjectLicense.Source, tt.wantProjectSrc)
			}
			if a.RequestedVersionLicense.Expression != tt.wantVersionExp {
				t.Errorf("RequestedVersionLicense.Expression = %q, want %q", a.RequestedVersionLicense.Expression, tt.wantVersionExp)
			}
		})
	}
}

// TestEnrichLicenseFromManifest_EndToEnd exercises the full dispatcher with a
// stubbed Maven Central server, asserting that a ProjectLicense currently
// labelled non-standard gets replaced with the manifest-derived SPDX value.
func TestEnrichLicenseFromManifest_EndToEnd(t *testing.T) {
	const pom = `<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>widget</artifactId>
  <version>1.0</version>
  <licenses>
    <license>
      <name>Apache License, Version 2.0</name>
      <url>https://www.apache.org/licenses/LICENSE-2.0.txt</url>
    </license>
  </licenses>
</project>`

	var hits int
	var hitsMu sync.Mutex
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsMu.Lock()
		hits++
		hitsMu.Unlock()
		if !strings.HasSuffix(r.URL.Path, ".pom") {
			http.Error(w, "expected .pom", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(pom))
	}))
	defer ts.Close()

	mv := maven.NewClient()
	mv.SetBaseURL(ts.URL)
	svc := &IntegrationService{mavenClient: mv}

	target := &domain.Analysis{
		Package:        &domain.Package{PURL: "pkg:maven/com.example/widget@1.0", Ecosystem: "maven", Version: "1.0"},
		ProjectLicense: domain.ResolvedLicense{Source: domain.LicenseSourceDepsDevProjectNonStandard, Raw: "Custom Vendor License"},
	}
	skipAlreadySPDX := &domain.Analysis{
		Package:                 &domain.Package{PURL: "pkg:maven/com.example/clean@1.0", Ecosystem: "maven", Version: "1.0"},
		ProjectLicense:          domain.ResolvedLicense{Expression: "MIT", Raw: "MIT", Source: domain.LicenseSourceDepsDevProjectSPDX},
		RequestedVersionLicense: domain.ResolvedLicense{Expression: "MIT", Raw: "MIT", Source: domain.LicenseSourceDepsDevVersionSPDX},
	}
	skipNonMaven := &domain.Analysis{
		Package: &domain.Package{PURL: "pkg:npm/example@1.0", Ecosystem: "npm", Version: "1.0"},
	}

	svc.enrichLicenseFromManifest(context.Background(), map[string]*domain.Analysis{
		"target":  target,
		"skip_ok": skipAlreadySPDX,
		"npm":     skipNonMaven,
	})

	if target.ProjectLicense.Expression != "Apache-2.0" {
		t.Errorf("target ProjectLicense.Expression = %q, want %q", target.ProjectLicense.Expression, "Apache-2.0")
	}
	if target.ProjectLicense.Source != domain.LicenseSourceMavenPOMSPDX {
		t.Errorf("target ProjectLicense.Source = %q, want %q", target.ProjectLicense.Source, domain.LicenseSourceMavenPOMSPDX)
	}
	if target.RequestedVersionLicense.Expression != "Apache-2.0" {
		t.Errorf("target RequestedVersionLicense.Expression = %q, want %q", target.RequestedVersionLicense.Expression, "Apache-2.0")
	}
	if skipAlreadySPDX.ProjectLicense.Source != domain.LicenseSourceDepsDevProjectSPDX {
		t.Errorf("clean analysis was overwritten: source=%q", skipAlreadySPDX.ProjectLicense.Source)
	}

	hitsMu.Lock()
	defer hitsMu.Unlock()
	if hits != 1 {
		t.Fatalf("expected exactly 1 POM fetch, got %d", hits)
	}
}

// TestEnrichLicenseFromManifest_NilClient ensures the enricher is a no-op when
// the Maven client is unwired (FetchService instances that opt out, etc.).
func TestEnrichLicenseFromManifest_NilClient(t *testing.T) {
	svc := &IntegrationService{}
	a := &domain.Analysis{
		Package:        &domain.Package{PURL: "pkg:maven/g/a@1", Ecosystem: "maven", Version: "1"},
		ProjectLicense: domain.ResolvedLicense{Source: domain.LicenseSourceDepsDevProjectNonStandard, Raw: "x"},
	}
	svc.enrichLicenseFromManifest(context.Background(), map[string]*domain.Analysis{"a": a})
	if a.ProjectLicense.Source != domain.LicenseSourceDepsDevProjectNonStandard {
		t.Errorf("expected analysis untouched when mavenClient is nil; got source=%q", a.ProjectLicense.Source)
	}
}

// TestEnrichLicenseFromManifest_UnparseablePURL exercises the parse-failure
// branch in the dispatcher: an analysis with a malformed PURL must be skipped
// without affecting siblings or panicking.
func TestEnrichLicenseFromManifest_UnparseablePURL(t *testing.T) {
	const pom = `<?xml version="1.0"?><project><licenses><license><name>MIT</name></license></licenses></project>`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(pom))
	}))
	defer ts.Close()

	mv := maven.NewClient()
	mv.SetBaseURL(ts.URL)
	svc := &IntegrationService{mavenClient: mv}

	bad := &domain.Analysis{
		Package:        &domain.Package{PURL: "not-a-valid-purl", Ecosystem: "maven", Version: "1"},
		ProjectLicense: domain.ResolvedLicense{Source: domain.LicenseSourceDepsDevProjectNonStandard, Raw: "x"},
	}
	good := &domain.Analysis{
		Package:        &domain.Package{PURL: "pkg:maven/com.example/widget@1.0", Ecosystem: "maven", Version: "1.0"},
		ProjectLicense: domain.ResolvedLicense{Source: domain.LicenseSourceDepsDevProjectNonStandard, Raw: "y"},
	}
	svc.enrichLicenseFromManifest(context.Background(), map[string]*domain.Analysis{"bad": bad, "good": good})

	if bad.ProjectLicense.Source != domain.LicenseSourceDepsDevProjectNonStandard {
		t.Errorf("bad analysis should be untouched; got source=%q", bad.ProjectLicense.Source)
	}
	if good.ProjectLicense.Expression != "MIT" {
		t.Errorf("good analysis should be enriched to MIT; got %+v", good.ProjectLicense)
	}
	if good.RequestedVersionLicense.Expression != "MIT" {
		t.Errorf("good analysis should have RequestedVersionLicense populated to MIT; got %+v", good.RequestedVersionLicense)
	}
}

// TestEnrichLicenseFromManifest_VersionlessPURL verifies that a Maven analysis
// whose PURL has no version is still enriched when ReleaseInfo provides a
// usable version via resolvedVersion().
func TestEnrichLicenseFromManifest_VersionlessPURL(t *testing.T) {
	const pom = `<?xml version="1.0"?><project><licenses><license><name>MIT</name></license></licenses></project>`

	var hits int
	var hitsMu sync.Mutex
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsMu.Lock()
		hits++
		hitsMu.Unlock()
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(pom))
	}))
	defer ts.Close()

	mv := maven.NewClient()
	mv.SetBaseURL(ts.URL)
	svc := &IntegrationService{mavenClient: mv}

	versionless := &domain.Analysis{
		Package:        &domain.Package{PURL: "pkg:maven/com.example/widget", Ecosystem: "maven"},
		ProjectLicense: domain.ResolvedLicense{Source: domain.LicenseSourceDepsDevProjectNonStandard, Raw: "Custom"},
		ReleaseInfo:    &domain.ReleaseInfo{StableVersion: &domain.VersionDetail{Version: "2.0.0"}},
	}
	noVersion := &domain.Analysis{
		Package:        &domain.Package{PURL: "pkg:maven/com.example/noversion", Ecosystem: "maven"},
		ProjectLicense: domain.ResolvedLicense{Source: domain.LicenseSourceDepsDevProjectNonStandard, Raw: "x"},
	}

	svc.enrichLicenseFromManifest(context.Background(), map[string]*domain.Analysis{
		"versionless": versionless,
		"noversion":   noVersion,
	})

	if versionless.ProjectLicense.Expression != "MIT" {
		t.Errorf("versionless analysis should be enriched to MIT; got %+v", versionless.ProjectLicense)
	}
	if noVersion.ProjectLicense.Source != domain.LicenseSourceDepsDevProjectNonStandard {
		t.Errorf("noversion analysis should be untouched; got source=%q", noVersion.ProjectLicense.Source)
	}

	hitsMu.Lock()
	defer hitsMu.Unlock()
	if hits != 1 {
		t.Fatalf("expected exactly 1 POM fetch (versionless only), got %d", hits)
	}
}

// TestApplyManifestLicenses_DisagreementLogged installs a structured slog
// handler and asserts that a manifest disagreeing with an existing canonical
// SPDX produces a "license_disagreement" record carrying both sources.
func TestApplyManifestLicenses_DisagreementLogged(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	a := &domain.Analysis{
		Package:        &domain.Package{PURL: "pkg:maven/com.example/widget@1.0"},
		ProjectLicense: domain.ResolvedLicense{Expression: "MIT", Source: domain.LicenseSourceDepsDevProjectSPDX, Raw: "MIT"},
	}
	manifest := []domain.ResolvedLicense{{
		Expression: "Apache-2.0",
		Source:     domain.LicenseSourceMavenPOMSPDX,
		Raw:        "Apache-2.0",
	}}
	applyManifestLicenses(a, manifest)

	if a.ProjectLicense.Expression != "MIT" {
		t.Fatalf("canonical SPDX must not be overwritten; got %q", a.ProjectLicense.Expression)
	}
	out := buf.String()
	if !strings.Contains(out, `"msg":"license_disagreement"`) {
		t.Fatalf("expected license_disagreement log; got: %s", out)
	}
	if !strings.Contains(out, `"existing":"MIT"`) || !strings.Contains(out, `"manifest":"Apache-2.0"`) {
		t.Fatalf("log missing existing/manifest expressions: %s", out)
	}
	if !strings.Contains(out, `"existing_source":"`+domain.LicenseSourceDepsDevProjectSPDX+`"`) {
		t.Fatalf("log missing existing_source field: %s", out)
	}
	if !strings.Contains(out, `"manifest_source":"`+domain.LicenseSourceMavenPOMSPDX+`"`) {
		t.Fatalf("log missing manifest_source field: %s", out)
	}
	if !strings.Contains(out, `"purl":"pkg:maven/com.example/widget@1.0"`) {
		t.Fatalf("log missing purl evidence: %s", out)
	}
}
