package eolevaluator

import (
	"context"
	"strings"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// fallbackCases enumerates ecosystems where deps.dev hosts data AND we have no
// authoritative ecosystem-specific rule. Today this is golang (deps.dev "go")
// and gem (deps.dev "rubygems").
var fallbackCases = []struct {
	purlEcosystem      string
	purlStr            string
	version            string
	wantRefHasSegments []string // expected substrings in the Reference URL
}{
	{
		purlEcosystem:      "golang",
		purlStr:            "pkg:golang/example.com/foo@1.0.0",
		version:            "1.0.0",
		wantRefHasSegments: []string{"https://deps.dev/go/", "1.0.0"}, // ecosystem normalized to "go"
	},
	{
		purlEcosystem:      "gem",
		purlStr:            "pkg:gem/rails@7.0.0",
		version:            "7.0.0",
		wantRefHasSegments: []string{"https://deps.dev/rubygems/rails/7.0.0"}, // gem → rubygems
	},
}

func TestEvaluator_DepsDevFallback_FiresOnSupportedEcosystem(t *testing.T) {
	for _, tc := range fallbackCases {
		t.Run(tc.purlEcosystem, func(t *testing.T) {
			ev := NewEvaluator(nil)
			ev.SetMaxWorkers(1)
			analysis := &domain.Analysis{
				Package: &domain.Package{PURL: tc.purlStr, Ecosystem: tc.purlEcosystem},
				ReleaseInfo: &domain.ReleaseInfo{
					StableVersion: &domain.VersionDetail{Version: tc.version, IsDeprecated: true},
				},
			}
			out, err := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": analysis})
			if err != nil {
				t.Fatalf("EvaluateBatch failed: %v", err)
			}
			st := out["k"]
			if st.State != domain.EOLEndOfLife {
				t.Fatalf("[%s] expected EOLEndOfLife from deps.dev fallback, got %v", tc.purlEcosystem, st.State)
			}
			var ev0 *domain.EOLEvidence
			for i := range st.Evidences {
				if st.Evidences[i].Source == "deps.dev" {
					ev0 = &st.Evidences[i]
					break
				}
			}
			if ev0 == nil {
				t.Fatalf("[%s] expected deps.dev evidence, got %#v", tc.purlEcosystem, st.Evidences)
			}
			if ev0.Confidence != 0.95 {
				t.Errorf("[%s] expected Confidence 0.95, got %v", tc.purlEcosystem, ev0.Confidence)
			}
			for _, seg := range tc.wantRefHasSegments {
				if !strings.Contains(ev0.Reference, seg) {
					t.Errorf("[%s] expected Reference to contain %q, got %q", tc.purlEcosystem, seg, ev0.Reference)
				}
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
	out, err := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": analysis})
	if err != nil {
		t.Fatalf("EvaluateBatch failed: %v", err)
	}
	if out["k"].State == domain.EOLEndOfLife {
		t.Fatalf("expected non-EOL when IsDeprecated=false, got EOL")
	}
}

func TestEvaluator_DepsDevFallback_DoesNotFire_AuthoritativeEcosystem(t *testing.T) {
	// Ecosystems with ecosystem-specific authoritative rules must not be touched
	// by the fallback even when deps.dev reports IsDeprecated=true.
	for _, eco := range []string{"npm", "pypi", "maven", "nuget", "composer", "packagist", "cargo"} {
		t.Run(eco, func(t *testing.T) {
			purlStr := "pkg:" + eco + "/foo@1.0.0"
			if eco == "maven" {
				purlStr = "pkg:maven/group/foo@1.0.0"
			}
			ev := NewEvaluator(nil)
			ev.SetMaxWorkers(1)
			// Disable the ecosystem-specific clients so the authoritative rule
			// returns false on fetch error and the chain progresses to the fallback.
			// The fallback's allow-list (not the chain short-circuit) is what
			// blocks the rule for these ecosystems.
			ev.SetPyPIClient(nil)
			ev.SetCratesClient(nil)
			analysis := &domain.Analysis{
				Package: &domain.Package{PURL: purlStr, Ecosystem: eco},
				ReleaseInfo: &domain.ReleaseInfo{
					StableVersion: &domain.VersionDetail{Version: "1.0.0", IsDeprecated: true},
				},
			}
			out, err := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": analysis})
			if err != nil {
				t.Fatalf("[%s] EvaluateBatch failed: %v", eco, err)
			}
			if out["k"].State == domain.EOLEndOfLife {
				t.Fatalf("[%s] expected non-EOL (ecosystem covered by authoritative rule), got EOL", eco)
			}
		})
	}
}

func TestEvaluator_DepsDevFallback_DoesNotFire_DepsDevUnsupportedEcosystem(t *testing.T) {
	// deps.dev does not host pub / hex / conan; the fallback must skip these
	// rather than emit an unreachable evidence URL.
	for _, eco := range []string{"pub", "hex", "conan"} {
		t.Run(eco, func(t *testing.T) {
			ev := NewEvaluator(nil)
			ev.SetMaxWorkers(1)
			analysis := &domain.Analysis{
				Package: &domain.Package{PURL: "pkg:" + eco + "/foo@1.0.0", Ecosystem: eco},
				ReleaseInfo: &domain.ReleaseInfo{
					StableVersion: &domain.VersionDetail{Version: "1.0.0", IsDeprecated: true},
				},
			}
			out, err := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": analysis})
			if err != nil {
				t.Fatalf("[%s] EvaluateBatch failed: %v", eco, err)
			}
			if out["k"].State == domain.EOLEndOfLife {
				t.Fatalf("[%s] expected non-EOL (deps.dev does not host this ecosystem), got EOL", eco)
			}
		})
	}
}

func TestEvaluator_DepsDevFallback_DoesNotFire_NilReleaseInfo(t *testing.T) {
	ev := NewEvaluator(nil)
	ev.SetMaxWorkers(1)
	analysis := &domain.Analysis{
		Package:     &domain.Package{PURL: "pkg:golang/example.com/foo@1.0.0", Ecosystem: "golang"},
		ReleaseInfo: nil,
	}
	out, err := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": analysis})
	if err != nil {
		t.Fatalf("EvaluateBatch failed: %v", err)
	}
	if out["k"].State == domain.EOLEndOfLife {
		t.Fatalf("expected non-EOL when ReleaseInfo nil, got EOL")
	}
}

func TestEvaluator_DepsDevFallback_DoesNotFire_EmptyVersion(t *testing.T) {
	ev := NewEvaluator(nil)
	ev.SetMaxWorkers(1)
	analysis := &domain.Analysis{
		Package: &domain.Package{PURL: "pkg:golang/example.com/foo@1.0.0", Ecosystem: "golang"},
		ReleaseInfo: &domain.ReleaseInfo{
			StableVersion: &domain.VersionDetail{Version: "", IsDeprecated: true},
		},
	}
	out, err := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": analysis})
	if err != nil {
		t.Fatalf("EvaluateBatch failed: %v", err)
	}
	if out["k"].State == domain.EOLEndOfLife {
		t.Fatalf("expected non-EOL when StableVersion.Version empty, got EOL")
	}
}
