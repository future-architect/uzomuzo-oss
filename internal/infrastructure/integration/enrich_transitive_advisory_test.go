package integration

import (
	"context"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
)

func TestEnrichTransitiveAdvisories(t *testing.T) {
	t.Run("appends transitive advisories to stable version", func(t *testing.T) {
		purls := []string{"pkg:npm/request@2.88.2"}
		analyses := map[string]*domain.Analysis{
			"pkg:npm/request@2.88.2": {
				EffectivePURL: "pkg:npm/request@2.88.2",
				Package:       &domain.Package{Ecosystem: "npm", Version: "2.88.2"},
				ReleaseInfo: &domain.ReleaseInfo{
					StableVersion: &domain.VersionDetail{
						Version: "2.88.2",
						Advisories: []domain.Advisory{
							{ID: "GHSA-p8p7-x288-28g6", Source: "GHSA", URL: "https://github.com/advisories/GHSA-p8p7-x288-28g6"},
						},
					},
				},
			},
		}
		depsGraph := map[string]*depsdev.DependenciesResponse{
			"pkg:npm/request@2.88.2": {
				Nodes: []depsdev.DependencyNode{
					{VersionKey: depsdev.DependencyVersionKey{System: "NPM", Name: "request", Version: "2.88.2"}, Relation: "SELF"},
					{VersionKey: depsdev.DependencyVersionKey{System: "NPM", Name: "qs", Version: "6.5.5"}, Relation: "DIRECT"},
					{VersionKey: depsdev.DependencyVersionKey{System: "NPM", Name: "tough-cookie", Version: "2.5.0"}, Relation: "DIRECT"},
				},
			},
		}

		svc := &IntegrationService{
			depsdevClient: &stubDepsDevClient{
				transitiveAdvisoryResults: map[string][]depsdev.AdvisoryKey{
					"qs@6.5.5":           {{ID: "GHSA-6rw7-vpxm-498p"}},
					"tough-cookie@2.5.0": {{ID: "GHSA-72xf-g2v4-qvf3"}},
				},
				advisoryDetails: map[string]*depsdev.AdvisoryDetail{
					"GHSA-6rw7-vpxm-498p": {Title: "arrayLimit bypass DoS", CVSS3Score: 3.7},
					"GHSA-72xf-g2v4-qvf3": {Title: "Prototype Pollution", CVSS3Score: 6.5},
				},
			},
		}

		svc.enrichTransitiveAdvisories(context.Background(), purls, analyses, depsGraph)

		a := analyses["pkg:npm/request@2.88.2"]
		vd := a.ReleaseInfo.StableVersion
		if len(vd.Advisories) != 3 {
			t.Fatalf("expected 3 advisories (1 direct + 2 transitive), got %d", len(vd.Advisories))
		}

		// Check direct advisory is marked
		foundDirect := false
		for _, adv := range vd.Advisories {
			if adv.ID == "GHSA-p8p7-x288-28g6" {
				if adv.Relation != domain.AdvisoryRelationDirect {
					t.Errorf("expected GHSA-p8p7-x288-28g6 to be DIRECT, got %q", adv.Relation)
				}
				foundDirect = true
			}
		}
		if !foundDirect {
			t.Error("direct advisory GHSA-p8p7-x288-28g6 not found")
		}

		// Check transitive counts
		if got := vd.TransitiveAdvisoryCount(); got != 2 {
			t.Errorf("TransitiveAdvisoryCount() = %d, want 2", got)
		}

		// Check sort order (CVSS3 descending)
		for i := 1; i < len(vd.Advisories); i++ {
			if vd.Advisories[i-1].CVSS3Score < vd.Advisories[i].CVSS3Score {
				t.Errorf("advisories not sorted by CVSS3 descending: [%d]=%f > [%d]=%f",
					i, vd.Advisories[i].CVSS3Score, i-1, vd.Advisories[i-1].CVSS3Score)
			}
		}

		// Check DependencyName is set on transitive advisories
		for _, adv := range vd.TransitiveAdvisories() {
			if adv.DependencyName == "" {
				t.Errorf("transitive advisory %s should have DependencyName set", adv.ID)
			}
		}
	})

	t.Run("skips duplicate advisory IDs already present as direct", func(t *testing.T) {
		purls := []string{"pkg:npm/test@1.0.0"}
		analyses := map[string]*domain.Analysis{
			"pkg:npm/test@1.0.0": {
				EffectivePURL: "pkg:npm/test@1.0.0",
				Package:       &domain.Package{Ecosystem: "npm", Version: "1.0.0"},
				ReleaseInfo: &domain.ReleaseInfo{
					StableVersion: &domain.VersionDetail{
						Version: "1.0.0",
						Advisories: []domain.Advisory{
							{ID: "GHSA-shared", Source: "GHSA"},
						},
					},
				},
			},
		}
		depsGraph := map[string]*depsdev.DependenciesResponse{
			"pkg:npm/test@1.0.0": {
				Nodes: []depsdev.DependencyNode{
					{VersionKey: depsdev.DependencyVersionKey{System: "NPM", Name: "test", Version: "1.0.0"}, Relation: "SELF"},
					{VersionKey: depsdev.DependencyVersionKey{System: "NPM", Name: "dep", Version: "2.0.0"}, Relation: "DIRECT"},
				},
			},
		}

		svc := &IntegrationService{
			depsdevClient: &stubDepsDevClient{
				transitiveAdvisoryResults: map[string][]depsdev.AdvisoryKey{
					"dep@2.0.0": {{ID: "GHSA-shared"}, {ID: "GHSA-unique"}},
				},
				advisoryDetails: map[string]*depsdev.AdvisoryDetail{
					"GHSA-unique": {Title: "Unique issue", CVSS3Score: 5.0},
				},
			},
		}

		svc.enrichTransitiveAdvisories(context.Background(), purls, analyses, depsGraph)

		vd := analyses["pkg:npm/test@1.0.0"].ReleaseInfo.StableVersion
		if len(vd.Advisories) != 2 {
			t.Fatalf("expected 2 advisories (1 direct + 1 unique transitive), got %d", len(vd.Advisories))
		}

		// GHSA-shared should be DIRECT, not duplicated as TRANSITIVE
		for _, adv := range vd.Advisories {
			if adv.ID == "GHSA-shared" && adv.Relation == domain.AdvisoryRelationTransitive {
				t.Error("GHSA-shared should not appear as TRANSITIVE when it's already DIRECT")
			}
		}
	})

	t.Run("nil deps graph results is a no-op", func(t *testing.T) {
		svc := &IntegrationService{
			depsdevClient: &stubDepsDevClient{},
		}
		// Should not panic.
		svc.enrichTransitiveAdvisories(context.Background(), nil, nil, nil)
	})

	t.Run("empty deps graph results is a no-op", func(t *testing.T) {
		svc := &IntegrationService{
			depsdevClient: &stubDepsDevClient{},
		}
		svc.enrichTransitiveAdvisories(context.Background(), []string{"pkg:npm/x@1.0.0"}, map[string]*domain.Analysis{}, map[string]*depsdev.DependenciesResponse{})
	})
}

func TestMarkDirectAdvisories(t *testing.T) {
	t.Run("sets DIRECT on empty relation", func(t *testing.T) {
		vd := &domain.VersionDetail{
			Advisories: []domain.Advisory{
				{ID: "GHSA-aaa", Relation: ""},
				{ID: "GHSA-bbb", Relation: domain.AdvisoryRelationTransitive},
			},
		}
		markDirectAdvisories(vd)
		if vd.Advisories[0].Relation != domain.AdvisoryRelationDirect {
			t.Errorf("expected DIRECT, got %q", vd.Advisories[0].Relation)
		}
		// Should not overwrite existing non-empty relation
		if vd.Advisories[1].Relation != domain.AdvisoryRelationTransitive {
			t.Errorf("should not overwrite TRANSITIVE, got %q", vd.Advisories[1].Relation)
		}
	})

	t.Run("nil version detail does not panic", func(t *testing.T) {
		markDirectAdvisories(nil)
	})
}

func TestAppendTransitiveAdvisories(t *testing.T) {
	t.Run("deduplicates against existing entries", func(t *testing.T) {
		vd := &domain.VersionDetail{
			Advisories: []domain.Advisory{
				{ID: "GHSA-aaa", CVSS3Score: 8.0, Relation: domain.AdvisoryRelationDirect},
			},
		}
		newAdvisories := []domain.Advisory{
			{ID: "GHSA-aaa", CVSS3Score: 8.0, Relation: domain.AdvisoryRelationTransitive}, // duplicate
			{ID: "GHSA-bbb", CVSS3Score: 5.0, Relation: domain.AdvisoryRelationTransitive},
		}

		appendTransitiveAdvisories(vd, newAdvisories)

		if len(vd.Advisories) != 2 {
			t.Fatalf("expected 2 advisories, got %d", len(vd.Advisories))
		}
		if vd.Advisories[0].ID != "GHSA-aaa" || vd.Advisories[1].ID != "GHSA-bbb" {
			t.Errorf("unexpected advisory order: %v", vd.Advisories)
		}
	})

	t.Run("nil version detail does not panic", func(t *testing.T) {
		appendTransitiveAdvisories(nil, []domain.Advisory{{ID: "GHSA-aaa"}})
	})
}
