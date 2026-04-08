package eolevaluator

import (
	"context"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/npmjs"
)

// fakeNpmClient is a test double for npmDeprecationClient that captures call arguments.
type fakeNpmClient struct {
	info  *npmjs.DeprecationInfo
	found bool
	err   error
	// captured arguments from the last call
	calledNamespace string
	calledName      string
	calledVersion   string
}

func (f *fakeNpmClient) GetDeprecation(_ context.Context, namespace, name, version string) (*npmjs.DeprecationInfo, bool, error) {
	f.calledNamespace = namespace
	f.calledName = name
	f.calledVersion = version
	return f.info, f.found, f.err
}

// TestEvaluator_NpmDeprecation_StableVersionPresent verifies npm deprecation is
// detected when StableVersion is present (existing behavior — should pass).
func TestEvaluator_NpmDeprecation_StableVersionPresent(t *testing.T) {
	npm := &fakeNpmClient{
		info:  &npmjs.DeprecationInfo{Deprecated: true, Message: "vm2 is deprecated"},
		found: true,
	}
	ev := NewEvaluator(nil)
	ev.SetMaxWorkers(1)
	ev.SetNpmClient(npm)

	a := &domain.Analysis{
		Package:       &domain.Package{PURL: "pkg:npm/vm2@3.9.19"},
		EffectivePURL: "pkg:npm/vm2@3.9.19",
		ReleaseInfo: &domain.ReleaseInfo{
			StableVersion: &domain.VersionDetail{Version: "3.9.19"},
		},
	}
	out, err := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": a})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	st := out["k"]
	if st.State != domain.EOLEndOfLife {
		t.Fatalf("expected EOL for deprecated npm pkg with StableVersion, got %v", st.State)
	}
	if npm.calledNamespace != "" {
		t.Errorf("expected empty namespace, got %q", npm.calledNamespace)
	}
	if npm.calledName != "vm2" {
		t.Errorf("expected name %q, got %q", "vm2", npm.calledName)
	}
	if npm.calledVersion != "3.9.19" {
		t.Errorf("expected version %q, got %q", "3.9.19", npm.calledVersion)
	}
}

// TestEvaluator_NpmDeprecation_NoStableVersion_Bug218 reproduces issue #218:
// when ReleaseInfo or StableVersion is nil (e.g., deps.dev data lag), the npm
// deprecation check is skipped entirely and the package is classified as NotEOL.
func TestEvaluator_NpmDeprecation_NoStableVersion_Bug218(t *testing.T) {
	npm := &fakeNpmClient{
		info:  &npmjs.DeprecationInfo{Deprecated: true, Message: "vm2 is deprecated"},
		found: true,
	}
	ev := NewEvaluator(nil)
	ev.SetMaxWorkers(1)
	ev.SetNpmClient(npm)

	// Simulate vm2 with PURL containing version but no ReleaseInfo.StableVersion.
	// This is the scenario from issue #218 where deps.dev data lags.
	a := &domain.Analysis{
		Package:       &domain.Package{PURL: "pkg:npm/vm2@3.9.19"},
		EffectivePURL: "pkg:npm/vm2@3.9.19",
		// No ReleaseInfo — simulates missing deps.dev version data
	}
	out, err := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": a})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	st := out["k"]
	if st.State != domain.EOLEndOfLife {
		t.Fatalf("issue #218: expected EOL for deprecated npm pkg even without StableVersion, got %v", st.State)
	}
	if npm.calledNamespace != "" {
		t.Errorf("expected empty namespace, got %q", npm.calledNamespace)
	}
	if npm.calledName != "vm2" {
		t.Errorf("expected name %q, got %q", "vm2", npm.calledName)
	}
	if npm.calledVersion != "3.9.19" {
		t.Errorf("expected version %q, got %q", "3.9.19", npm.calledVersion)
	}
}

// TestEvaluator_NpmDeprecation_NoStableVersion_EffectivePURLOnly_Bug218 reproduces
// a variant of issue #218 where ReleaseInfo exists but StableVersion is nil.
func TestEvaluator_NpmDeprecation_NoStableVersion_EffectivePURLOnly_Bug218(t *testing.T) {
	npm := &fakeNpmClient{
		info:  &npmjs.DeprecationInfo{Deprecated: true, Message: "vm2 is deprecated"},
		found: true,
	}
	ev := NewEvaluator(nil)
	ev.SetMaxWorkers(1)
	ev.SetNpmClient(npm)

	a := &domain.Analysis{
		Package:       &domain.Package{PURL: "pkg:npm/vm2@3.9.19"},
		EffectivePURL: "pkg:npm/vm2@3.9.19",
		ReleaseInfo:   &domain.ReleaseInfo{}, // exists but StableVersion is nil
	}
	out, err := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": a})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	st := out["k"]
	if st.State != domain.EOLEndOfLife {
		t.Fatalf("issue #218: expected EOL for deprecated npm pkg (empty ReleaseInfo), got %v", st.State)
	}
	if npm.calledNamespace != "" {
		t.Errorf("expected empty namespace, got %q", npm.calledNamespace)
	}
	if npm.calledName != "vm2" {
		t.Errorf("expected name %q, got %q", "vm2", npm.calledName)
	}
	if npm.calledVersion != "3.9.19" {
		t.Errorf("expected version %q, got %q", "3.9.19", npm.calledVersion)
	}
}

// TestEvaluator_NpmDeprecation_ScopedPackage_NoStableVersion ensures scoped npm
// packages are also caught by the fallback check.
func TestEvaluator_NpmDeprecation_ScopedPackage_NoStableVersion(t *testing.T) {
	npm := &fakeNpmClient{
		info:  &npmjs.DeprecationInfo{Deprecated: true, Message: "use @new/pkg instead"},
		found: true,
	}
	ev := NewEvaluator(nil)
	ev.SetMaxWorkers(1)
	ev.SetNpmClient(npm)

	a := &domain.Analysis{
		Package:       &domain.Package{PURL: "pkg:npm/%40old/pkg@1.0.0"},
		EffectivePURL: "pkg:npm/%40old/pkg@1.0.0",
	}
	out, err := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": a})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	st := out["k"]
	if st.State != domain.EOLEndOfLife {
		t.Fatalf("expected EOL for deprecated scoped npm pkg without StableVersion, got %v", st.State)
	}
	if npm.calledNamespace != "@old" {
		t.Errorf("expected namespace %q, got %q", "@old", npm.calledNamespace)
	}
	if npm.calledName != "pkg" {
		t.Errorf("expected name %q, got %q", "pkg", npm.calledName)
	}
	if npm.calledVersion != "1.0.0" {
		t.Errorf("expected version %q, got %q", "1.0.0", npm.calledVersion)
	}
}

// TestEvaluator_NpmDeprecation_TypedNilClient verifies that passing a typed-nil
// *npmjs.Client via SetNpmClient does not panic and leaves State as NotEOL.
func TestEvaluator_NpmDeprecation_TypedNilClient(t *testing.T) {
	var nilClient *npmjs.Client // typed-nil
	ev := NewEvaluator(nil)
	ev.SetMaxWorkers(1)
	ev.SetNpmClient(nilClient) // typed-nil interface

	a := &domain.Analysis{
		Package:       &domain.Package{PURL: "pkg:npm/vm2@3.9.19"},
		EffectivePURL: "pkg:npm/vm2@3.9.19",
		ReleaseInfo: &domain.ReleaseInfo{
			StableVersion: &domain.VersionDetail{Version: "3.9.19"},
		},
	}
	out, err := ev.EvaluateBatch(context.Background(), map[string]*domain.Analysis{"k": a})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	st := out["k"]
	if st.State != domain.EOLNotEOL {
		t.Fatalf("expected NotEOL with typed-nil npm client, got %v", st.State)
	}
}
