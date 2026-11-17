package scan

import (
	"context"
	"reflect"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/application"
	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
	domainscan "github.com/future-architect/uzomuzo-oss/internal/domain/scan"
)

type stubParser struct{ deps []depparser.ParsedDependency }

func (p stubParser) Parse(_ context.Context, _ []byte) ([]depparser.ParsedDependency, error) {
	return p.deps, nil
}
func (p stubParser) FormatName() string { return "stub" }

func TestSummarizeSkippedTransitive(t *testing.T) {
	const (
		direct  = "pkg:npm/direct@1.0.0"
		both    = "pkg:npm/both@1.0.0"
		t1      = "pkg:npm/t1@1.0.0"
		t2      = "pkg:npm/t2@1.0.0"
		t3      = "pkg:npm/t3@1.0.0"
		noVia   = "pkg:npm/novia@1.0.0"
		unknown = "pkg:npm/unknown@1.0.0"
	)
	deps := []depparser.ParsedDependency{
		{PURL: direct, Relation: depparser.RelationDirect},
		{PURL: unknown, Relation: depparser.RelationUnknown},
		{PURL: t1, Relation: depparser.RelationTransitive, ViaParents: []string{"express"}},
		{PURL: t1, Relation: depparser.RelationTransitive, ViaParents: []string{"express"}}, // duplicate PURL: counted once
		{PURL: t2, Relation: depparser.RelationTransitive, ViaParents: []string{"express", "fastapi-cli"}},
		{PURL: t3, Relation: depparser.RelationTransitive, ViaParents: []string{"fastapi-cli"}},
		{PURL: noVia, Relation: depparser.RelationTransitive},
		{PURL: both, Relation: depparser.RelationTransitive, ViaParents: []string{"express"}}, // also direct below: not skipped
		{PURL: both, Relation: depparser.RelationDirect},
	}

	t.Run("counts unique skipped PURLs and per-parent totals", func(t *testing.T) {
		_, kept, _, _ := selectDependencies(deps, false)
		got := summarizeSkippedTransitive(deps, kept)
		want := &TransitiveSkipped{
			Count: 4, // t1, t2, t3, noVia
			Via:   map[string]int{"express": 2, "fastapi-cli": 2},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("no transitive deps yields nil", func(t *testing.T) {
		only := []depparser.ParsedDependency{{PURL: direct, Relation: depparser.RelationDirect}}
		_, kept, _, _ := selectDependencies(only, false)
		if got := summarizeSkippedTransitive(only, kept); got != nil {
			t.Errorf("got %+v, want nil", got)
		}
	})
}

func TestTransitiveSkipped_TopVia(t *testing.T) {
	s := &TransitiveSkipped{Count: 20, Via: map[string]int{"b": 7, "a": 7, "c": 5, "d": 1}}
	want := []ViaCount{{Name: "a", Count: 7}, {Name: "b", Count: 7}, {Name: "c", Count: 5}}
	if got := s.TopVia(3); !reflect.DeepEqual(got, want) {
		t.Errorf("TopVia(3) = %v, want %v", got, want)
	}
	if got := s.TopVia(10); len(got) != 4 {
		t.Errorf("TopVia(10) len = %d, want 4", len(got))
	}
}

// Transitive-only input ends before evaluation but must still report what was skipped.
func TestRunFromParser_SkippedTransitiveReported(t *testing.T) {
	svc, err := NewService(application.NewAnalysisService(nil))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := domainscan.ParseFailPolicy("")
	if err != nil {
		t.Fatal(err)
	}
	p := stubParser{deps: []depparser.ParsedDependency{
		{PURL: "pkg:npm/t1@1.0.0", Relation: depparser.RelationTransitive, ViaParents: []string{"express"}},
	}}
	res, err := svc.RunFromParser(context.Background(), p, nil, policy, ParserConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if res.TransitiveSkipped == nil || res.TransitiveSkipped.Count != 1 || res.TransitiveSkipped.Via["express"] != 1 {
		t.Errorf("TransitiveSkipped = %+v, want count 1 via express", res.TransitiveSkipped)
	}
}
