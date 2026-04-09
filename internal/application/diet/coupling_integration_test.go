//go:build cgo

package diet

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/treesitter"
)

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
			wantCallSites:   3,
			wantUnused:      false,
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
