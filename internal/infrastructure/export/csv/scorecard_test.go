package csv

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"slices"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

func TestExportScorecard_DependentsColumns(t *testing.T) {
	zero, direct, total := 0, 5, 7
	analyses := map[string]*domain.Analysis{
		"pkg:npm/a@1.0.0":     {Repository: &domain.Repository{}, DependentCount: 7, Dependents: &domain.DependentsObservation{Source: domain.DependentsSourceDepsDev, Version: "1.0.0", Direct: &direct, Total: &total}},
		"pkg:npm/b@2.0.0":     {Repository: &domain.Repository{}, Dependents: &domain.DependentsObservation{Source: domain.DependentsSourceDepsDev, Version: "2.0.0", Direct: &zero, Total: &zero}},
		"pkg:gem/c@1.0.0":     {Repository: &domain.Repository{}, DependentCount: 5, Dependents: &domain.DependentsObservation{Source: domain.DependentsSourceRubyGems, Direct: &direct}},
		"pkg:golang/d@v1.0.0": {Repository: &domain.Repository{}},
	}
	path := filepath.Join(t.TempDir(), "scorecard.csv")
	if err := ExportScorecard(analyses, path); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rows[0][len(rows[0])-4:], []string{"dependents_source", "dependents_version", "dependents_direct", "dependents_total"}) {
		t.Fatalf("header tail: %v", rows[0])
	}
	want := map[string][]string{
		"pkg:npm/a@1.0.0":     {"deps.dev", "1.0.0", "5", "7"},
		"pkg:npm/b@2.0.0":     {"deps.dev", "2.0.0", "0", "0"},
		"pkg:gem/c@1.0.0":     {"RubyGems", "", "5", ""},
		"pkg:golang/d@v1.0.0": {"", "", "", ""},
	}
	for _, row := range rows[1:] {
		if !slices.Equal(row[len(row)-4:], want[row[0]]) {
			t.Errorf("%s: tail=%v, want %v", row[0], row[len(row)-4:], want[row[0]])
		}
		if row[0] == "pkg:npm/a@1.0.0" && row[14] != "7" {
			t.Errorf("legacy count moved or changed: %v", row)
		}
	}
}
