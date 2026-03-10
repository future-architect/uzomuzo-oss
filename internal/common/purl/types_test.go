package purl

import (
	"reflect"
	"sort"
	"testing"
)

func TestSupportedEcosystems(t *testing.T) {
	tests := []struct {
		name            string
		expectedMinSize int
		mustContain     []string
	}{
		{
			name:            "returns_supported_ecosystems",
			expectedMinSize: 8,
			mustContain: []string{
				"npm",
				"pypi",
				"maven",
				"nuget",
				"cargo",
				"golang",
				"gem",
				"github",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ecosystems := SupportedEcosystems()

			if len(ecosystems) < tt.expectedMinSize {
				t.Errorf("SupportedEcosystems() returned %d ecosystems, want at least %d", len(ecosystems), tt.expectedMinSize)
			}

			for _, mustHave := range tt.mustContain {
				found := false
				for _, ecosystem := range ecosystems {
					if ecosystem == mustHave {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("SupportedEcosystems() missing required ecosystem: %s", mustHave)
				}
			}

			// Verify no duplicates
			seen := make(map[string]bool)
			for _, ecosystem := range ecosystems {
				if seen[ecosystem] {
					t.Errorf("SupportedEcosystems() contains duplicate: %s", ecosystem)
				}
				seen[ecosystem] = true
			}
		})
	}
}

func TestIsEcosystemSupported(t *testing.T) {
	tests := []struct {
		name      string
		ecosystem string
		expected  bool
	}{
		{
			name:      "npm_ecosystem_supported",
			ecosystem: "npm",
			expected:  true,
		},
		{
			name:      "pypi_ecosystem_supported",
			ecosystem: "pypi",
			expected:  true,
		},
		{
			name:      "maven_ecosystem_supported",
			ecosystem: "maven",
			expected:  true,
		},
		{
			name:      "nuget_ecosystem_supported",
			ecosystem: "nuget",
			expected:  true,
		},
		{
			name:      "cargo_ecosystem_supported",
			ecosystem: "cargo",
			expected:  true,
		},
		{
			name:      "golang_ecosystem_supported",
			ecosystem: "golang",
			expected:  true,
		},
		{
			name:      "gem_ecosystem_supported",
			ecosystem: "gem",
			expected:  true,
		},
		{
			name:      "github_ecosystem_supported",
			ecosystem: "github",
			expected:  true,
		},
		{
			name:      "unsupported_ecosystem",
			ecosystem: "unsupported",
			expected:  false,
		},
		{
			name:      "empty_ecosystem",
			ecosystem: "",
			expected:  false,
		},
		{
			name:      "case_sensitive_uppercase",
			ecosystem: "NPM",
			expected:  false,
		},
		{
			name:      "case_sensitive_mixed",
			ecosystem: "Npm",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsEcosystemSupported(tt.ecosystem)
			if result != tt.expected {
				t.Errorf("IsEcosystemSupported(%q) = %v, want %v", tt.ecosystem, result, tt.expected)
			}
		})
	}
}

func TestPackageManagerMapping(t *testing.T) {
	tests := []struct {
		name              string
		packageManager    string
		expectedEcosystem string
		shouldExist       bool
	}{
		{
			name:              "npm_package_manager",
			packageManager:    "NPM",
			expectedEcosystem: "npm",
			shouldExist:       true,
		},
		{
			name:              "pip_package_manager",
			packageManager:    "PIP",
			expectedEcosystem: "pypi",
			shouldExist:       true,
		},
		{
			name:              "pypi_package_manager",
			packageManager:    "PYPI",
			expectedEcosystem: "pypi",
			shouldExist:       true,
		},
		{
			name:              "maven_package_manager",
			packageManager:    "MAVEN",
			expectedEcosystem: "maven",
			shouldExist:       true,
		},
		{
			name:              "nuget_package_manager",
			packageManager:    "NUGET",
			expectedEcosystem: "nuget",
			shouldExist:       true,
		},
		{
			name:              "cargo_package_manager",
			packageManager:    "CARGO",
			expectedEcosystem: "cargo",
			shouldExist:       true,
		},
		{
			name:              "rust_package_manager",
			packageManager:    "RUST",
			expectedEcosystem: "cargo",
			shouldExist:       true,
		},
		{
			name:              "go_package_manager",
			packageManager:    "GO",
			expectedEcosystem: "golang",
			shouldExist:       true,
		},
		{
			name:              "golang_package_manager",
			packageManager:    "GOLANG",
			expectedEcosystem: "golang",
			shouldExist:       true,
		},
		{
			name:              "rubygems_package_manager",
			packageManager:    "RUBYGEMS",
			expectedEcosystem: "gem",
			shouldExist:       true,
		},
		{
			name:              "gem_package_manager",
			packageManager:    "GEM",
			expectedEcosystem: "gem",
			shouldExist:       true,
		},
		{
			name:              "github_package_manager",
			packageManager:    "GITHUB",
			expectedEcosystem: "github",
			shouldExist:       true,
		},
		{
			name:              "unsupported_package_manager",
			packageManager:    "UNSUPPORTED",
			expectedEcosystem: "",
			shouldExist:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ecosystem, exists := PackageManagerMapping[tt.packageManager]

			if exists != tt.shouldExist {
				t.Errorf("PackageManagerMapping[%q] exists = %v, want %v", tt.packageManager, exists, tt.shouldExist)
			}

			if exists && ecosystem != tt.expectedEcosystem {
				t.Errorf("PackageManagerMapping[%q] = %q, want %q", tt.packageManager, ecosystem, tt.expectedEcosystem)
			}
		})
	}
}

func TestMapPackageManagerToEcosystem(t *testing.T) {
	tests := []struct {
		name              string
		packageManager    string
		expectedEcosystem string
	}{
		{
			name:              "uppercase_npm",
			packageManager:    "NPM",
			expectedEcosystem: "npm",
		},
		{
			name:              "lowercase_npm",
			packageManager:    "npm",
			expectedEcosystem: "npm",
		},
		{
			name:              "mixed_case_npm",
			packageManager:    "Npm",
			expectedEcosystem: "npm",
		},
		{
			name:              "pip_to_pypi",
			packageManager:    "pip",
			expectedEcosystem: "pypi",
		},
		{
			name:              "uppercase_pip_to_pypi",
			packageManager:    "PIP",
			expectedEcosystem: "pypi",
		},
		{
			name:              "pypi_direct",
			packageManager:    "pypi",
			expectedEcosystem: "pypi",
		},
		{
			name:              "maven_mapping",
			packageManager:    "maven",
			expectedEcosystem: "maven",
		},
		{
			name:              "rust_to_cargo",
			packageManager:    "rust",
			expectedEcosystem: "cargo",
		},
		{
			name:              "go_to_golang",
			packageManager:    "go",
			expectedEcosystem: "golang",
		},
		{
			name:              "rubygems_to_gem",
			packageManager:    "rubygems",
			expectedEcosystem: "gem",
		},
		{
			name:              "unsupported_package_manager",
			packageManager:    "unsupported",
			expectedEcosystem: "",
		},
		{
			name:              "empty_package_manager",
			packageManager:    "",
			expectedEcosystem: "",
		},
		{
			name:              "unknown_but_supported_ecosystem",
			packageManager:    "cargo",
			expectedEcosystem: "cargo",
		},
		{
			name:              "unknown_and_unsupported",
			packageManager:    "completely-unknown",
			expectedEcosystem: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MapPackageManagerToEcosystem(tt.packageManager)
			if result != tt.expectedEcosystem {
				t.Errorf("MapPackageManagerToEcosystem(%q) = %q, want %q", tt.packageManager, result, tt.expectedEcosystem)
			}
		})
	}
}

func TestPackageManagerMapping_Completeness(t *testing.T) {
	tests := []struct {
		name        string
		testFunc    func() bool
		description string
	}{
		{
			name: "all_mapped_ecosystems_are_supported",
			testFunc: func() bool {
				for _, ecosystem := range PackageManagerMapping {
					if !IsEcosystemSupported(ecosystem) {
						return false
					}
				}
				return true
			},
			description: "All ecosystems in PackageManagerMapping should be supported",
		},
		{
			name: "mapping_covers_major_package_managers",
			testFunc: func() bool {
				requiredManagers := []string{"NPM", "PIP", "MAVEN", "CARGO", "GO", "GEM"}
				for _, manager := range requiredManagers {
					if _, exists := PackageManagerMapping[manager]; !exists {
						return false
					}
				}
				return true
			},
			description: "Mapping should cover all major package managers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.testFunc() {
				t.Errorf("Completeness test failed: %s", tt.description)
			} else {
				t.Logf("Completeness test passed: %s", tt.description)
			}
		})
	}
}

func TestEcosystemsIntegration(t *testing.T) {
	tests := []struct {
		name        string
		testFunc    func() bool
		description string
	}{
		{
			name: "all_supported_ecosystems_have_consistent_behavior",
			testFunc: func() bool {
				ecosystems := SupportedEcosystems()
				for _, ecosystem := range ecosystems {
					if !IsEcosystemSupported(ecosystem) {
						return false
					}
				}
				return true
			},
			description: "All ecosystems from SupportedEcosystems() should pass IsEcosystemSupported()",
		},
		{
			name: "mapping_and_supported_ecosystems_alignment",
			testFunc: func() bool {
				// Get all unique ecosystems from mapping
				mappingEcosystems := make(map[string]bool)
				for _, ecosystem := range PackageManagerMapping {
					mappingEcosystems[ecosystem] = true
				}

				// Check if all mapping ecosystems are in supported list
				supported := SupportedEcosystems()
				supportedMap := make(map[string]bool)
				for _, ecosystem := range supported {
					supportedMap[ecosystem] = true
				}

				for ecosystem := range mappingEcosystems {
					if !supportedMap[ecosystem] {
						return false
					}
				}
				return true
			},
			description: "All ecosystems in PackageManagerMapping should be in SupportedEcosystems()",
		},
		{
			name: "case_insensitive_mapping_works",
			testFunc: func() bool {
				testCases := []struct {
					input    string
					expected string
				}{
					{"npm", "npm"},
					{"NPM", "npm"},
					{"Npm", "npm"},
					{"pip", "pypi"},
					{"PIP", "pypi"},
					{"Pip", "pypi"},
				}

				for _, tc := range testCases {
					result := MapPackageManagerToEcosystem(tc.input)
					if result != tc.expected {
						return false
					}
				}
				return true
			},
			description: "MapPackageManagerToEcosystem should handle case variations correctly",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.testFunc() {
				t.Errorf("Integration test failed: %s", tt.description)
			} else {
				t.Logf("Integration test passed: %s", tt.description)
			}
		})
	}
}

func TestPackageManagerMappingKeys(t *testing.T) {
	tests := []struct {
		name     string
		expected []string
	}{
		{
			name: "verify_all_expected_keys_present",
			expected: []string{
				"NPM", "PIP", "PYPI", "MAVEN", "NUGET", "CARGO", "RUST",
				"GO", "GOLANG", "RUBYGEMS", "GEM", "GITHUB",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get all keys from the mapping
			var actualKeys []string
			for key := range PackageManagerMapping {
				actualKeys = append(actualKeys, key)
			}

			// Sort for comparison
			sort.Strings(actualKeys)
			sort.Strings(tt.expected)

			if !reflect.DeepEqual(actualKeys, tt.expected) {
				t.Errorf("PackageManagerMapping keys = %v, want %v", actualKeys, tt.expected)
			}
		})
	}
}
