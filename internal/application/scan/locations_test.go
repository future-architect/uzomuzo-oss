package scan

import (
	"reflect"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
)

func TestSelectDependencyLocations(t *testing.T) {
	deps := []depparser.ParsedDependency{
		{PURL: "pkg:npm/a@1", Relation: depparser.RelationDirect, Locations: []depparser.SourceLocation{{Line: 4}, {Line: 6}}},
		{PURL: "pkg:npm/a@1", Relation: depparser.RelationDirect, Locations: []depparser.SourceLocation{{Line: 6}, {Line: 9}}},
		{PURL: "pkg:npm/b@1", Relation: depparser.RelationTransitive, Locations: []depparser.SourceLocation{{Line: 10}}},
	}
	purls, _, _, locs := selectDependencies(deps, false)
	if !reflect.DeepEqual(purls, []string{"pkg:npm/a@1"}) {
		t.Fatalf("PURLs = %v", purls)
	}
	want := map[string][]depparser.SourceLocation{"pkg:npm/a@1": {{Line: 4}, {Line: 6}, {Line: 9}}}
	if !reflect.DeepEqual(locs, want) {
		t.Fatalf("locations = %v, want %v", locs, want)
	}
}
