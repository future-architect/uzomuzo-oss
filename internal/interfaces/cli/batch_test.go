package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

func TestFilterPackageTypes(t *testing.T) {
	tests := []struct {
		name               string
		purls              []string
		expectedAllowed    int
		expectedNotAllowed int
	}{
		{name: "empty_purls", purls: []string{}, expectedAllowed: 0, expectedNotAllowed: 0},
		{name: "nil_purls", purls: nil, expectedAllowed: 0, expectedNotAllowed: 0},
		{name: "mixed_supported_and_unsupported", purls: []string{
			"pkg:npm/express@4.18.2",
			"pkg:pypi/django@3.2.0",
			"pkg:unsupported/package@1.0.0",
			"pkg:golang/github.com/gin-gonic/gin@1.7.0",
		}, expectedAllowed: 3, expectedNotAllowed: 1},
		{name: "all_supported_packages", purls: []string{
			"pkg:npm/express@4.18.2",
			"pkg:pypi/django@3.2.0",
			"pkg:golang/github.com/gin-gonic/gin@1.7.0",
		}, expectedAllowed: 3, expectedNotAllowed: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed, notAllowed := filterPackageTypes(tt.purls)
			if len(allowed) != tt.expectedAllowed {
				t.Errorf("filterPackageTypes() allowed count = %d, want %d", len(allowed), tt.expectedAllowed)
			}
			if len(notAllowed) != tt.expectedNotAllowed {
				t.Errorf("filterPackageTypes() notAllowed count = %d, want %d", len(notAllowed), tt.expectedNotAllowed)
			}
			if len(allowed)+len(notAllowed) != len(tt.purls) {
				t.Errorf("filterPackageTypes() total count mismatch: allowed %d + notAllowed %d != input %d", len(allowed), len(notAllowed), len(tt.purls))
			}
		})
	}
}

func TestRandomSample(t *testing.T) {
	tests := []struct {
		name       string
		items      []string
		sampleSize int
		expected   int
	}{
		{name: "sample_size_zero", items: []string{"item1", "item2", "item3"}, sampleSize: 0, expected: 3},
		{name: "sample_size_negative", items: []string{"item1", "item2", "item3"}, sampleSize: -1, expected: 3},
		{name: "sample_size_larger_than_items", items: []string{"item1", "item2", "item3"}, sampleSize: 10, expected: 3},
		{name: "sample_size_equal_to_items", items: []string{"item1", "item2", "item3"}, sampleSize: 3, expected: 3},
		{name: "sample_size_smaller_than_items", items: []string{"item1", "item2", "item3", "item4", "item5"}, sampleSize: 2, expected: 2},
		{name: "empty_items", items: []string{}, sampleSize: 2, expected: 0},
		{name: "nil_items", items: nil, sampleSize: 2, expected: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := randomSample(tt.items, tt.sampleSize)
			if len(result) != tt.expected {
				t.Errorf("randomSample() length = %d, want %d", len(result), tt.expected)
			}
			seen := make(map[string]bool)
			for _, item := range result {
				if seen[item] {
					t.Errorf("randomSample() contains duplicate item: %s", item)
				}
				seen[item] = true
			}
			originalSet := make(map[string]bool)
			for _, item := range tt.items {
				originalSet[item] = true
			}
			for _, item := range result {
				if !originalSet[item] {
					t.Errorf("randomSample() contains item not in original: %s", item)
				}
			}
		})
	}
}

func TestCategorizeInputs(t *testing.T) {
	tests := []struct {
		name               string
		inputs             []string
		expectedPURLs      int
		expectedGitHubURLs int
	}{
		{name: "empty_inputs", inputs: []string{}, expectedPURLs: 0, expectedGitHubURLs: 0},
		{name: "nil_inputs", inputs: nil, expectedPURLs: 0, expectedGitHubURLs: 0},
		{name: "only_purls", inputs: []string{
			"pkg:npm/express@4.18.2",
			"pkg:pypi/django@3.2.0",
			"pkg:golang/github.com/gin-gonic/gin@1.7.0",
		}, expectedPURLs: 3, expectedGitHubURLs: 0},
		{name: "only_github_urls", inputs: []string{
			"https://github.com/expressjs/express",
			"https://github.com/django/django",
			"https://github.com/gin-gonic/gin",
		}, expectedPURLs: 0, expectedGitHubURLs: 3},
		{name: "mixed_purls_and_github_urls", inputs: []string{
			"pkg:npm/express@4.18.2",
			"https://github.com/expressjs/express",
			"pkg:pypi/django@3.2.0",
			"https://github.com/django/django",
		}, expectedPURLs: 2, expectedGitHubURLs: 2},
		{name: "inputs_with_empty_strings", inputs: []string{
			"pkg:npm/express@4.18.2",
			"",
			"https://github.com/expressjs/express",
			"   ",
		}, expectedPURLs: 1, expectedGitHubURLs: 1},
		{name: "inputs_with_invalid_formats", inputs: []string{
			"pkg:npm/express@4.18.2",
			"invalid-format",
			"https://github.com/expressjs/express",
			"not-a-valid-url",
		}, expectedPURLs: 1, expectedGitHubURLs: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			purls, githubURLs := categorizeInputs(tt.inputs)
			if len(purls) != tt.expectedPURLs {
				t.Errorf("categorizeInputs() PURLs count = %d, want %d", len(purls), tt.expectedPURLs)
			}
			if len(githubURLs) != tt.expectedGitHubURLs {
				t.Errorf("categorizeInputs() GitHub URLs count = %d, want %d", len(githubURLs), tt.expectedGitHubURLs)
			}
			for _, purl := range purls {
				if !strings.HasPrefix(purl, "pkg:") {
					t.Errorf("categorizeInputs() invalid PURL format: %s", purl)
				}
			}
			for _, url := range githubURLs {
				if url == "" {
					t.Errorf("categorizeInputs() empty GitHub URL")
				}
			}
		})
	}
}

func TestDisplayFunctions_NoPanic(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func()
	}{
		{name: "display_batch_analyses_full_empty", testFunc: func() {
			defer func() {
				if r := recover(); r != nil {
					t.Error("displayBatchAnalysesFull panicked with empty input")
				}
			}()
			displayBatchAnalysesFull(make(map[string]*domain.Analysis), ProcessingOptions{})
		}},
		{name: "display_batch_analyses_full_filter_empty", testFunc: func() {
			defer func() {
				if r := recover(); r != nil {
					t.Error("displayBatchAnalysesFull panicked with empty input + filter")
				}
			}()
			displayBatchAnalysesFull(make(map[string]*domain.Analysis), ProcessingOptions{OnlyReviewNeeded: true})
		}},
		{name: "display_batch_errors_empty", testFunc: func() {
			defer func() {
				if r := recover(); r != nil {
					t.Error("displayBatchErrors panicked with empty input")
				}
			}()
			displayBatchErrors(make(map[string]*domain.Analysis))
		}},
		{name: "display_batch_summary_empty", testFunc: func() {
			defer func() {
				if r := recover(); r != nil {
					t.Error("displayBatchAnalysesSummary panicked with empty input")
				}
			}()
			displayBatchAnalysesSummary(make(map[string]*domain.Analysis))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.testFunc() })
	}
}

// License display tests
func TestDisplayBatchAnalysesFull_Licenses_Same(t *testing.T) {
	analysis := &domain.Analysis{OriginalPURL: "pkg:npm/example@1.0.0", EffectivePURL: "pkg:npm/example@1.0.0", ProjectLicense: domain.ResolvedLicense{Identifier: "MIT", Raw: "MIT", IsSPDX: true, Source: domain.LicenseSourceDepsDevProjectSPDX}, RequestedVersionLicenses: []domain.ResolvedLicense{{Identifier: "MIT", Raw: "MIT", IsSPDX: true, Source: domain.LicenseSourceDepsDevVersionSPDX}}}
	analysis.EnsureCanonical()
	analyses := map[string]*domain.Analysis{"pkg:npm/example@1.0.0": analysis}
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe error: %v", err)
	}
	os.Stdout = w
	displayBatchAnalysesFull(analyses, ProcessingOptions{})
	_ = w.Close() // best-effort cleanup
	os.Stdout = oldStdout
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("copy error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "License: MIT") {
		t.Fatalf("expected collapsed single-line license, got: %s", out)
	}
	if strings.Contains(out, "Licenses:") {
		t.Fatalf("did not expect plural header in collapsed mode: %s", out)
	}
}

func TestDisplayBatchAnalysesFull_Licenses_Different(t *testing.T) {
	analysis := &domain.Analysis{OriginalPURL: "pkg:npm/example@2.0.0", EffectivePURL: "pkg:npm/example@2.0.0", ProjectLicense: domain.ResolvedLicense{Identifier: "Apache-2.0", Raw: "Apache-2.0", IsSPDX: true, Source: domain.LicenseSourceDepsDevProjectSPDX}, RequestedVersionLicenses: []domain.ResolvedLicense{{Identifier: "MIT", Raw: "MIT", IsSPDX: true, Source: domain.LicenseSourceDepsDevVersionSPDX}}}
	analysis.EnsureCanonical()
	analyses := map[string]*domain.Analysis{"pkg:npm/example@2.0.0": analysis}
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe error: %v", err)
	}
	os.Stdout = w
	displayBatchAnalysesFull(analyses, ProcessingOptions{})
	_ = w.Close() // best-effort cleanup
	os.Stdout = oldStdout
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("copy error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Licenses:") {
		t.Fatalf("expected plural header for differing licenses, got: %s", out)
	}
	if !strings.Contains(out, "Project: Apache-2.0") || !strings.Contains(out, "Requested Version: MIT") {
		t.Fatalf("expected project & requested lines, got: %s", out)
	}
	if strings.Contains(out, "License: Apache-2.0") {
		t.Fatalf("unexpected collapsed single-line for differing licenses: %s", out)
	}
}
