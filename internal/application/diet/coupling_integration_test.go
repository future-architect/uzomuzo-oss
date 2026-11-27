//go:build cgo

package diet

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	domaindiet "github.com/future-architect/uzomuzo-oss/internal/domain/diet"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/treesitter"
)

func TestRun_TypesPackagesAreNotEasyWins_Integration(t *testing.T) {
	const fooPURL = "pkg:npm/foo@1.0.0"
	const barPURL = "pkg:npm/bar@1.0.0"
	tests := []struct {
		name      string
		typesPURL string
	}{
		{name: "encoded scope", typesPURL: "pkg:npm/%40types/foo@1.0.0"},
		{name: "literal scope", typesPURL: "pkg:npm/@types/foo@1.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "src"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "src", "index.ts"), []byte("import foo from 'foo';\nfoo.run();\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			graphResult := &domaindiet.GraphResult{
				DirectDeps: []string{fooPURL, tt.typesPURL, barPURL},
				Metrics: map[string]*domaindiet.GraphMetrics{
					fooPURL:      {ExclusiveTransitiveCount: 0},
					tt.typesPURL: {ExclusiveTransitiveCount: 1},
					barPURL:      {ExclusiveTransitiveCount: 1},
				},
			}
			analyzer := treesitter.NewAnalyzer()
			t.Cleanup(analyzer.Close)
			service := NewService(&stubGraphAnalyzer{result: graphResult}, analyzer, nil, nil)
			plan, err := service.Run(context.Background(), DietInput{SBOMData: []byte("test-sbom"), SourceRoot: dir})
			if err != nil {
				t.Fatalf("Run() error: %v", err)
			}
			if plan.Summary.EasyWins != 1 {
				t.Errorf("EasyWins = %d, want 1 (bar only)", plan.Summary.EasyWins)
			}
			if len(plan.Entries) != 3 {
				t.Fatalf("entries = %d, want 3", len(plan.Entries))
			}
			entries := make(map[string]domaindiet.DietEntry, len(plan.Entries))
			for _, entry := range plan.Entries {
				entries[entry.PURL] = entry
			}
			foo, ok := entries[fooPURL]
			if !ok {
				t.Fatalf("missing entry for %s", fooPURL)
			}
			if foo.Coupling.IsUnused || foo.Coupling.ImportFileCount != 1 || foo.Coupling.CallSiteCount != 1 {
				t.Errorf("foo coupling = %+v, want one used import and call", foo.Coupling)
			}
			for _, purl := range []string{tt.typesPURL, barPURL} {
				entry, ok := entries[purl]
				if !ok {
					t.Errorf("missing entry for %s", purl)
					continue
				}
				if !entry.Coupling.IsUnused {
					t.Errorf("%s IsUnused = false, want true", purl)
				}
				if entry.Scores.PriorityScore <= 0.3 {
					t.Errorf("%s PriorityScore = %f, want > 0.3", purl, entry.Scores.PriorityScore)
				}
				if purl == tt.typesPURL && (entry.Name != "@types/foo" || entry.Ecosystem != "npm") {
					t.Errorf("type definitions entry = (%q, %q), want (@types/foo, npm)", entry.Name, entry.Ecosystem)
				}
			}
		})
	}
}

// TestBuildImportPaths_AnalyzeCoupling_Integration verifies the full pipeline
// from PURL to buildImportPaths to AnalyzeCoupling, ensuring that the generated
// import paths actually match real Java source code. This catches regressions
// in buildImportPaths that unit tests with hardcoded paths would mask.
func TestBuildImportPaths_AnalyzeCoupling_Integration(t *testing.T) {
	tests := []struct {
		name            string
		purl            string
		sourceFile      string
		sourceCode      string
		wantImportCount int
		wantCallSites   int
		wantUnused      bool
	}{
		{
			name:       "gson via override mapping",
			purl:       "pkg:maven/com.google.code.gson/gson@2.10",
			sourceFile: "Main.java",
			sourceCode: `import com.google.gson.Gson;

public class Main {
    public static void main(String[] args) {
        Gson gson = new Gson();
        String json = gson.toJson("hello");
        String json2 = gson.fromJson("{}", String.class);
    }
}
`,
			wantImportCount: 1,
			// 4 call sites: Gson local var type (1) + new Gson() (1) + gson.toJson (1) + gson.fromJson (1)
			wantCallSites: 4,
			wantUnused:    false,
		},
		{
			name:       "commons-lang3 via groupId heuristic",
			purl:       "pkg:maven/org.apache.commons/commons-lang3@3.14",
			sourceFile: "Main.java",
			sourceCode: `import org.apache.commons.lang3.StringUtils;

public class Main {
    public static void main(String[] args) {
        boolean b = StringUtils.isBlank("");
    }
}
`,
			wantImportCount: 1,
			wantCallSites:   1,
			wantUnused:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			err := os.WriteFile(filepath.Join(dir, tt.sourceFile), []byte(tt.sourceCode), 0644)
			if err != nil {
				t.Fatalf("failed to write source file: %v", err)
			}

			// Use the real buildImportPaths — not hardcoded paths.
			importPaths := buildImportPaths([]string{tt.purl})
			paths := importPaths[tt.purl]
			if len(paths) == 0 {
				t.Fatalf("buildImportPaths(%q) returned no candidate import paths for the current PURL", tt.purl)
			}

			t.Logf("buildImportPaths(%q) = %v", tt.purl, paths)

			analyzer := treesitter.NewAnalyzer()
			t.Cleanup(analyzer.Close)
			result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
			if err != nil {
				t.Fatalf("AnalyzeCoupling() error: %v", err)
			}

			ca, ok := result[tt.purl]
			if !ok {
				if tt.wantImportCount > 0 || tt.wantCallSites > 0 {
					t.Fatalf("no coupling result for %s; buildImportPaths generated %v but none matched the source",
						tt.purl, importPaths[tt.purl])
				}
				// No result and we expected zero coupling — pass.
				return
			}

			if ca.ImportFileCount != tt.wantImportCount {
				t.Errorf("ImportFileCount = %d, want %d", ca.ImportFileCount, tt.wantImportCount)
			}
			if ca.CallSiteCount != tt.wantCallSites {
				t.Errorf("CallSiteCount = %d, want %d", ca.CallSiteCount, tt.wantCallSites)
			}
			if ca.IsUnused != tt.wantUnused {
				t.Errorf("IsUnused = %v, want %v", ca.IsUnused, tt.wantUnused)
			}
		})
	}

	// Test unused dependency by including a used PURL alongside the unused one.
	// When no dependency matches any source, AnalyzeCoupling returns nil (coupling
	// unavailable). A used PURL ensures the analyzer returns non-nil results so the
	// unused PURL's absence from the result map is meaningful.
	t.Run("unused dependency alongside used dependency", func(t *testing.T) {
		dir := t.TempDir()
		sourceCode := `import com.google.gson.Gson;

public class Main {
    public static void main(String[] args) {
        Gson gson = new Gson();
    }
}
`
		err := os.WriteFile(filepath.Join(dir, "Main.java"), []byte(sourceCode), 0644)
		if err != nil {
			t.Fatalf("failed to write source file: %v", err)
		}

		usedPURL := "pkg:maven/com.google.code.gson/gson@2.10"
		unusedPURL := "pkg:maven/org.apache.commons/commons-lang3@3.14"

		importPaths := buildImportPaths([]string{usedPURL, unusedPURL})
		t.Logf("importPaths = %v", importPaths)

		analyzer := treesitter.NewAnalyzer()
		t.Cleanup(analyzer.Close)
		result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
		if err != nil {
			t.Fatalf("AnalyzeCoupling() error: %v", err)
		}
		if result == nil {
			t.Fatal("AnalyzeCoupling() returned nil; expected non-nil with at least the used dependency")
		}

		// The used dependency must be present.
		if _, ok := result[usedPURL]; !ok {
			t.Errorf("expected coupling result for used PURL %s", usedPURL)
		}

		// The unused dependency must either be absent or marked as unused.
		if ca, ok := result[unusedPURL]; ok {
			if !ca.IsUnused {
				t.Errorf("unused PURL %s: IsUnused = false, want true", unusedPURL)
			}
		}
		// Absent from the result map is also acceptable — means no coupling data collected.
	})
}
