package scan

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/application"
	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
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
	tests := []struct {
		name string
		n    int
		want []ViaCount
	}{
		{"cut at n, ties broken by name", 3, []ViaCount{{Name: "a", Count: 7}, {Name: "b", Count: 7}, {Name: "c", Count: 5}}},
		{"n larger than parents returns all, ordered", 10, []ViaCount{{Name: "a", Count: 7}, {Name: "b", Count: 7}, {Name: "c", Count: 5}, {Name: "d", Count: 1}}},
		{"n of zero returns none", 0, []ViaCount{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.TopVia(tt.n); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("TopVia(%d) = %v, want %v", tt.n, got, tt.want)
			}
		})
	}
}

// fixtureSource answers every lookup with a failed analysis so RunFromParser
// can run its full path (evaluation included) without a network.
type fixtureSource struct{}

func (fixtureSource) AnalyzeFromPURLs(_ context.Context, keys []string) (map[string]*analysis.Analysis, error) {
	out := make(map[string]*analysis.Analysis, len(keys))
	for _, k := range keys {
		out[k] = &analysis.Analysis{OriginalPURL: k, Error: errors.New("fixture lookup failure")}
	}
	return out, nil
}

func (f fixtureSource) AnalyzeFromGitHubURLs(ctx context.Context, keys []string) (map[string]*analysis.Analysis, error) {
	return f.AnalyzeFromPURLs(ctx, keys)
}

// With a direct dependency present the scan evaluates it and still reports the
// skipped transitive ones; with ShowTransitive nothing is skipped, so nil.
func TestRunFromParser_SkippedTransitiveWithDirect(t *testing.T) {
	svc, err := NewService(application.NewAnalysisService(fixtureSource{}))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := domainscan.ParseFailPolicy("")
	if err != nil {
		t.Fatal(err)
	}
	p := stubParser{deps: []depparser.ParsedDependency{
		{PURL: "pkg:npm/express@1", Relation: depparser.RelationDirect},
		{PURL: "pkg:npm/debug@1", Relation: depparser.RelationTransitive, ViaParents: []string{"express"}},
	}}
	tests := []struct {
		name        string
		cfg         ParserConfig
		wantEntries int
		wantSkipped *TransitiveSkipped
	}{
		{"default skips transitive and reports it", ParserConfig{}, 1, &TransitiveSkipped{Count: 1, Via: map[string]int{"express": 1}}},
		{"ShowTransitive evaluates all and reports nothing", ParserConfig{ShowTransitive: true}, 2, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := svc.RunFromParser(context.Background(), p, nil, policy, tt.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Entries) != tt.wantEntries {
				t.Errorf("entries = %d, want %d", len(res.Entries), tt.wantEntries)
			}
			if !reflect.DeepEqual(res.TransitiveSkipped, tt.wantSkipped) {
				t.Errorf("TransitiveSkipped = %+v, want %+v", res.TransitiveSkipped, tt.wantSkipped)
			}
		})
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
	if len(res.Entries) != 0 {
		t.Errorf("entries = %d, want 0", len(res.Entries))
	}
	want := &TransitiveSkipped{Count: 1, Via: map[string]int{"express": 1}}
	if !reflect.DeepEqual(res.TransitiveSkipped, want) {
		t.Errorf("TransitiveSkipped = %+v, want %+v", res.TransitiveSkipped, want)
	}
}
