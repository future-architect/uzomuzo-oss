package pypi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// buildTestWheel creates an in-memory ZIP (wheel) with the given file entries.
func buildTestWheel(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("zip.Create(%q): %v", name, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("zip.Write(%q): %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip.Close: %v", err)
	}
	return buf.Bytes()
}

func TestExtractImportNamesFromWheel_TopLevelTxt(t *testing.T) {
	t.Parallel()
	wheel := buildTestWheel(t, map[string]string{
		"bs4/__init__.py":                               "",
		"beautifulsoup4-4.12.3.dist-info/METADATA":      "Name: beautifulsoup4",
		"beautifulsoup4-4.12.3.dist-info/top_level.txt": "bs4\n",
	})
	names := extractImportNamesFromWheel(wheel)
	if len(names) != 1 || names[0] != "bs4" {
		t.Fatalf("expected [bs4], got %v", names)
	}
}

func TestExtractImportNamesFromWheel_TopLevelTxt_Multiple(t *testing.T) {
	t.Parallel()
	wheel := buildTestWheel(t, map[string]string{
		"pkg-1.0.dist-info/top_level.txt": "yaml\n_yaml\n",
		"yaml/__init__.py":                "",
		"_yaml/__init__.py":               "",
	})
	names := extractImportNamesFromWheel(wheel)
	// _yaml starts with underscore but isPyIdentifierSafe allows it
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %v", names)
	}
	if names[0] != "yaml" && names[1] != "yaml" {
		t.Fatalf("expected yaml in result, got %v", names)
	}
}

func TestExtractImportNamesFromWheel_RECORD(t *testing.T) {
	t.Parallel()
	wheel := buildTestWheel(t, map[string]string{
		"mypkg/__init__.py": "",
		"mypkg/core.py":     "",
		"mypkg-1.0.dist-info/RECORD": "mypkg/__init__.py,sha256=abc,0\n" +
			"mypkg/core.py,sha256=def,100\n" +
			"mypkg-1.0.dist-info/METADATA,,\n",
	})
	names := extractImportNamesFromWheel(wheel)
	if len(names) != 1 || names[0] != "mypkg" {
		t.Fatalf("expected [mypkg], got %v", names)
	}
}

func TestExtractImportNamesFromWheel_InitPyFallback(t *testing.T) {
	t.Parallel()
	// No top_level.txt, no RECORD — only directory structure
	wheel := buildTestWheel(t, map[string]string{
		"airthings/__init__.py":               "",
		"airthings/cloud.py":                  "",
		"airthings_cloud-1.0.dist-info/WHEEL": "Wheel-Version: 1.0",
	})
	names := extractImportNamesFromWheel(wheel)
	if len(names) != 1 || names[0] != "airthings" {
		t.Fatalf("expected [airthings], got %v", names)
	}
}

func TestExtractImportNamesFromWheel_Empty(t *testing.T) {
	t.Parallel()
	// Only metadata, no Python packages
	wheel := buildTestWheel(t, map[string]string{
		"pkg-1.0.dist-info/METADATA": "Name: pkg",
		"pkg-1.0.dist-info/WHEEL":    "Wheel-Version: 1.0",
	})
	names := extractImportNamesFromWheel(wheel)
	if len(names) != 0 {
		t.Fatalf("expected empty, got %v", names)
	}
}

func TestExtractImportNamesFromWheel_InvalidZip(t *testing.T) {
	t.Parallel()
	names := extractImportNamesFromWheel([]byte("not a zip"))
	if names != nil {
		t.Fatalf("expected nil for invalid zip, got %v", names)
	}
}

func TestExtractImportNamesFromWheel_SkipsUnderscoredInitPy(t *testing.T) {
	t.Parallel()
	// parseInitPyDirs skips directories starting with underscore
	wheel := buildTestWheel(t, map[string]string{
		"_private/__init__.py":    "",
		"real_pkg/__init__.py":    "",
		"pkg-1.0.dist-info/WHEEL": "Wheel-Version: 1.0",
	})
	names := extractImportNamesFromWheel(wheel)
	if len(names) != 1 || names[0] != "real_pkg" {
		t.Fatalf("expected [real_pkg], got %v", names)
	}
}

func TestResolveImportNames_Integration(t *testing.T) {
	t.Parallel()

	wheel := buildTestWheel(t, map[string]string{
		"bs4/__init__.py":                               "",
		"beautifulsoup4-4.12.3.dist-info/METADATA":      "Name: beautifulsoup4",
		"beautifulsoup4-4.12.3.dist-info/top_level.txt": "bs4\n",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pypi/beautifulsoup4/json":
			resp := map[string]interface{}{
				"urls": []map[string]interface{}{
					{
						"filename":    "beautifulsoup4-4.12.3.tar.gz",
						"packagetype": "sdist",
						"size":        500000,
						"url":         fmt.Sprintf("%s/files/beautifulsoup4-4.12.3.tar.gz", r.Host),
					},
					{
						"filename":    "beautifulsoup4-4.12.3-py3-none-any.whl",
						"packagetype": "bdist_wheel",
						"size":        int64(len(wheel)),
						"url":         fmt.Sprintf("http://%s/files/beautifulsoup4-4.12.3-py3-none-any.whl", r.Host),
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case "/files/beautifulsoup4-4.12.3-py3-none-any.whl":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(wheel)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL)
	c.SetCacheTTL(5 * time.Minute)

	names, err := c.ResolveImportNames(context.Background(), "beautifulsoup4")
	if err != nil {
		t.Fatalf("ResolveImportNames: %v", err)
	}
	if len(names) != 1 || names[0] != "bs4" {
		t.Fatalf("expected [bs4], got %v", names)
	}
}

func TestResolveImportNames_NoWheel(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"urls": []map[string]interface{}{
				{
					"filename":    "pkg-1.0.tar.gz",
					"packagetype": "sdist",
					"size":        100000,
					"url":         "http://example.com/pkg-1.0.tar.gz",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL)

	names, err := c.ResolveImportNames(context.Background(), "pkg")
	if err != nil {
		t.Fatalf("ResolveImportNames: %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("expected empty for sdist-only, got %v", names)
	}
}

func TestResolveImportNames_WheelTooLarge(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"urls": []map[string]interface{}{
				{
					"filename":    "big-1.0-cp311-none-linux_x86_64.whl",
					"packagetype": "bdist_wheel",
					"size":        10 << 20, // 10 MB — exceeds limit
					"url":         "http://example.com/big.whl",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL)

	names, err := c.ResolveImportNames(context.Background(), "big")
	if err != nil {
		t.Fatalf("ResolveImportNames: %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("expected empty for oversized wheel, got %v", names)
	}
}

func TestResolveImportNames_Cache(t *testing.T) {
	t.Parallel()
	var hits int32

	wheel := buildTestWheel(t, map[string]string{
		"yaml/__init__.py":                   "",
		"pyyaml-6.0.dist-info/top_level.txt": "yaml\n",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		switch r.URL.Path {
		case "/pypi/pyyaml/json":
			resp := map[string]interface{}{
				"urls": []map[string]interface{}{
					{
						"filename":    "PyYAML-6.0-py3-none-any.whl",
						"packagetype": "bdist_wheel",
						"size":        int64(len(wheel)),
						"url":         fmt.Sprintf("http://%s/files/PyYAML-6.0-py3-none-any.whl", r.Host),
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case "/files/PyYAML-6.0-py3-none-any.whl":
			_, _ = w.Write(wheel)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL)
	c.SetCacheTTL(5 * time.Minute)

	ctx := context.Background()

	// First call — should hit network.
	names1, err := c.ResolveImportNames(ctx, "pyyaml")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if len(names1) != 1 || names1[0] != "yaml" {
		t.Fatalf("first call: expected [yaml], got %v", names1)
	}
	firstHits := atomic.LoadInt32(&hits)

	// Second call — should hit cache (no new HTTP requests).
	names2, err := c.ResolveImportNames(ctx, "pyyaml")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if len(names2) != 1 || names2[0] != "yaml" {
		t.Fatalf("second call: expected [yaml], got %v", names2)
	}
	secondHits := atomic.LoadInt32(&hits)
	if secondHits != firstHits {
		t.Fatalf("cache miss: HTTP hits went from %d to %d", firstHits, secondHits)
	}
}

func TestResolveImportNames_PackageNotFound(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL)

	names, err := c.ResolveImportNames(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("ResolveImportNames: %v", err)
	}
	if len(names) != 0 {
		t.Fatalf("expected empty for 404, got %v", names)
	}
}

func TestIsPyIdentifierSafe(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "valid", in: "bs4", want: true},
		{name: "underscore_prefix", in: "_yaml", want: true},
		{name: "with_digits", in: "lib2to3", want: true},
		{name: "digit_start", in: "3scale", want: false},
		{name: "hyphen", in: "my-pkg", want: false},
		{name: "empty", in: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isPyIdentifierSafe(tt.in); got != tt.want {
				t.Errorf("isPyIdentifierSafe(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
