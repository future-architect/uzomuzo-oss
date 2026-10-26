package scan

import (
	"reflect"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
)

func TestSelectDependencies(t *testing.T) {
	const (
		direct     = "pkg:golang/example.com/direct@v1.0.0"
		transitive = "pkg:golang/example.com/transitive@v1.0.0"
		both       = "pkg:golang/example.com/both@v1.0.0"
	)
	deps := []depparser.ParsedDependency{
		{PURL: transitive, Relation: depparser.RelationTransitive, ViaParents: []string{"direct"}},
		{PURL: both, Relation: depparser.RelationTransitive, ViaParents: []string{"direct"}},
		{PURL: direct, Relation: depparser.RelationDirect},
		{PURL: both, Relation: depparser.RelationDirect},
		{PURL: direct, Relation: depparser.RelationDirect},
	}

	tests := []struct {
		name           string
		showTransitive bool
		wantPURLs      []string
		wantRelations  map[string]depparser.DependencyRelation
		wantVia        map[string][]string
	}{
		{
			name:           "transitive skipped by default",
			showTransitive: false,
			wantPURLs:      []string{direct, both},
			wantRelations: map[string]depparser.DependencyRelation{
				direct: depparser.RelationDirect,
				both:   depparser.RelationDirect,
			},
			wantVia: map[string][]string{direct: nil, both: nil},
		},
		{
			name:           "transitive kept, and a PURL also listed as direct is direct",
			showTransitive: true,
			wantPURLs:      []string{transitive, both, direct},
			wantRelations: map[string]depparser.DependencyRelation{
				transitive: depparser.RelationTransitive,
				both:       depparser.RelationDirect,
				direct:     depparser.RelationDirect,
			},
			wantVia: map[string][]string{transitive: {"direct"}, both: nil, direct: nil},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			purls, relations, via := selectDependencies(deps, tt.showTransitive)
			if !reflect.DeepEqual(purls, tt.wantPURLs) {
				t.Errorf("purls = %v, want %v", purls, tt.wantPURLs)
			}
			if !reflect.DeepEqual(relations, tt.wantRelations) {
				t.Errorf("relations = %v, want %v", relations, tt.wantRelations)
			}
			if !reflect.DeepEqual(via, tt.wantVia) {
				t.Errorf("viaParents = %v, want %v", via, tt.wantVia)
			}
		})
	}
}
