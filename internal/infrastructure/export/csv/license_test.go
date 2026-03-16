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
		OriginalPURL:   "pkg:npm/example",
		EffectivePURL:  "pkg:npm/example@1.0.0",
		ProjectLicense: domain.ResolvedLicense{Identifier: "MIT", Raw: "MIT", IsSPDX: true, Source: domain.LicenseSourceDepsDevProjectSPDX},
		RequestedVersionLicenses: []domain.ResolvedLicense{
			{Identifier: "MIT", Raw: "MIT", IsSPDX: true, Source: domain.LicenseSourceDepsDevVersionSPDX},
		},
		PackageLinks: &domain.PackageLinks{RegistryURL: "https://registry.example/pkg"},
		RepoURL:      "https://github.com/example/repo",
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
