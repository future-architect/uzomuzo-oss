package purl

import (
	"testing"
)

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
