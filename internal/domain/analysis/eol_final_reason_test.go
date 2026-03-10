package analysis

import "testing"

func TestEOLStatus_FinalReason_Priorities(t *testing.T) {
	past := EOLStatus{
		Reason: "catalog human reason",
		Evidences: []EOLEvidence{
			{Source: "HumanCatalog", Summary: "human evidence", Confidence: 1.0},
			{Source: "Other", Summary: "other", Confidence: 0.9},
		},
	}
	if got := past.FinalReason(); got != "catalog human reason" {
		t.Fatalf("want catalog reason, got %q", got)
	}

	noReasonHumanEvidence := EOLStatus{
		Evidences: []EOLEvidence{
			{Source: "HumanCatalog", Summary: "human reviewed reason", Confidence: 0.4},
			{Source: "Other", Summary: "other evidence", Confidence: 0.9},
		},
	}
	if got := noReasonHumanEvidence.FinalReason(); got != "human reviewed reason" {
		t.Fatalf("want human catalog evidence, got %q", got)
	}

	highestConfidence := EOLStatus{
		Evidences: []EOLEvidence{
			{Source: "Other", Summary: "low", Confidence: 0.1},
			{Source: "Other", Summary: "high", Confidence: 0.9},
			{Source: "Other", Summary: "mid", Confidence: 0.5},
		},
	}
	if got := highestConfidence.FinalReason(); got != "high" {
		t.Fatalf("want highest confidence summary, got %q", got)
	}

	tieConfidence := EOLStatus{
		Evidences: []EOLEvidence{
			{Source: "Other", Summary: "first", Confidence: 0.8},
			{Source: "Other", Summary: "second", Confidence: 0.8},
		},
	}
	if got := tieConfidence.FinalReason(); got != "first" {
		t.Fatalf("want first summary on tie, got %q", got)
	}

	noEvidence := EOLStatus{}
	if got := noEvidence.FinalReason(); got != "" {
		t.Fatalf("want empty string when no evidence, got %q", got)
	}
}

func TestEOLStatus_FinalReasonJa(t *testing.T) {
	withJa := EOLStatus{Reason: "english", ReasonJa: "にほんご"}
	if got := withJa.FinalReasonJa(); got != "にほんご" {
		t.Fatalf("want Japanese reason, got %q", got)
	}

	fallback := EOLStatus{Reason: "english"}
	if got := fallback.FinalReasonJa(); got != "english" {
		t.Fatalf("want fallback english reason, got %q", got)
	}

	fallbackEvidence := EOLStatus{Evidences: []EOLEvidence{{Source: "Other", Summary: "ev", Confidence: 0.5}}}
	if got := fallbackEvidence.FinalReasonJa(); got != "ev" {
		t.Fatalf("want evidence summary, got %q", got)
	}
}
