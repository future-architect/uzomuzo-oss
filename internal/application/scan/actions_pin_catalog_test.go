package scan

import (
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	domainaudit "github.com/future-architect/uzomuzo-oss/internal/domain/audit"
)

func TestApplyActionPinCatalog_FlipsDeprecatedPin(t *testing.T) {
	entries := []domainaudit.AuditEntry{
		{
			PURL:       "https://github.com/actions/upload-artifact",
			Source:     domainaudit.SourceActions,
			Verdict:    domainaudit.VerdictOK,
			Analysis:   &analysis.Analysis{EOL: analysis.EOLStatus{State: analysis.EOLNotEOL}},
			ActionRefs: []string{"v3"},
		},
	}

	applyActionPinCatalog(entries)

	got := entries[0]
	if got.Verdict != domainaudit.VerdictReplace {
		t.Errorf("verdict = %q, want %q", got.Verdict, domainaudit.VerdictReplace)
	}
	if got.Analysis.EOL.State != analysis.EOLEndOfLife {
		t.Errorf("EOL state = %q, want %q", got.Analysis.EOL.State, analysis.EOLEndOfLife)
	}
	if got.Analysis.EOL.Successor != "v4" {
		t.Errorf("successor = %q, want %q", got.Analysis.EOL.Successor, "v4")
	}
	if len(got.Analysis.EOL.Evidences) == 0 {
		t.Fatal("expected at least one ActionPinCatalog evidence")
	}
	ev := got.Analysis.EOL.Evidences[len(got.Analysis.EOL.Evidences)-1]
	if ev.Source != "ActionPinCatalog" {
		t.Errorf("evidence source = %q, want ActionPinCatalog", ev.Source)
	}
	if ev.Reference == "" {
		t.Error("evidence reference URL must not be empty")
	}
}

func TestApplyActionPinCatalog_LeavesCurrentPinUntouched(t *testing.T) {
	entries := []domainaudit.AuditEntry{
		{
			PURL:       "https://github.com/actions/upload-artifact",
			Source:     domainaudit.SourceActions,
			Verdict:    domainaudit.VerdictOK,
			Analysis:   &analysis.Analysis{EOL: analysis.EOLStatus{State: analysis.EOLNotEOL}},
			ActionRefs: []string{"v4"}, // current major
		},
	}
	applyActionPinCatalog(entries)
	if entries[0].Verdict != domainaudit.VerdictOK {
		t.Errorf("verdict = %q, want OK (v4 is current)", entries[0].Verdict)
	}
	if entries[0].Analysis.EOL.State != analysis.EOLNotEOL {
		t.Errorf("EOL state changed unexpectedly: %q", entries[0].Analysis.EOL.State)
	}
}

func TestApplyActionPinCatalog_SkipsNonActionSources(t *testing.T) {
	entries := []domainaudit.AuditEntry{
		{
			PURL:       "pkg:npm/express@4.18.2",
			Source:     domainaudit.SourceDirect,
			Verdict:    domainaudit.VerdictOK,
			Analysis:   &analysis.Analysis{EOL: analysis.EOLStatus{State: analysis.EOLNotEOL}},
			ActionRefs: []string{"v3"}, // would match upload-artifact but source is not actions
		},
	}
	applyActionPinCatalog(entries)
	if entries[0].Verdict != domainaudit.VerdictOK {
		t.Errorf("non-action source must not be affected by catalog; got verdict %q", entries[0].Verdict)
	}
}

func TestApplyActionPinCatalog_SkipsSHAAndBranchPins(t *testing.T) {
	tests := []struct {
		name string
		ref  string
	}{
		{"SHA", "de0fac2e4500dabe0009e67214ff5f5447ce83dd"},
		{"branch main", "main"},
		{"branch master", "master"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries := []domainaudit.AuditEntry{
				{
					PURL:       "https://github.com/actions/checkout",
					Source:     domainaudit.SourceActions,
					Verdict:    domainaudit.VerdictOK,
					Analysis:   &analysis.Analysis{EOL: analysis.EOLStatus{State: analysis.EOLNotEOL}},
					ActionRefs: []string{tt.ref},
				},
			}
			applyActionPinCatalog(entries)
			if entries[0].Verdict != domainaudit.VerdictOK {
				t.Errorf("unresolvable ref %q should not flip verdict; got %q", tt.ref, entries[0].Verdict)
			}
		})
	}
}

func TestApplyActionPinCatalog_MixedPinsFlipOnAnyMatch(t *testing.T) {
	entries := []domainaudit.AuditEntry{
		{
			PURL:       "https://github.com/actions/checkout",
			Source:     domainaudit.SourceActions,
			Verdict:    domainaudit.VerdictOK,
			Analysis:   &analysis.Analysis{EOL: analysis.EOLStatus{State: analysis.EOLNotEOL}},
			ActionRefs: []string{"v4", "v2"}, // one current, one EOL
		},
	}
	applyActionPinCatalog(entries)
	if entries[0].Verdict != domainaudit.VerdictReplace {
		t.Errorf("verdict should be replace when any ref is deprecated; got %q", entries[0].Verdict)
	}
}

func TestApplyActionRefs_PopulatesOnlyActionSources(t *testing.T) {
	entries := []domainaudit.AuditEntry{
		{PURL: "https://github.com/actions/checkout", Source: domainaudit.SourceActions},
		{PURL: "pkg:npm/express@4.18.2", Source: domainaudit.SourceDirect},
	}
	refs := map[string][]string{
		"https://github.com/actions/checkout": {"v4"},
		"pkg:npm/express@4.18.2":              {"ignored"},
	}
	applyActionRefs(entries, refs)
	if len(entries[0].ActionRefs) != 1 || entries[0].ActionRefs[0] != "v4" {
		t.Errorf("action entry refs = %v, want [v4]", entries[0].ActionRefs)
	}
	if len(entries[1].ActionRefs) != 0 {
		t.Errorf("non-action entry must not receive refs, got %v", entries[1].ActionRefs)
	}
}
