package integration

import (
	"testing"
	"time"

	domain "github.com/future-architect/uzomuzo/internal/domain/analysis"
	"github.com/future-architect/uzomuzo/internal/infrastructure/depsdev"
)

// makeVersion is a helper to construct a deps.dev Version with minimal fields.
func makeVersion(version string, links []depsdev.Link) depsdev.Version {
	return depsdev.Version{
		VersionKey:  depsdev.VersionKey{Version: version},
		PublishedAt: time.Time{},
		Links:       links,
	}
}

// Test that HomepageURL is selected from version links when label contains "home" or "project",
// and that RegistryURL is built correctly for scoped npm packages (with '@' namespace).
func TestPopulateAnalysisFromBatchResult_PackageLinks_HomepageFromLinks_And_RegistryURL_NPM_Scoped(t *testing.T) {
	svc := &IntegrationService{}

	// Encoded scope should be handled by PathUnescape before parsing.
	const p = "pkg:npm/%40types/lodash@4.14.195"

	analysis := &domain.Analysis{OriginalPURL: p, EffectivePURL: p,
		Package: &domain.Package{PURL: p, Ecosystem: "npm", Version: "4.14.195"},
	}
	analysis.EnsureCanonical()

	batch := &depsdev.BatchResult{
		PURL: p,
		ReleaseInfo: depsdev.ReleaseInfo{
			StableVersion: makeVersion("4.14.195", []depsdev.Link{
				{Label: "Homepage", URL: "https://example.com/home"},
				{Label: "Repository", URL: "https://github.com/lodash/lodash"},
			}),
		},
	}

	svc.populateAnalysisFromBatchResult(analysis, batch)

	if analysis.PackageLinks == nil {
		t.Fatalf("PackageLinks should be initialized")
	}
	if got, want := analysis.PackageLinks.HomepageURL, "https://example.com/home"; got != want {
		t.Fatalf("HomepageURL = %q, want %q", got, want)
	}
	// For npm with namespace, finalName should be "@types/lodash".
	if got, want := analysis.PackageLinks.RegistryURL, "https://www.npmjs.com/package/@types/lodash"; got != want {
		t.Fatalf("RegistryURL = %q, want %q", got, want)
	}
}

// Test that when version links lack a homepage-like label, the code falls back to project.Homepage,
// and RegistryURL is built for composer/packagist.
func TestPopulateAnalysisFromBatchResult_PackageLinks_HomepageFallbackToProject_Composer(t *testing.T) {
	svc := &IntegrationService{}

	const p = "pkg:composer/symfony/console@v5.4.0"

	analysis := &domain.Analysis{OriginalPURL: p, EffectivePURL: p,
		Package: &domain.Package{PURL: p, Ecosystem: "composer", Version: "v5.4.0"},
	}
	analysis.EnsureCanonical()

	batch := &depsdev.BatchResult{
		PURL: p,
		ReleaseInfo: depsdev.ReleaseInfo{
			StableVersion: makeVersion("v5.4.0", []depsdev.Link{
				// No "home" or "project" in label -> should not match
				{Label: "Docs", URL: "https://symfony.com/doc"},
			}),
		},
		Project: &depsdev.Project{
			ProjectKey: depsdev.ProjectKey{ID: "github.com/symfony/console"},
			Homepage:   "https://symfony.com/components/Console",
		},
	}

	svc.populateAnalysisFromBatchResult(analysis, batch)

	if analysis.PackageLinks == nil {
		t.Fatalf("PackageLinks should be initialized")
	}
	if got, want := analysis.PackageLinks.HomepageURL, "https://symfony.com/components/Console"; got != want {
		t.Fatalf("HomepageURL (fallback) = %q, want %q", got, want)
	}
	if got, want := analysis.PackageLinks.RegistryURL, "https://packagist.org/packages/symfony/console"; got != want {
		t.Fatalf("RegistryURL = %q, want %q", got, want)
	}
}

// Test that RegistryURL is composed correctly per-ecosystem rules (maven, npm, composer).
func TestPopulateAnalysisFromBatchResult_PackageLinks_RegistryURL_Compositions(t *testing.T) {
	tests := []struct {
		name     string
		ecos     string
		purl     string
		expected string
	}{
		{
			name:     "maven_group_artifact",
			ecos:     "maven",
			purl:     "pkg:maven/org.apache.commons/commons-lang3@3.14.0",
			expected: "https://central.sonatype.com/artifact/org.apache.commons/commons-lang3",
		},
		{
			name:     "npm_scoped",
			ecos:     "npm",
			purl:     "pkg:npm/%40scope/pkg@1.2.3",
			expected: "https://www.npmjs.com/package/@scope/pkg",
		},
		{
			name:     "composer_vendor_package",
			ecos:     "composer",
			purl:     "pkg:composer/laravel/framework@11.0.0",
			expected: "https://packagist.org/packages/laravel/framework",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &IntegrationService{}

			analysis := &domain.Analysis{OriginalPURL: tt.purl, EffectivePURL: tt.purl,
				Package: &domain.Package{PURL: tt.purl, Ecosystem: tt.ecos, Version: "dummy"},
			}
			analysis.EnsureCanonical()

			// At least one version must be non-empty to enter the PackageLinks population block.
			batch := &depsdev.BatchResult{
				PURL: tt.purl,
				ReleaseInfo: depsdev.ReleaseInfo{
					StableVersion: makeVersion("dummy", nil),
				},
			}

			svc.populateAnalysisFromBatchResult(analysis, batch)

			if analysis.PackageLinks == nil {
				t.Fatalf("PackageLinks should be initialized")
			}
			if got := analysis.PackageLinks.RegistryURL; got != tt.expected {
				t.Fatalf("RegistryURL = %q, want %q", got, tt.expected)
			}
		})
	}
}
