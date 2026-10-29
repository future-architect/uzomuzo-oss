package gomod_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depparser/gomod"
)

func TestRequirementLocations(t *testing.T) {
	deps, err := (&gomod.Parser{}).Parse(context.Background(), readTestData(t, "indirect_replace_tool.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 3 {
		t.Fatalf("dependencies = %d", len(deps))
	}
	for i, line := range []int{5, 8, 9} {
		if want := []depparser.SourceLocation{{Line: line}}; !reflect.DeepEqual(deps[i].Locations, want) {
			t.Errorf("%s locations = %v, want %v", deps[i].PURL, deps[i].Locations, want)
		}
	}
}
