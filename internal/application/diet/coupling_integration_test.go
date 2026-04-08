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
			wantCallSites:   2,
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
		{
			name:       "unused dependency produces zero coupling",
			purl:       "pkg:maven/org.apache.commons/commons-lang3@3.14",
			sourceFile: "Main.java",
			sourceCode: `public class Main {
    public static void main(String[] args) {
        System.out.println("no dependency imports here");
    }
}
`,
			wantImportCount: 0,
			wantCallSites:   0,
			wantUnused:      true,
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
			if len(importPaths) == 0 {
				t.Fatalf("buildImportPaths(%q) returned no entries; expected at least one candidate", tt.purl)
			}

			t.Logf("buildImportPaths(%q) = %v", tt.purl, importPaths[tt.purl])

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
}
