package common

import (
	"testing"

	domain "github.com/future-architect/uzomuzo/internal/domain/analysis"
)

func TestNewResultProcessor(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "create_new_result_processor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			processor := NewResultProcessor()

			if processor == nil {
				t.Error("NewResultProcessor() returned nil")
			}
		})
	}
}

func TestResultProcessor_FilterPackageTypes(t *testing.T) {
	tests := []struct {
		name               string
		purls              []string
		expectedAllowed    []string
		expectedNotAllowed []string
	}{
		{
			name: "mixed_supported_and_unsupported",
			purls: []string{
				"pkg:npm/lodash@4.17.21",
				"pkg:unsupported/test@1.0.0",
				"pkg:pypi/requests@2.25.1",
				"pkg:invalid-format",
			},
			expectedAllowed: []string{
				"pkg:npm/lodash@4.17.21",
				"pkg:pypi/requests@2.25.1",
			},
			expectedNotAllowed: []string{
				"pkg:unsupported/test@1.0.0",
				"pkg:invalid-format",
			},
		},
		{
			name: "all_supported_packages",
			purls: []string{
				"pkg:npm/lodash@4.17.21",
				"pkg:pypi/requests@2.25.1",
				"pkg:maven/org.springframework/spring-core@5.3.8",
				"pkg:cargo/serde@1.0.136",
			},
			expectedAllowed: []string{
				"pkg:npm/lodash@4.17.21",
				"pkg:pypi/requests@2.25.1",
				"pkg:maven/org.springframework/spring-core@5.3.8",
				"pkg:cargo/serde@1.0.136",
			},
			expectedNotAllowed: nil,
		},
		{
			name: "all_unsupported_packages",
			purls: []string{
				"pkg:unsupported1/test@1.0.0",
				"pkg:unsupported2/test@2.0.0",
			},
			expectedAllowed: nil,
			expectedNotAllowed: []string{
				"pkg:unsupported1/test@1.0.0",
				"pkg:unsupported2/test@2.0.0",
			},
		},
		{
			name:               "empty_input",
			purls:              []string{},
			expectedAllowed:    nil,
			expectedNotAllowed: nil,
		},
		{
			name:               "nil_input",
			purls:              nil,
			expectedAllowed:    nil,
			expectedNotAllowed: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			processor := NewResultProcessor()
			allowed, notAllowed := processor.FilterPackageTypes(tt.purls)

			// Check allowed results
			if len(allowed) != len(tt.expectedAllowed) {
				t.Errorf("Expected %d allowed PURLs, got %d", len(tt.expectedAllowed), len(allowed))
			} else {
				for i, expected := range tt.expectedAllowed {
					if allowed[i] != expected {
						t.Errorf("Expected allowed[%d] = %q, got %q", i, expected, allowed[i])
					}
				}
			}

			// Check not allowed results
			if len(notAllowed) != len(tt.expectedNotAllowed) {
				t.Errorf("Expected %d not allowed PURLs, got %d", len(tt.expectedNotAllowed), len(notAllowed))
			} else {
				for i, expected := range tt.expectedNotAllowed {
					if notAllowed[i] != expected {
						t.Errorf("Expected notAllowed[%d] = %q, got %q", i, expected, notAllowed[i])
					}
				}
			}
		})
	}
}

func TestColorizeResult(t *testing.T) {
	tests := []struct {
		name     string
		result   string
		expected string
	}{
		{
			name:     "active_label",
			result:   string(domain.LabelActive),
			expected: "🟢 " + string(domain.LabelActive),
		},
		{
			name:     "stalled_label",
			result:   string(domain.LabelStalled),
			expected: "⚪ " + string(domain.LabelStalled),
		},
		{
			name:     "legacy_safe_label",
			result:   string(domain.LabelLegacySafe),
			expected: "🔵 " + string(domain.LabelLegacySafe),
		},
		{
			name:     "eol_confirmed_label",
			result:   string(domain.LabelEOLConfirmed),
			expected: "🔴 " + string(domain.LabelEOLConfirmed),
		},
		{
			name:     "eol_effective_label",
			result:   string(domain.LabelEOLEffective),
			expected: "🛑 " + string(domain.LabelEOLEffective),
		},
		{
			name:     "eol_planned_label",
			result:   string(domain.LabelEOLScheduled),
			expected: "🟠 " + string(domain.LabelEOLScheduled),
		},
		{
			name:     "review_needed_label",
			result:   string(domain.LabelReviewNeeded),
			expected: "⚠️ " + string(domain.LabelReviewNeeded),
		},
		{
			name:     "unknown_label",
			result:   "Unknown-Label",
			expected: "⚪ Unknown-Label",
		},
		{
			name:     "empty_label",
			result:   "",
			expected: "⚪ ",
		},
		{
			name:     "custom_result",
			result:   "Custom-Result",
			expected: "⚪ Custom-Result",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ColorizeResult(tt.result)
			if result != tt.expected {
				t.Errorf("ColorizeResult(%q) = %q, want %q", tt.result, result, tt.expected)
			}
		})
	}
}

func TestAllowedPackageTypes(t *testing.T) {
	tests := []struct {
		name            string
		packageType     string
		shouldBeAllowed bool
	}{
		{
			name:            "cargo_package_type",
			packageType:     "cargo",
			shouldBeAllowed: true,
		},
		{
			name:            "golang_package_type",
			packageType:     "golang",
			shouldBeAllowed: true,
		},
		{
			name:            "maven_package_type",
			packageType:     "maven",
			shouldBeAllowed: true,
		},
		{
			name:            "npm_package_type",
			packageType:     "npm",
			shouldBeAllowed: true,
		},
		{
			name:            "nuget_package_type",
			packageType:     "nuget",
			shouldBeAllowed: true,
		},
		{
			name:            "pypi_package_type",
			packageType:     "pypi",
			shouldBeAllowed: true,
		},
		{
			name:            "gem_package_type",
			packageType:     "gem",
			shouldBeAllowed: true,
		},
		{
			name:            "github_package_type",
			packageType:     "github",
			shouldBeAllowed: true,
		},
		{
			name:            "unsupported_package_type",
			packageType:     "unsupported",
			shouldBeAllowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed := AllowedPackageTypes[tt.packageType]
			if allowed != tt.shouldBeAllowed {
				t.Errorf("AllowedPackageTypes[%q] = %v, want %v", tt.packageType, allowed, tt.shouldBeAllowed)
			}
		})
	}
}
