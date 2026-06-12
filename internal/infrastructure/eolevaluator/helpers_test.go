package eolevaluator

import (
	"strings"
	"testing"

	purl "github.com/future-architect/uzomuzo-oss/internal/common/purl"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	eoltext "github.com/future-architect/uzomuzo-oss/internal/infrastructure/eoltext"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/npmjs"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/nuget"
)

// mustParseOrZero returns a zeroed ParsedPURL on parse failure so pinning tests
// can exercise the "wrong ecosystem" and malformed paths without a nil dereference.
// It is only used in tests.
func mustParseOrZero(s string) *purl.ParsedPURL {
	p := purl.NewParser()
	parsed, err := p.Parse(s)
	if err != nil {
		// Return a zeroed (but non-nil) ParsedPURL so the helper functions can
		// safely inspect GetEcosystem() etc and return their zero values.
		return &purl.ParsedPURL{}
	}
	return parsed
}

// TestParseComposerFromPURL_pinning pins the canonical outputs for
// parseComposerFromPURL so future refactors stay behavior-compatible.
//
// Note: packageurl-go normalizes composer namespace and name to lowercase per
// the PURL spec (Packagist paths are canonically lowercase). The pinned expected
// values already use lowercase.
func TestParseComposerFromPURL_pinning(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		input      string
		wantVendor string
		wantName   string
	}{
		{
			name:       "canonical composer PURL",
			input:      "pkg:composer/vendor/name@1.0",
			wantVendor: "vendor",
			wantName:   "name",
		},
		{
			name:       "malformed - missing name segment",
			input:      "pkg:composer/vendor",
			wantVendor: "",
			wantName:   "",
		},
		{
			name:       "malformed - unparseable string",
			input:      "",
			wantVendor: "",
			wantName:   "",
		},
		{
			name:       "wrong ecosystem",
			input:      "pkg:npm/vendor/name@1.0",
			wantVendor: "",
			wantName:   "",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotVendor, gotName := parseComposerFromPURL(mustParseOrZero(tt.input))
			if gotVendor != tt.wantVendor || gotName != tt.wantName {
				t.Errorf("parseComposerFromPURL(%q) = (%q, %q), want (%q, %q)",
					tt.input, gotVendor, gotName, tt.wantVendor, tt.wantName)
			}
		})
	}
}

// TestParseNuGetIDFromPURL_pinning pins the canonical outputs for
// parseNuGetIDFromPURL so future refactors stay behavior-compatible.
func TestParseNuGetIDFromPURL_pinning(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  string
		wantID string
	}{
		{
			name:   "canonical NuGet PURL",
			input:  "pkg:nuget/Newtonsoft.Json@13.0.1",
			wantID: "Newtonsoft.Json",
		},
		{
			name:   "NuGet with namespace (malformed) - reconstructed",
			input:  "pkg:nuget/Namespace/PackageId@1.0",
			wantID: "Namespace/PackageId",
		},
		{
			name:   "malformed - unparseable string",
			input:  "",
			wantID: "",
		},
		{
			name:   "malformed - empty name",
			input:  "pkg:nuget/",
			wantID: "",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotID := parseNuGetIDFromPURL(mustParseOrZero(tt.input))
			if gotID != tt.wantID {
				t.Errorf("parseNuGetIDFromPURL(%q) = %q, want %q", tt.input, gotID, tt.wantID)
			}
		})
	}
}

// TestParseMavenFromPURL_pinning pins the canonical outputs for
// parseMavenFromPURL so future refactors stay behavior-compatible.
func TestParseMavenFromPURL_pinning(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		input        string
		wantGroup    string
		wantArtifact string
		wantVersion  string
	}{
		{
			name:         "canonical Maven PURL",
			input:        "pkg:maven/group/artifact@1.2.3",
			wantGroup:    "group",
			wantArtifact: "artifact",
			wantVersion:  "1.2.3",
		},
		{
			name:         "malformed - no version",
			input:        "pkg:maven/group/artifact",
			wantGroup:    "",
			wantArtifact: "",
			wantVersion:  "",
		},
		{
			name:         "malformed - unparseable string",
			input:        "",
			wantGroup:    "",
			wantArtifact: "",
			wantVersion:  "",
		},
		{
			name:         "wrong ecosystem",
			input:        "pkg:npm/name@1.0",
			wantGroup:    "",
			wantArtifact: "",
			wantVersion:  "",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotGroup, gotArtifact, gotVersion := parseMavenFromPURL(mustParseOrZero(tt.input))
			if gotGroup != tt.wantGroup || gotArtifact != tt.wantArtifact || gotVersion != tt.wantVersion {
				t.Errorf("parseMavenFromPURL(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tt.input,
					gotGroup, gotArtifact, gotVersion,
					tt.wantGroup, tt.wantArtifact, tt.wantVersion)
			}
		})
	}
}

func Test_decideNuGetEOL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		id         string
		info       *nuget.DeprecationInfo
		wantState  domain.EOLState
		wantSucc   string
		wantRefHas string // substring to check in Reference
		wantConf   float64
	}{
		{
			name:       "CriticalBugs -> EOL",
			id:         "Test.Package",
			info:       &nuget.DeprecationInfo{Reasons: []string{"CriticalBugs"}},
			wantState:  domain.EOLEndOfLife,
			wantRefHas: "https://api.nuget.org/v3/registration5",
			wantConf:   1.0,
		},
		{
			name:       "Legacy with successor -> EOL",
			id:         "Old.Package",
			info:       &nuget.DeprecationInfo{Reasons: []string{"Legacy"}, AlternatePackageID: "New.Package"},
			wantState:  domain.EOLEndOfLife,
			wantSucc:   "New.Package",
			wantRefHas: "https://api.nuget.org/v3/registration5",
			wantConf:   0.9,
		},
		{
			name:       "Legacy without successor -> warn",
			id:         "Legacy.Package",
			info:       &nuget.DeprecationInfo{Reasons: []string{"Legacy"}},
			wantState:  domain.EOLNotEOL,
			wantRefHas: "https://api.nuget.org/v3/registration5",
			wantConf:   0.7,
		},
		{
			name:       "Other strong wording -> warn (0.8)",
			id:         "Other.Package",
			info:       &nuget.DeprecationInfo{Reasons: []string{"Other"}, Message: "This package is no longer maintained"},
			wantState:  domain.EOLNotEOL,
			wantRefHas: "https://api.nuget.org/v3/registration5",
			wantConf:   0.8,
		},
		{
			name:       "Other plain -> warn (0.5)",
			id:         "Other.Package",
			info:       &nuget.DeprecationInfo{Reasons: []string{"Other"}, Message: "Deprecated for other reasons"},
			wantState:  domain.EOLNotEOL,
			wantRefHas: "https://api.nuget.org/v3/registration5",
			wantConf:   0.5,
		},
		{
			name:       "Other with successor (HTML fallback) -> EOL",
			id:         "Microsoft.Azure.EventHubs",
			info:       &nuget.DeprecationInfo{Reasons: []string{"Other"}, AlternatePackageID: "Azure.Messaging.EventHubs"},
			wantState:  domain.EOLEndOfLife,
			wantSucc:   "Azure.Messaging.EventHubs",
			wantRefHas: "https://api.nuget.org/v3/registration5",
			wantConf:   0.9,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			state, succ, evs := decideNuGetEOL(tt.id, tt.info)

			if state != tt.wantState {
				t.Fatalf("state = %v, want %v", state, tt.wantState)
			}
			if succ != tt.wantSucc {
				t.Fatalf("successor = %q, want %q", succ, tt.wantSucc)
			}
			if len(evs) == 0 {
				t.Fatalf("evidences is empty")
			}
			ev := evs[0]
			if tt.wantRefHas != "" && !strings.Contains(ev.Reference, tt.wantRefHas) {
				t.Fatalf("reference = %q, want to contain %q", ev.Reference, tt.wantRefHas)
			}
			if ev.Confidence != tt.wantConf {
				t.Fatalf("confidence = %v, want %v", ev.Confidence, tt.wantConf)
			}
		})
	}
}

func TestDecideNpmEOL(t *testing.T) {
	info := &npmjs.DeprecationInfo{
		Deprecated: true,
		Message:    "Use newpkg instead",
		Successor:  "newpkg",
	}
	state, successor, evidences := decideNpmEOL("oldpkg", "1.2.3", info)
	if state != "EOL" {
		t.Errorf("expected EOL state")
	}
	if successor != "newpkg" {
		t.Errorf("expected successor 'newpkg', got '%s'", successor)
	}
	if len(evidences) == 0 || evidences[0].Source != "npmjs" {
		t.Errorf("expected npmjs evidence")
	}
	if ref := evidences[0].Reference; !strings.Contains(ref, "https://registry.npmjs.org/") {
		t.Errorf("expected registry reference, got %s", ref)
	}
	if sum := evidences[0].Summary; !strings.Contains(sum, "Deprecated in npm registry") {
		t.Errorf("expected deprecation summary, got %s", sum)
	}

	info2 := &npmjs.DeprecationInfo{Unpublished: true}
	state2, _, evidences2 := decideNpmEOL("oldpkg", "2.0.0", info2)
	if state2 != "EOL" {
		t.Errorf("expected EOL state for unpublished")
	}
	if len(evidences2) == 0 || evidences2[0].Source != "npmjs" {
		t.Errorf("expected npmjs evidence for unpublished")
	}
	if ref := evidences2[0].Reference; !strings.Contains(ref, "https://registry.npmjs.org/") {
		t.Errorf("expected registry reference for unpublished, got %s", ref)
	}
	if sum := evidences2[0].Summary; !strings.Contains(sum, "Unpublished in npm registry") {
		t.Errorf("expected unpublished summary, got %s", sum)
	}

	info3 := &npmjs.DeprecationInfo{}
	state3, _, evidences3 := decideNpmEOL("oldpkg", "3.0.0", info3)
	if state3 != "NotEOL" {
		t.Errorf("expected NotEOL state for not deprecated/unpublished")
	}
	if len(evidences3) != 0 {
		t.Errorf("expected no evidences for not deprecated/unpublished")
	}
}

// Additional npm successor extraction scenarios
func TestDecideNpmEOL_MessageOnlySuccessor(t *testing.T) {
	info := &npmjs.DeprecationInfo{Deprecated: true, Message: "Package deprecated, use awesome-lib instead"}
	state, succ, _ := decideNpmEOL("awesome/old", "1.0.0", info)
	if state != domain.EOLEndOfLife {
		t.Fatalf("expected EOLEndOfLife")
	}
	if succ != "" {
		t.Fatalf("expected empty successor (message-only extraction is not re-run), got %q", succ)
	}
}

func TestDetectShortMessage_NegativeWithSuccessorPhrase(t *testing.T) {
	txt := "This project is not deprecated; use otherlib instead"
	res := eoltext.DetectLifecycle(eoltext.LifecycleDetectOpts{Source: eoltext.SourceShortMessage, PackageName: "oldpkg", Text: txt})
	if res.Matched {
		t.Fatalf("expected no match due to negative context, got %+v", res)
	}
}
