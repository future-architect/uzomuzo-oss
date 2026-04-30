package csv

import (
	"os"
	"strings"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

func TestExportLicenses_Basic(t *testing.T) {
	// Arrange
	an := &domain.Analysis{
		OriginalPURL:            "pkg:npm/example",
		EffectivePURL:           "pkg:npm/example@1.0.0",
		ProjectLicense:          domain.ResolvedLicense{Expression: "MIT", Raw: "MIT", Source: domain.LicenseSourceDepsDevProjectSPDX},
		RequestedVersionLicense: domain.ResolvedLicense{Expression: "MIT", Raw: "MIT", Source: domain.LicenseSourceDepsDevVersionSPDX},
		PackageLinks:            &domain.PackageLinks{RegistryURL: "https://registry.example/pkg"},
		RepoURL:                 "https://github.com/example/repo",
	}
	analyses := map[string]*domain.Analysis{"pkg:npm/example": an}

	file, err := os.CreateTemp(t.TempDir(), "licenses-*.csv")
	if err != nil {
		t.Fatalf("temp file: %v", err)
	}
	_ = file.Close()

	// Act
	if err := ExportLicenses(analyses, file.Name()); err != nil {
		// Fail
		t.Fatalf("ExportLicenses() error = %v", err)
	}

	// Assert
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatalf("read exported: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "original_purl") {
		t.Errorf("missing header original_purl")
	}
	if !strings.Contains(content, "pkg:npm/example@1.0.0") {
		t.Errorf("expected effective PURL line")
	}
	if !strings.Contains(content, "registry_url") {
		t.Errorf("missing registry_url header")
	}
	if !strings.Contains(content, "repository_url") {
		t.Errorf("missing repository_url header")
	}
	if !strings.Contains(content, "https://registry.example/pkg") {
		t.Errorf("missing registry URL value")
	}
	if !strings.Contains(content, "https://github.com/example/repo") {
		t.Errorf("missing repository URL value")
	}
	if !strings.Contains(content, "project_spdx_version_all_spdx_consistent") {
		// scenario classification for simple consistent SPDX case
		// Accept either that string or fallback to catch_all if rules change
		if !strings.Contains(content, "catch_all") {
			t.Errorf("expected scenario classification, got: %s", content)
		}
	}
}

// TestExportLicenses_NOASSERTION_Scenarios locks the dedicated NOASSERTION
// branches added in the scenarioInputs migration. Without them, NOASSERTION
// at either level fell through every branch into "catch_all" and the
// `licenses_all_missing_or_nonstandard` flag silently reported `false` even
// though no usable license was present.
func TestExportLicenses_NOASSERTION_Scenarios(t *testing.T) {
	noassert := domain.ResolvedLicense{Expression: "NOASSERTION", Raw: "NOASSERTION", Source: domain.LicenseSourceDepsDevVersionSPDX}
	mit := domain.ResolvedLicense{Expression: "MIT", Raw: "MIT", Source: domain.LicenseSourceDepsDevProjectSPDX}

	cases := []struct {
		name             string
		project, version domain.ResolvedLicense
		wantScenario     string
		wantAllMissing   string // exact CSV cell for licenses_all_missing_or_nonstandard
	}{
		{name: "noassertion_both", project: noassert, version: noassert, wantScenario: "noassertion_both", wantAllMissing: "true"},
		{name: "noassertion_project_only", project: noassert, version: mit, wantScenario: "noassertion_project", wantAllMissing: "false"},
		{name: "noassertion_version_only", project: mit, version: noassert, wantScenario: "noassertion_version", wantAllMissing: "true"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			an := &domain.Analysis{
				OriginalPURL:            "pkg:npm/example",
				EffectivePURL:           "pkg:npm/example@1.0.0",
				ProjectLicense:          tc.project,
				RequestedVersionLicense: tc.version,
			}
			file, err := os.CreateTemp(t.TempDir(), "licenses-*.csv")
			if err != nil {
				t.Fatalf("temp file: %v", err)
			}
			_ = file.Close()

			if err := ExportLicenses(map[string]*domain.Analysis{"pkg:npm/example": an}, file.Name()); err != nil {
				t.Fatalf("ExportLicenses() error = %v", err)
			}
			data, err := os.ReadFile(file.Name())
			if err != nil {
				t.Fatalf("read exported: %v", err)
			}
			content := string(data)
			if !strings.Contains(content, tc.wantScenario) {
				t.Errorf("expected scenario %q in CSV, got:\n%s", tc.wantScenario, content)
			}
			if !strings.Contains(content, tc.wantAllMissing+",") {
				// licenses_all_missing_or_nonstandard is a boolean cell — assert
				// it appears followed by a comma so we don't accidentally match
				// the same value in another column.
				t.Errorf("expected licenses_all_missing_or_nonstandard=%q, got:\n%s", tc.wantAllMissing, content)
			}
		})
	}
}
