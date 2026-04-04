package integration

import (
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
)

func TestCollectAdvisoryIDs(t *testing.T) {
	t.Run("nil VersionDetail", func(t *testing.T) {
		idSet := make(map[string]struct{})
		collectAdvisoryIDs(nil, idSet)
		if len(idSet) != 0 {
			t.Fatalf("expected empty set, got %d entries", len(idSet))
		}
	})

	t.Run("deduplicates across versions", func(t *testing.T) {
		idSet := make(map[string]struct{})
		vd1 := &domain.VersionDetail{
			Advisories: []domain.Advisory{
				{ID: "GHSA-aaa"},
				{ID: "GHSA-bbb"},
			},
		}
		vd2 := &domain.VersionDetail{
			Advisories: []domain.Advisory{
				{ID: "GHSA-aaa"}, // duplicate
				{ID: "GHSA-ccc"},
			},
		}
		collectAdvisoryIDs(vd1, idSet)
		collectAdvisoryIDs(vd2, idSet)
		if len(idSet) != 3 {
			t.Fatalf("expected 3 unique IDs, got %d", len(idSet))
		}
		for _, id := range []string{"GHSA-aaa", "GHSA-bbb", "GHSA-ccc"} {
			if _, ok := idSet[id]; !ok {
				t.Errorf("missing expected ID %s", id)
			}
		}
	})
}

func TestEnrichVersionAdvisories(t *testing.T) {
	t.Run("nil VersionDetail", func(t *testing.T) {
		// Should not panic.
		enrichVersionAdvisories(nil, nil)
	})

	t.Run("populates severity and sorts by CVSS3 descending", func(t *testing.T) {
		vd := &domain.VersionDetail{
			Advisories: []domain.Advisory{
				{ID: "GHSA-low", Source: "GHSA"},
				{ID: "GHSA-high", Source: "GHSA"},
				{ID: "GHSA-unknown", Source: "GHSA"},
			},
		}
		details := map[string]*depsdev.AdvisoryDetail{
			"GHSA-low":  {Title: "Low issue", CVSS3Score: 3.1},
			"GHSA-high": {Title: "High issue", CVSS3Score: 8.5},
			// GHSA-unknown not in details — simulates fetch failure
		}

		enrichVersionAdvisories(vd, details)

		// Verify sorting: high (8.5) > low (3.1) > unknown (0.0)
		if len(vd.Advisories) != 3 {
			t.Fatalf("expected 3 advisories, got %d", len(vd.Advisories))
		}
		if vd.Advisories[0].ID != "GHSA-high" {
			t.Errorf("expected first advisory to be GHSA-high, got %s", vd.Advisories[0].ID)
		}
		if vd.Advisories[0].CVSS3Score != 8.5 || vd.Advisories[0].Severity != "HIGH" || vd.Advisories[0].Title != "High issue" {
			t.Errorf("unexpected first advisory fields: %+v", vd.Advisories[0])
		}
		if vd.Advisories[1].ID != "GHSA-low" {
			t.Errorf("expected second advisory to be GHSA-low, got %s", vd.Advisories[1].ID)
		}
		if vd.Advisories[1].CVSS3Score != 3.1 || vd.Advisories[1].Severity != "LOW" {
			t.Errorf("unexpected second advisory fields: %+v", vd.Advisories[1])
		}
		// Unknown advisory should retain zero values
		if vd.Advisories[2].ID != "GHSA-unknown" {
			t.Errorf("expected third advisory to be GHSA-unknown, got %s", vd.Advisories[2].ID)
		}
		if vd.Advisories[2].CVSS3Score != 0 || vd.Advisories[2].Severity != "" {
			t.Errorf("unknown advisory should have zero values: %+v", vd.Advisories[2])
		}
	})

	t.Run("stable sort preserves order for equal scores", func(t *testing.T) {
		vd := &domain.VersionDetail{
			Advisories: []domain.Advisory{
				{ID: "GHSA-first", Source: "GHSA"},
				{ID: "GHSA-second", Source: "GHSA"},
			},
		}
		details := map[string]*depsdev.AdvisoryDetail{
			"GHSA-first":  {Title: "First", CVSS3Score: 7.0},
			"GHSA-second": {Title: "Second", CVSS3Score: 7.0},
		}

		enrichVersionAdvisories(vd, details)

		if vd.Advisories[0].ID != "GHSA-first" || vd.Advisories[1].ID != "GHSA-second" {
			t.Errorf("stable sort should preserve order for equal scores: got %s, %s",
				vd.Advisories[0].ID, vd.Advisories[1].ID)
		}
	})
}
