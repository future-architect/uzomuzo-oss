package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/clearlydefined"
)

const cdMavenSingleSPDXBody = `{
  "licensed": {
    "declared": "Apache-2.0",
    "score": { "total": 100, "declared": 60 }
  }
}`

const cdMavenExpressionBody = `{
  "licensed": {
    "declared": "CDDL-1.1 OR GPL-2.0-only",
    "score": { "total": 100, "declared": 60 }
  }
}`

func TestEnrichLicenseFromClearlyDefined_FillsNonStandardFromSPDX(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(cdMavenSingleSPDXBody))
	}))
	t.Cleanup(srv.Close)

	cd := clearlydefined.NewClient()
	cd.SetBaseURL(srv.URL)
	cd.SetHTTPClient(srv.Client())
	svc := &IntegrationService{cdClient: cd}

	a := &domain.Analysis{
		Package: &domain.Package{
			PURL:      "pkg:maven/org.apache.commons/commons-lang3@3.12.0",
			Ecosystem: "maven",
		},
		ProjectLicense: domain.ResolvedLicense{
			Source: domain.LicenseSourceDepsDevProjectNonStandard,
			Raw:    "Apache 2 (alias miss)",
		},
	}
	svc.enrichLicenseFromClearlyDefined(context.Background(), map[string]*domain.Analysis{"a": a})

	if a.ProjectLicense.Source != domain.LicenseSourceClearlyDefinedSPDX {
		t.Errorf("ProjectLicense.Source = %q, want %q", a.ProjectLicense.Source, domain.LicenseSourceClearlyDefinedSPDX)
	}
	if a.ProjectLicense.Identifier != "Apache-2.0" {
		t.Errorf("ProjectLicense.Identifier = %q, want Apache-2.0", a.ProjectLicense.Identifier)
	}
	if !a.ProjectLicense.IsSPDX {
		t.Errorf("ProjectLicense.IsSPDX = false, want true")
	}
}

func TestEnrichLicenseFromClearlyDefined_PreservesCanonicalSPDX(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		// CD says Apache-2.0; existing analysis already has canonical MIT.
		_, _ = w.Write([]byte(cdMavenSingleSPDXBody))
	}))
	t.Cleanup(srv.Close)

	cd := clearlydefined.NewClient()
	cd.SetBaseURL(srv.URL)
	cd.SetHTTPClient(srv.Client())
	svc := &IntegrationService{cdClient: cd}

	canonical := domain.ResolvedLicense{
		Identifier: "MIT",
		Source:     domain.LicenseSourceDepsDevProjectSPDX,
		Raw:        "MIT",
		IsSPDX:     true,
	}
	a := &domain.Analysis{
		Package: &domain.Package{
			PURL:      "pkg:maven/com.example/foo@1.0.0",
			Ecosystem: "maven",
		},
		ProjectLicense: canonical,
		// RequestedVersionLicenses also fully SPDX so needsManifestLicense
		// will decide CD doesn't even need to fetch.
		RequestedVersionLicenses: []domain.ResolvedLicense{canonical},
	}
	svc.enrichLicenseFromClearlyDefined(context.Background(), map[string]*domain.Analysis{"a": a})

	if calls != 0 {
		t.Errorf("CD was called %d times for an already-canonical analysis; expected 0", calls)
	}
	if a.ProjectLicense != canonical {
		t.Errorf("ProjectLicense mutated to %+v, want canonical preserved", a.ProjectLicense)
	}
}

func TestEnrichLicenseFromClearlyDefined_SPDXExpressionEmitsMultipleLeaves(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(cdMavenExpressionBody))
	}))
	t.Cleanup(srv.Close)

	cd := clearlydefined.NewClient()
	cd.SetBaseURL(srv.URL)
	cd.SetHTTPClient(srv.Client())
	svc := &IntegrationService{cdClient: cd}

	a := &domain.Analysis{
		Package: &domain.Package{
			PURL:      "pkg:maven/javax.servlet/javax.servlet-api@4.0.1",
			Ecosystem: "maven",
		},
		// Empty so CD's first SPDX leaf can promote.
	}
	svc.enrichLicenseFromClearlyDefined(context.Background(), map[string]*domain.Analysis{"a": a})

	if a.ProjectLicense.Identifier != "CDDL-1.1" {
		t.Errorf("ProjectLicense.Identifier = %q, want CDDL-1.1 (first SPDX leaf)", a.ProjectLicense.Identifier)
	}
	if len(a.RequestedVersionLicenses) != 2 {
		t.Fatalf("RequestedVersionLicenses len = %d, want 2", len(a.RequestedVersionLicenses))
	}
	wantIDs := []string{"CDDL-1.1", "GPL-2.0-only"}
	for i, want := range wantIDs {
		if a.RequestedVersionLicenses[i].Identifier != want {
			t.Errorf("RequestedVersionLicenses[%d].Identifier = %q, want %q",
				i, a.RequestedVersionLicenses[i].Identifier, want)
		}
	}
}

func TestEnrichLicenseFromClearlyDefined_NilClientNoop(t *testing.T) {
	svc := &IntegrationService{cdClient: nil}
	a := &domain.Analysis{
		Package:        &domain.Package{PURL: "pkg:maven/g/a@1", Ecosystem: "maven"},
		ProjectLicense: domain.ResolvedLicense{Source: domain.LicenseSourceDepsDevProjectNonStandard, Raw: "x"},
	}
	// Should not panic, should leave analysis alone.
	svc.enrichLicenseFromClearlyDefined(context.Background(), map[string]*domain.Analysis{"a": a})
	if a.ProjectLicense.Source != domain.LicenseSourceDepsDevProjectNonStandard {
		t.Errorf("ProjectLicense mutated when cdClient is nil: %+v", a.ProjectLicense)
	}
}

func TestEnrichLicenseFromClearlyDefined_DedupsSameCoordinate(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(cdMavenSingleSPDXBody))
	}))
	t.Cleanup(srv.Close)

	cd := clearlydefined.NewClient()
	cd.SetBaseURL(srv.URL)
	cd.SetHTTPClient(srv.Client())
	svc := &IntegrationService{cdClient: cd}

	mk := func() *domain.Analysis {
		return &domain.Analysis{
			Package: &domain.Package{
				PURL:      "pkg:maven/org.apache.commons/commons-lang3@3.12.0",
				Ecosystem: "maven",
			},
			ProjectLicense: domain.ResolvedLicense{Source: domain.LicenseSourceDepsDevProjectNonStandard, Raw: "x"},
		}
	}
	svc.enrichLicenseFromClearlyDefined(context.Background(), map[string]*domain.Analysis{
		"a": mk(),
		"b": mk(),
		"c": mk(),
	})

	if calls != 1 {
		t.Errorf("CD called %d times; expected 1 (3 analyses share the same coordinate)", calls)
	}
}
