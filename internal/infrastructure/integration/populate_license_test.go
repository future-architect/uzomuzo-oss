package integration

import (
	"context"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
)

// TestPopulateLicenses_DerivedVersionPromotion verifies the rule "when
// ProjectLicense is non-standard and the requested version yields a
// single-leaf SPDX expression, promote that expression to ProjectLicense
// with Source=DerivedFromVersion."
func TestPopulateLicenses_DerivedVersionPromotion(t *testing.T) {
	svc := &IntegrationService{}
	analysis := &domain.Analysis{OriginalPURL: "pkg:npm/example@1.0.0", EffectivePURL: "pkg:npm/example@1.0.0"}
	analysis.EnsureCanonical()
	analysis.ReleaseInfo = &domain.ReleaseInfo{RequestedVersion: &domain.VersionDetail{Version: "1.0.0"}}
	analysis.ProjectLicense = domain.ResolvedLicense{
		Expression: "",
		Raw:        "non-standard",
		Source:     domain.LicenseSourceDepsDevProjectNonStandard,
	}

	batch := &depsdev.BatchResult{
		Package: &depsdev.Package{
			Versions: []depsdev.Version{{
				VersionKey: depsdev.VersionKey{Version: "1.0.0"},
				Licenses:   []string{"MIT"},
			}},
		},
		Project: &depsdev.Project{License: "non-standard"},
	}

	svc.populateLicenses(context.Background(), analysis, batch)

	if analysis.ProjectLicense.Expression != "MIT" {
		t.Fatalf("expected promoted project license MIT, got Expression=%q", analysis.ProjectLicense.Expression)
	}
	if analysis.ProjectLicense.Source != domain.LicenseSourceDerivedFromVersion {
		t.Fatalf("expected source DerivedFromVersion, got %q", analysis.ProjectLicense.Source)
	}
	if analysis.RequestedVersionLicense.Expression != "MIT" {
		t.Fatalf("expected requested version license MIT, got Expression=%q", analysis.RequestedVersionLicense.Expression)
	}
}

// TestPopulateLicenses_PromotionSkippedOnCompound pins the ADR-0018 rule
// that compound version expressions ("MIT OR Apache-2.0") are NOT promoted
// to project-level — a project's own LICENSE file may pick one leaf in ways
// the upstream metadata does not capture, so a compound at project-level
// would assert a dual-license posture the project may not actually hold.
func TestPopulateLicenses_PromotionSkippedOnCompound(t *testing.T) {
	svc := &IntegrationService{}
	analysis := &domain.Analysis{OriginalPURL: "pkg:npm/example@1.0.0", EffectivePURL: "pkg:npm/example@1.0.0"}
	analysis.EnsureCanonical()
	analysis.ReleaseInfo = &domain.ReleaseInfo{RequestedVersion: &domain.VersionDetail{Version: "1.0.0"}}
	// ProjectLicense is empty so promotion eligibility is otherwise satisfied.

	batch := &depsdev.BatchResult{
		Package: &depsdev.Package{
			Versions: []depsdev.Version{{
				VersionKey: depsdev.VersionKey{Version: "1.0.0"},
				Licenses:   []string{"MIT", "Apache-2.0"}, // compound at version level
			}},
		},
	}

	svc.populateLicenses(context.Background(), analysis, batch)

	if analysis.RequestedVersionLicense.Expression != "MIT OR Apache-2.0" {
		t.Fatalf("expected version expression OR-joined to MIT OR Apache-2.0, got %q", analysis.RequestedVersionLicense.Expression)
	}
	if !analysis.ProjectLicense.IsZero() {
		t.Fatalf("compound version expression must NOT promote to project-level, got %+v", analysis.ProjectLicense)
	}
}

// TestPopulateLicenses_WithExceptionPromotes verifies that single-leaf
// expressions carrying a WITH exception (e.g. GPL-2.0-only WITH
// Classpath-exception-2.0) are still treated as single-leaf and promoted.
// The leaf-vs-compound distinction tracks AST shape, not the presence of an
// exception clause.
func TestPopulateLicenses_WithExceptionPromotes(t *testing.T) {
	svc := &IntegrationService{}
	analysis := &domain.Analysis{OriginalPURL: "pkg:maven/com.example/foo@1.0.0", EffectivePURL: "pkg:maven/com.example/foo@1.0.0"}
	analysis.EnsureCanonical()
	analysis.ReleaseInfo = &domain.ReleaseInfo{RequestedVersion: &domain.VersionDetail{Version: "1.0.0"}}

	batch := &depsdev.BatchResult{
		Package: &depsdev.Package{
			Versions: []depsdev.Version{{
				VersionKey: depsdev.VersionKey{Version: "1.0.0"},
				Licenses:   []string{"GPL-2.0-only WITH Classpath-exception-2.0"},
			}},
		},
	}

	svc.populateLicenses(context.Background(), analysis, batch)

	want := "GPL-2.0-only WITH Classpath-exception-2.0"
	if analysis.RequestedVersionLicense.Expression != want {
		t.Fatalf("version: want %q got %q", want, analysis.RequestedVersionLicense.Expression)
	}
	if analysis.ProjectLicense.Expression != want {
		t.Fatalf("project (promoted): want %q got %q", want, analysis.ProjectLicense.Expression)
	}
	if analysis.ProjectLicense.Source != domain.LicenseSourceDerivedFromVersion {
		t.Fatalf("project source: want DerivedFromVersion, got %q", analysis.ProjectLicense.Source)
	}
}

// TestPopulateLicenses_DepsDevAlreadyCompoundString covers the latent bug
// fixed by the rewrite: deps.dev's Version.LicenseDetails[].Spdx may itself
// be a compound expression like "MIT OR Apache-2.0". The previous leaf-only
// normalizer corrupted this into "MIT-OR-Apache-2.0" via heuristic fallback.
// The rewrite must preserve compound shape.
func TestPopulateLicenses_DepsDevAlreadyCompoundString(t *testing.T) {
	svc := &IntegrationService{}
	analysis := &domain.Analysis{OriginalPURL: "pkg:maven/com.example/bar@2.0.0", EffectivePURL: "pkg:maven/com.example/bar@2.0.0"}
	analysis.EnsureCanonical()
	analysis.ReleaseInfo = &domain.ReleaseInfo{RequestedVersion: &domain.VersionDetail{Version: "2.0.0"}}

	batch := &depsdev.BatchResult{
		Package: &depsdev.Package{
			Versions: []depsdev.Version{{
				VersionKey:     depsdev.VersionKey{Version: "2.0.0"},
				LicenseDetails: []depsdev.LicenseDetail{{Spdx: "MIT OR Apache-2.0"}},
			}},
		},
	}

	svc.populateLicenses(context.Background(), analysis, batch)

	if analysis.RequestedVersionLicense.Expression != "MIT OR Apache-2.0" {
		t.Fatalf("compound LicenseDetails.Spdx must round-trip; got %q", analysis.RequestedVersionLicense.Expression)
	}
	if analysis.RequestedVersionLicense.Source != domain.LicenseSourceDepsDevVersionSPDX {
		t.Fatalf("Source: want DepsDevVersionSPDX, got %q", analysis.RequestedVersionLicense.Source)
	}
}

// TestPopulateLicenses_NOASSERTION_NotPromoted: NOASSERTION is recognized as
// a sentinel but is not "usable SPDX" — it must not be promoted to project.
func TestPopulateLicenses_NOASSERTION_NotPromoted(t *testing.T) {
	svc := &IntegrationService{}
	analysis := &domain.Analysis{OriginalPURL: "pkg:npm/example@3.0.0", EffectivePURL: "pkg:npm/example@3.0.0"}
	analysis.EnsureCanonical()
	analysis.ReleaseInfo = &domain.ReleaseInfo{RequestedVersion: &domain.VersionDetail{Version: "3.0.0"}}

	batch := &depsdev.BatchResult{
		Package: &depsdev.Package{
			Versions: []depsdev.Version{{
				VersionKey: depsdev.VersionKey{Version: "3.0.0"},
				Licenses:   []string{"NOASSERTION"},
			}},
		},
	}

	svc.populateLicenses(context.Background(), analysis, batch)

	if analysis.RequestedVersionLicense.Expression != "NOASSERTION" {
		t.Fatalf("version: want NOASSERTION, got %q", analysis.RequestedVersionLicense.Expression)
	}
	if !analysis.ProjectLicense.IsZero() {
		t.Fatalf("NOASSERTION must NOT promote to project, got %+v", analysis.ProjectLicense)
	}
}
