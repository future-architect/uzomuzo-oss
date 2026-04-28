package eolevaluator

import (
	"context"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

func TestEvaluator_DepsDevFallback_FiresOnAllowedEcosystem(t *testing.T) {
	for _, eco := range []string{"golang", "gem", "pub", "hex", "conan"} {
		t.Run(eco, func(t *testing.T) {
			purlStr := "pkg:" + eco + "/example@1.0.0"
			if eco == "golang" {
				purlStr = "pkg:golang/example.com/foo@1.0.0"
			}
			ev := NewEvaluator(nil)
			ev.SetMaxWorkers(1)
			analysis := &domain.Analysis{
				Package: &domain.Package{PURL: purlStr, Ecosystem: eco},
				ReleaseInfo: &domain.ReleaseInfo{
					StableVersion: &domain.VersionDetail{Version: "1.0.0", IsDeprecated: true},
				},
			}
			out, err := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": analysis})
			if err != nil {
				t.Fatalf("EvaluateBatch failed: %v", err)
			}
			st := out["k"]
			if st.State != domain.EOLEndOfLife {
				t.Fatalf("[%s] expected EOLEndOfLife from deps.dev fallback, got %v", eco, st.State)
			}
			found := false
			for _, evd := range st.Evidences {
				if evd.Source == "deps.dev" && evd.Confidence == 0.95 {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("[%s] expected deps.dev evidence with confidence 0.95, got %#v", eco, st.Evidences)
			}
		})
	}
}

func TestEvaluator_DepsDevFallback_DoesNotFire_NotDeprecated(t *testing.T) {
	ev := NewEvaluator(nil)
	ev.SetMaxWorkers(1)
	analysis := &domain.Analysis{
		Package: &domain.Package{PURL: "pkg:golang/example.com/foo@1.0.0", Ecosystem: "golang"},
		ReleaseInfo: &domain.ReleaseInfo{
			StableVersion: &domain.VersionDetail{Version: "1.0.0", IsDeprecated: false},
		},
	}
	out, _ := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": analysis})
	if out["k"].State == domain.EOLEndOfLife {
		t.Fatalf("expected non-EOL when IsDeprecated=false, got EOL")
	}
}

func TestEvaluator_DepsDevFallback_DoesNotFire_DisallowedEcosystem(t *testing.T) {
	// npm/pypi/maven/nuget/composer/cargo are excluded from the deps.dev allow-list
	// because authoritative ecosystem-specific rules cover them.
	for _, eco := range []string{"npm", "pypi", "maven", "nuget", "composer", "cargo"} {
		t.Run(eco, func(t *testing.T) {
			purlStr := "pkg:" + eco + "/foo@1.0.0"
			if eco == "maven" {
				purlStr = "pkg:maven/group/foo@1.0.0"
			}
			ev := NewEvaluator(nil)
			ev.SetMaxWorkers(1)
			// Disable the ecosystem-specific clients to ensure the disallowed-ecosystem
			// guard (not the chain short-circuit) is what blocks the fallback.
			ev.SetPyPIClient(nil)
			ev.SetCratesClient(nil)
			analysis := &domain.Analysis{
				Package: &domain.Package{PURL: purlStr, Ecosystem: eco},
				ReleaseInfo: &domain.ReleaseInfo{
					StableVersion: &domain.VersionDetail{Version: "1.0.0", IsDeprecated: true},
				},
			}
			out, _ := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": analysis})
			if out["k"].State == domain.EOLEndOfLife {
				t.Fatalf("[%s] expected non-EOL (ecosystem excluded from deps.dev fallback), got EOL", eco)
			}
		})
	}
}

func TestEvaluator_DepsDevFallback_DoesNotFire_NilReleaseInfo(t *testing.T) {
	ev := NewEvaluator(nil)
	ev.SetMaxWorkers(1)
	analysis := &domain.Analysis{
		Package:     &domain.Package{PURL: "pkg:golang/example.com/foo@1.0.0", Ecosystem: "golang"},
		ReleaseInfo: nil, // explicit nil
	}
	out, err := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": analysis})
	if err != nil {
		t.Fatalf("EvaluateBatch failed: %v", err)
	}
	if out["k"].State == domain.EOLEndOfLife {
		t.Fatalf("expected non-EOL when ReleaseInfo nil, got EOL")
	}
}
