//go:build cgo

package treesitter

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyzer_Java(t *testing.T) {
	dir := t.TempDir()
	// Java variable-declaration resolution: "Gson gson = new Gson()" should
	// allow "gson.toJson()" to be counted as a call site for the Gson import.
	err := os.WriteFile(filepath.Join(dir, "Main.java"), []byte(`import com.google.gson.Gson;

public class Main {
    public static void main(String[] args) {
        Gson gson = new Gson();
        String json = gson.toJson("hello");
        String json2 = gson.fromJson("{}", String.class);
    }
}
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:maven/com.google.code.gson/gson@2.10": {"com.google.gson"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	ca, ok := result["pkg:maven/com.google.code.gson/gson@2.10"]
	if !ok {
		t.Fatal("expected coupling analysis for gson")
	}

	if ca.ImportFileCount != 1 {
		t.Errorf("ImportFileCount = %d, want 1", ca.ImportFileCount)
	}
	if ca.CallSiteCount != 3 {
		t.Errorf("CallSiteCount = %d, want 3 (new Gson, toJson, fromJson)", ca.CallSiteCount)
	}
	if ca.APIBreadth != 3 {
		t.Errorf("APIBreadth = %d, want 3 (Gson, toJson, fromJson)", ca.APIBreadth)
	}
	if ca.IsUnused {
		t.Error("IsUnused = true, want false")
	}
}

func TestAnalyzer_JavaStaticCall(t *testing.T) {
	dir := t.TempDir()
	// Static calls use the class name directly (e.g., StringUtils.isBlank).
	err := os.WriteFile(filepath.Join(dir, "Main.java"), []byte(`import org.apache.commons.lang3.StringUtils;

public class Main {
    public static void main(String[] args) {
        boolean b = StringUtils.isBlank("");
    }
}
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:maven/org.apache.commons/commons-lang3@3.14": {"org.apache.commons.lang3"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	ca, ok := result["pkg:maven/org.apache.commons/commons-lang3@3.14"]
	if !ok {
		t.Fatal("expected coupling analysis for commons-lang3")
	}

	if ca.CallSiteCount != 1 {
		t.Errorf("CallSiteCount = %d, want 1", ca.CallSiteCount)
	}
}

func TestAnalyzer_JavaStaticImport(t *testing.T) {
	dir := t.TempDir()
	// Static imports bring individual methods/fields into scope without qualification.
	// "import static org.junit.Assert.assertEquals" allows bare "assertEquals()" calls.
	err := os.WriteFile(filepath.Join(dir, "Main.java"), []byte(`import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertTrue;
import org.junit.Test;

public class Main {
    @Test
    public void testSomething() {
        assertEquals("hello", "hello");
        assertEquals(42, 42);
        assertTrue(true);
    }
}
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:maven/junit/junit@4.13.2": {"org.junit"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	ca, ok := result["pkg:maven/junit/junit@4.13.2"]
	if !ok {
		t.Fatal("expected coupling analysis for junit")
	}

	// The fixture has 3 relevant imported symbols: 2 static (assertEquals, assertTrue)
	// and 1 regular (Test), all declared in the same file, so ImportFileCount = 1.
	if ca.ImportFileCount != 1 {
		t.Errorf("ImportFileCount = %d, want 1", ca.ImportFileCount)
	}
	// 4 call sites: assertEquals() x2 + assertTrue() x1 + @Test x1
	if ca.CallSiteCount != 4 {
		t.Errorf("CallSiteCount = %d, want 4", ca.CallSiteCount)
	}
	// 3 distinct symbols: assertEquals, assertTrue, Test (annotation)
	if ca.APIBreadth != 3 {
		t.Errorf("APIBreadth = %d, want 3 (assertEquals, assertTrue, Test)", ca.APIBreadth)
	}
	if ca.IsUnused {
		t.Error("IsUnused = true, want false")
	}
}

func TestAnalyzer_JavaCaseInsensitivePURL(t *testing.T) {
	dir := t.TempDir()
	// Java import paths are case-sensitive, but the PURL-derived importToPURL
	// keys are lowercased. Lookup must be case-insensitive.
	err := os.WriteFile(filepath.Join(dir, "Main.java"), []byte(`import com.Google.Gson.Gson;

public class Main {
    public static void main(String[] args) {
        Gson g = new Gson();
        g.toJson("test");
    }
}
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:maven/com.google.code.gson/gson@2.10": {"com.google.gson"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	ca, ok := result["pkg:maven/com.google.code.gson/gson@2.10"]
	if !ok {
		t.Fatal("expected coupling analysis for case-mismatched Java import")
	}

	if ca.ImportFileCount != 1 {
		t.Errorf("ImportFileCount = %d, want 1", ca.ImportFileCount)
	}
}

func TestAnalyzer_JavaAnnotation(t *testing.T) {
	dir := t.TempDir()
	// Java annotation libraries (e.g., @Nullable, @Inject) are imported
	// but their usage via annotations was not counted as call sites.
	// This test verifies that annotations contribute to call_site_count.
	err := os.WriteFile(filepath.Join(dir, "Main.java"), []byte(`import javax.annotation.Nullable;
import com.google.inject.Inject;
import com.fasterxml.jackson.annotation.JsonProperty;

public class Main {
    @Inject
    private Service service;

    @Nullable
    public String getName() {
        return null;
    }

    @JsonProperty("name")
    public String name;
}
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:maven/com.google.code.findbugs/jsr305@3.0.2":               {"javax.annotation"},
		"pkg:maven/com.google.inject/guice@5.1":                         {"com.google.inject"},
		"pkg:maven/com.fasterxml.jackson.core/jackson-annotations@2.15": {"com.fasterxml.jackson.annotation"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name          string
		purl          string
		wantImports   int
		wantCallSites int
		wantBreadth   int
	}{
		{
			name:          "marker annotation @Nullable",
			purl:          "pkg:maven/com.google.code.findbugs/jsr305@3.0.2",
			wantImports:   1,
			wantCallSites: 1,
			wantBreadth:   1,
		},
		{
			name:          "marker annotation @Inject",
			purl:          "pkg:maven/com.google.inject/guice@5.1",
			wantImports:   1,
			wantCallSites: 1,
			wantBreadth:   1,
		},
		{
			name:          "annotation with arguments @JsonProperty",
			purl:          "pkg:maven/com.fasterxml.jackson.core/jackson-annotations@2.15",
			wantImports:   1,
			wantCallSites: 1,
			wantBreadth:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ca, ok := result[tt.purl]
			if !ok {
				t.Fatalf("expected coupling analysis for %s", tt.purl)
			}
			if ca.ImportFileCount != tt.wantImports {
				t.Errorf("ImportFileCount = %d, want %d", ca.ImportFileCount, tt.wantImports)
			}
			if ca.CallSiteCount != tt.wantCallSites {
				t.Errorf("CallSiteCount = %d, want %d", ca.CallSiteCount, tt.wantCallSites)
			}
			if ca.APIBreadth != tt.wantBreadth {
				t.Errorf("APIBreadth = %d, want %d", ca.APIBreadth, tt.wantBreadth)
			}
			if ca.IsUnused {
				t.Error("IsUnused = true, want false")
			}
		})
	}
}

func TestAnalyzer_JavaImplementsExtends(t *testing.T) {
	dir := t.TempDir()
	// Interface inheritance (implements/extends) should count as call sites.
	// These are type references that indicate coupling to the dependency.
	err := os.WriteFile(filepath.Join(dir, "MyPublisher.java"), []byte(`import org.reactivestreams.Publisher;
import junit.framework.TestCase;

public class MyPublisher implements Publisher {
    public void subscribe(Object subscriber) {}
}

class MyTest extends TestCase {
    public void testSomething() {}
}
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:maven/org.reactivestreams/reactive-streams@1.0.4": {"org.reactivestreams"},
		"pkg:maven/junit/junit@4.13.2":                         {"junit.framework"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name          string
		purl          string
		wantImports   int
		wantCallSites int
		wantBreadth   int
	}{
		{
			name:          "implements Publisher",
			purl:          "pkg:maven/org.reactivestreams/reactive-streams@1.0.4",
			wantImports:   1,
			wantCallSites: 1,
			wantBreadth:   1,
		},
		{
			name:          "extends TestCase",
			purl:          "pkg:maven/junit/junit@4.13.2",
			wantImports:   1,
			wantCallSites: 1,
			wantBreadth:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ca, ok := result[tt.purl]
			if !ok {
				t.Fatalf("expected coupling analysis for %s", tt.purl)
			}
			if ca.ImportFileCount != tt.wantImports {
				t.Errorf("ImportFileCount = %d, want %d", ca.ImportFileCount, tt.wantImports)
			}
			if ca.CallSiteCount != tt.wantCallSites {
				t.Errorf("CallSiteCount = %d, want %d", ca.CallSiteCount, tt.wantCallSites)
			}
			if ca.APIBreadth != tt.wantBreadth {
				t.Errorf("APIBreadth = %d, want %d", ca.APIBreadth, tt.wantBreadth)
			}
			if ca.IsUnused {
				t.Error("IsUnused = true, want false")
			}
		})
	}
}

func TestAnalyzer_JavaConstructorCall(t *testing.T) {
	dir := t.TempDir()
	// Constructor calls (new Type()) should count as call sites.
	// This captures usage like "new Gson()", "new ObjectMapper()" where the
	// class name is used directly without a method_invocation on an alias.
	err := os.WriteFile(filepath.Join(dir, "Main.java"), []byte(`import com.google.gson.Gson;
import com.fasterxml.jackson.databind.ObjectMapper;

public class Main {
    public static void main(String[] args) {
        Gson gson = new Gson();
        ObjectMapper mapper = new ObjectMapper();
    }
}
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:maven/com.google.code.gson/gson@2.10":                     {"com.google.gson"},
		"pkg:maven/com.fasterxml.jackson.core/jackson-databind@2.15.2": {"com.fasterxml.jackson.databind"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name           string
		purl           string
		wantMinCalls   int
		wantMinBreadth int
	}{
		{
			name:           "new Gson() constructor",
			purl:           "pkg:maven/com.google.code.gson/gson@2.10",
			wantMinCalls:   1,
			wantMinBreadth: 1,
		},
		{
			name:           "new ObjectMapper() constructor",
			purl:           "pkg:maven/com.fasterxml.jackson.core/jackson-databind@2.15.2",
			wantMinCalls:   1,
			wantMinBreadth: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ca, ok := result[tt.purl]
			if !ok {
				t.Fatalf("expected coupling analysis for %s", tt.purl)
			}
			if ca.CallSiteCount < tt.wantMinCalls {
				t.Errorf("CallSiteCount = %d, want >= %d", ca.CallSiteCount, tt.wantMinCalls)
			}
			if ca.APIBreadth < tt.wantMinBreadth {
				t.Errorf("APIBreadth = %d, want >= %d", ca.APIBreadth, tt.wantMinBreadth)
			}
			if ca.IsUnused {
				t.Error("IsUnused = true, want false")
			}
		})
	}
}
