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

func TestAnalyzer_JavaGenericType(t *testing.T) {
	dir := t.TempDir()
	// The tree-sitter Java grammar wraps types with angle brackets in a
	// generic_type node rather than a bare type_identifier. This means
	// "new Foo<>()", "extends Foo<T>", and "implements Foo<T>" are all
	// missed by patterns that only match (type_identifier).
	// Additionally, qualified constructors like "new Outer.Inner()" use
	// scoped_type_identifier instead of type_identifier.
	err := os.WriteFile(filepath.Join(dir, "Main.java"), []byte(`import com.google.gson.Gson;
import com.google.common.collect.ImmutableList;
import org.reactivestreams.Publisher;
import junit.framework.TestCase;

public class Main extends TestCase {
    public static void main(String[] args) {
        // bare constructor — should already work
        Gson gson = new Gson();

        // generic constructor with diamond — requires generic_type pattern
        ImmutableList<String> list = new ImmutableList<>();

        // generic constructor with explicit type arg
        ImmutableList<Integer> list2 = new ImmutableList<Integer>();
    }
}

// generic implements — requires generic_type pattern for super_interfaces
class MyPublisher implements Publisher<String> {
    public void subscribe(Object subscriber) {}
}

// generic extends — requires generic_type pattern for superclass
class MyList extends ImmutableList<String> {
}
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:maven/com.google.code.gson/gson@2.10":             {"com.google.gson"},
		"pkg:maven/com.google.guava/guava@33.0":                {"com.google.common"},
		"pkg:maven/org.reactivestreams/reactive-streams@1.0.4": {"org.reactivestreams"},
		"pkg:maven/junit/junit@4.13.2":                         {"junit.framework"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		purl        string
		wantImports int
		wantCalls   int
		wantBreadth int
	}{
		{
			// bare "new Gson()" — already works with existing type_identifier pattern
			name:        "bare constructor new Gson()",
			purl:        "pkg:maven/com.google.code.gson/gson@2.10",
			wantImports: 1,
			wantCalls:   1,
			wantBreadth: 1,
		},
		{
			// "new ImmutableList<>()" and "new ImmutableList<Integer>()" are generic_type,
			// plus "extends ImmutableList<String>" is also generic_type in superclass
			name:        "generic constructors and generic extends",
			purl:        "pkg:maven/com.google.guava/guava@33.0",
			wantImports: 1,
			wantCalls:   3,
			wantBreadth: 1,
		},
		{
			// "implements Publisher<String>" uses generic_type in super_interfaces
			name:        "generic implements Publisher<String>",
			purl:        "pkg:maven/org.reactivestreams/reactive-streams@1.0.4",
			wantImports: 1,
			wantCalls:   1,
			wantBreadth: 1,
		},
		{
			// "extends TestCase" is a bare type_identifier — should already work
			name:        "bare extends TestCase (baseline)",
			purl:        "pkg:maven/junit/junit@4.13.2",
			wantImports: 1,
			wantCalls:   1,
			wantBreadth: 1,
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
			if ca.CallSiteCount != tt.wantCalls {
				t.Errorf("CallSiteCount = %d, want %d", ca.CallSiteCount, tt.wantCalls)
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

func TestAnalyzer_JavaScopedConstructor(t *testing.T) {
	dir := t.TempDir()
	// Qualified constructors like "new ImmutableList.Builder()" use
	// scoped_type_identifier in tree-sitter rather than bare type_identifier.
	// Without a pattern for scoped_type_identifier, these are missed.
	// This test covers both generic ("new ImmutableList.Builder<>()") and
	// non-generic ("new ImmutableList.Builder()") scoped constructor forms.
	err := os.WriteFile(filepath.Join(dir, "Main.java"), []byte(`import com.google.common.collect.ImmutableList;
import java.util.Map;

public class Main {
    public static void main(String[] args) {
        // Non-generic scoped constructor — matched by scoped_type_identifier pattern
        ImmutableList.Builder builder = new ImmutableList.Builder();

        // Generic scoped constructor — matched by generic_type + scoped_type_identifier pattern
        ImmutableList.Builder<String> typedBuilder = new ImmutableList.Builder<>();

        // Non-generic scoped constructor from a different import
        Map.Entry entry = new Map.Entry();
    }
}
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:maven/com.google.guava/guava@33.0": {"com.google.common"},
		"pkg:maven/java/jdk@17":                 {"java.util"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		purl        string
		wantImports int
		wantCalls   int
		wantBreadth int
	}{
		{
			// "new ImmutableList.Builder()" (non-generic) + "new ImmutableList.Builder<>()" (generic)
			name:        "guava scoped constructors (generic + non-generic)",
			purl:        "pkg:maven/com.google.guava/guava@33.0",
			wantImports: 1,
			wantCalls:   2,
			wantBreadth: 1,
		},
		{
			// "new Map.Entry()" — non-generic scoped constructor
			name:        "jdk non-generic scoped constructor",
			purl:        "pkg:maven/java/jdk@17",
			wantImports: 1,
			wantCalls:   1,
			wantBreadth: 1,
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
			if ca.CallSiteCount != tt.wantCalls {
				t.Errorf("CallSiteCount = %d, want %d", ca.CallSiteCount, tt.wantCalls)
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

func TestAnalyzer_JavaMethodReference(t *testing.T) {
	dir := t.TempDir()
	// Java method references (Foo::bar) produce method_reference AST nodes.
	// These should be counted as call sites when the type is from an external dependency.
	// Without a method_reference query pattern, deps used ONLY via method references
	// are undercounted (call_site_count = 0 despite active usage).
	err := os.WriteFile(filepath.Join(dir, "Main.java"), []byte(`import com.google.gson.Gson;
import com.google.common.base.Strings;
import com.google.common.collect.ImmutableList;
import org.apache.commons.lang3.StringUtils;

import java.util.List;
import java.util.Arrays;
import java.util.stream.Collectors;

public class Main {
    // Method reference to external type's static method
    List<Boolean> blanks = Arrays.asList("a", "b").stream()
        .map(StringUtils::isBlank)
        .collect(Collectors.toList());

    // Method reference to external type's instance method
    List<String> jsons = Arrays.asList("a", "b").stream()
        .map(new Gson()::toJson)
        .collect(Collectors.toList());

    // Constructor reference (Type::new)
    Gson gson = Arrays.asList("config").stream()
        .map(Gson::new)
        .findFirst().orElse(null);

    // Method reference with qualified static method
    List<Boolean> nullOrEmpty = Arrays.asList("a", null).stream()
        .map(Strings::isNullOrEmpty)
        .collect(Collectors.toList());

    // Scoped qualifier method reference: ImmutableList.Builder::add
    // The qualifier is a scoped_identifier; only the inner identifier
    // ("Builder") should be matched against aliasMap.
    java.util.function.Function<String, ImmutableList.Builder<String>> adder =
        ImmutableList.Builder::add;
}
`), 0644)
	if err != nil {
		t.Fatal(err)
	}

	analyzer := NewAnalyzer()
	importPaths := map[string][]string{
		"pkg:maven/com.google.code.gson/gson@2.10":        {"com.google.gson"},
		"pkg:maven/com.google.guava/guava@33.0":           {"com.google.common"},
		"pkg:maven/org.apache.commons/commons-lang3@3.14": {"org.apache.commons.lang3"},
	}
	result, err := analyzer.AnalyzeCoupling(context.Background(), dir, importPaths)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		purl        string
		wantImports int
		wantCalls   int
		wantBreadth int
	}{
		{
			// StringUtils::isBlank — method reference to external static method
			name:        "method reference StringUtils::isBlank",
			purl:        "pkg:maven/org.apache.commons/commons-lang3@3.14",
			wantImports: 1,
			wantCalls:   1,
			wantBreadth: 1,
		},
		{
			// "new Gson()::toJson" contains an object_creation_expression which is
			// matched by the constructor query, counting "Gson" as a call site.
			// "Gson::new" is matched by the constructor reference pattern
			// (method_reference with "new" token), recording the qualifier "Gson"
			// as the symbol — consistent with how new Foo() records "Foo".
			// Total: 2 call sites, 1 unique symbol ("Gson").
			name:        "gson constructor reference and object creation",
			purl:        "pkg:maven/com.google.code.gson/gson@2.10",
			wantImports: 1,
			wantCalls:   2,
			wantBreadth: 1,
		},
		{
			// Strings::isNullOrEmpty + ImmutableList.Builder::add — guava has
			// two imports (Strings, ImmutableList) in one file and two method
			// references: one simple (Strings::isNullOrEmpty) and one scoped
			// (ImmutableList.Builder::add) where the field_access query
			// captures "ImmutableList" for aliasMap lookup.
			// ImportFileCount = 1 because both imports are in the same file.
			name:        "guava method references (simple + scoped qualifier)",
			purl:        "pkg:maven/com.google.guava/guava@33.0",
			wantImports: 1,
			wantCalls:   2,
			wantBreadth: 2,
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
			if ca.CallSiteCount != tt.wantCalls {
				t.Errorf("CallSiteCount = %d, want %d", ca.CallSiteCount, tt.wantCalls)
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
