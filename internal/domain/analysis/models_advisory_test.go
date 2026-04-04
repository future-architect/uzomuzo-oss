package analysis

import "testing"

func TestVersionDetail_DirectAdvisories(t *testing.T) {
	vd := &VersionDetail{
		Advisories: []Advisory{
			{ID: "GHSA-aaa", Relation: AdvisoryRelationDirect},
			{ID: "GHSA-bbb", Relation: AdvisoryRelationTransitive, DependencyName: "qs@6.5.5"},
			{ID: "GHSA-ccc", Relation: ""}, // legacy — treated as direct
			{ID: "GHSA-ddd", Relation: AdvisoryRelationTransitive, DependencyName: "form-data@2.3.3"},
		},
	}

	direct := vd.DirectAdvisories()
	if len(direct) != 2 {
		t.Fatalf("expected 2 direct advisories, got %d", len(direct))
	}
	if direct[0].ID != "GHSA-aaa" || direct[1].ID != "GHSA-ccc" {
		t.Errorf("unexpected direct advisories: %v", direct)
	}
}

func TestVersionDetail_TransitiveAdvisories(t *testing.T) {
	vd := &VersionDetail{
		Advisories: []Advisory{
			{ID: "GHSA-aaa", Relation: AdvisoryRelationDirect},
			{ID: "GHSA-bbb", Relation: AdvisoryRelationTransitive, DependencyName: "qs@6.5.5"},
			{ID: "GHSA-ccc", Relation: ""},
			{ID: "GHSA-ddd", Relation: AdvisoryRelationTransitive, DependencyName: "form-data@2.3.3"},
		},
	}

	transitive := vd.TransitiveAdvisories()
	if len(transitive) != 2 {
		t.Fatalf("expected 2 transitive advisories, got %d", len(transitive))
	}
	if transitive[0].ID != "GHSA-bbb" || transitive[1].ID != "GHSA-ddd" {
		t.Errorf("unexpected transitive advisories: %v", transitive)
	}
}

func TestVersionDetail_DirectAdvisoryCount(t *testing.T) {
	vd := &VersionDetail{
		Advisories: []Advisory{
			{ID: "GHSA-aaa", Relation: AdvisoryRelationDirect},
			{ID: "GHSA-bbb", Relation: AdvisoryRelationTransitive},
			{ID: "GHSA-ccc", Relation: ""}, // treated as direct
		},
	}
	if got := vd.DirectAdvisoryCount(); got != 2 {
		t.Errorf("DirectAdvisoryCount() = %d, want 2", got)
	}
}

func TestVersionDetail_TransitiveAdvisoryCount(t *testing.T) {
	vd := &VersionDetail{
		Advisories: []Advisory{
			{ID: "GHSA-aaa", Relation: AdvisoryRelationDirect},
			{ID: "GHSA-bbb", Relation: AdvisoryRelationTransitive},
			{ID: "GHSA-ccc", Relation: AdvisoryRelationTransitive},
		},
	}
	if got := vd.TransitiveAdvisoryCount(); got != 2 {
		t.Errorf("TransitiveAdvisoryCount() = %d, want 2", got)
	}
}

func TestVersionDetail_MaxTransitiveCVSS3(t *testing.T) {
	vd := &VersionDetail{
		Advisories: []Advisory{
			{ID: "GHSA-aaa", Relation: AdvisoryRelationDirect, CVSS3Score: 9.8},
			{ID: "GHSA-bbb", Relation: AdvisoryRelationTransitive, CVSS3Score: 6.5},
			{ID: "GHSA-ccc", Relation: AdvisoryRelationTransitive, CVSS3Score: 3.7},
		},
	}
	if got := vd.MaxTransitiveCVSS3(); got != 6.5 {
		t.Errorf("MaxTransitiveCVSS3() = %f, want 6.5", got)
	}
}

func TestVersionDetail_MaxTransitiveCVSS3_NoTransitive(t *testing.T) {
	vd := &VersionDetail{
		Advisories: []Advisory{
			{ID: "GHSA-aaa", Relation: AdvisoryRelationDirect, CVSS3Score: 9.8},
		},
	}
	if got := vd.MaxTransitiveCVSS3(); got != 0 {
		t.Errorf("MaxTransitiveCVSS3() = %f, want 0", got)
	}
}
