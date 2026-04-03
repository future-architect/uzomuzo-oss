package purl

import (
	"errors"
	"testing"
)

func TestNewParser(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "create_new_parser",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser()
			if parser == nil {
				t.Error("NewParser() returned nil")
			}
		})
	}
}

func TestParser_Parse(t *testing.T) {
	tests := []struct {
		name        string
		purl        string
		expectError bool
		wantRaw     string
	}{
		{
			name:        "valid_npm_purl",
			purl:        "pkg:npm/lodash@4.17.21",
			expectError: false,
			wantRaw:     "pkg:npm/lodash@4.17.21",
		},
		{
			name:        "valid_pypi_purl",
			purl:        "pkg:pypi/requests@2.25.1",
			expectError: false,
			wantRaw:     "pkg:pypi/requests@2.25.1",
		},
		{
			name:        "valid_maven_purl",
			purl:        "pkg:maven/org.springframework/spring-core@5.3.8",
			expectError: false,
			wantRaw:     "pkg:maven/org.springframework/spring-core@5.3.8",
		},
		{
			name:        "valid_cargo_purl",
			purl:        "pkg:cargo/serde@1.0.136",
			expectError: false,
			wantRaw:     "pkg:cargo/serde@1.0.136",
		},
		{
			name:        "valid_golang_purl",
			purl:        "pkg:golang/github.com/gorilla/mux@v1.8.0",
			expectError: false,
			wantRaw:     "pkg:golang/github.com/gorilla/mux@v1.8.0",
		},
		{
			name:        "invalid_purl_format",
			purl:        "not-a-purl",
			expectError: true,
			wantRaw:     "not-a-purl",
		},
		{
			name:        "empty_purl",
			purl:        "",
			expectError: true,
			wantRaw:     "",
		},
		{
			// packageurl-go v0.1.5+ accepts this as valid: name="" is parsed
			// without error. Previous versions (v0.1.3) rejected it.
			name:        "malformed_purl_missing_name",
			purl:        "pkg:npm/@4.17.21",
			expectError: false,
			wantRaw:     "pkg:npm/@4.17.21",
		},
		{
			name:        "purl_with_namespace",
			purl:        "pkg:npm/@types/node@16.11.7",
			expectError: false,
			wantRaw:     "pkg:npm/@types/node@16.11.7",
		},
		{
			name:        "purl_without_version",
			purl:        "pkg:npm/lodash",
			expectError: false,
			wantRaw:     "pkg:npm/lodash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser()
			result, err := parser.Parse(tt.purl)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
					return
				}

				// Verify it's a ParseError
				var parseErr *ParseError
				if !errors.As(err, &parseErr) {
					t.Errorf("Expected ParseError, got %T", err)
					return
				}

				if parseErr.PURL != tt.purl {
					t.Errorf("ParseError.PURL = %q, want %q", parseErr.PURL, tt.purl)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
					return
				}
			}

			// Always check that Raw field is set correctly (even on error)
			if result == nil {
				t.Error("Parse() returned nil result")
				return
			}

			if result.Raw != tt.wantRaw {
				t.Errorf("ParsedPURL.Raw = %q, want %q", result.Raw, tt.wantRaw)
			}
		})
	}
}

func TestParsedPURL_GetEcosystem(t *testing.T) {
	tests := []struct {
		name              string
		purl              string
		expectedEcosystem string
	}{
		{
			name:              "npm_ecosystem",
			purl:              "pkg:npm/lodash@4.17.21",
			expectedEcosystem: "npm",
		},
		{
			name:              "pypi_ecosystem",
			purl:              "pkg:pypi/requests@2.25.1",
			expectedEcosystem: "pypi",
		},
		{
			name:              "maven_ecosystem",
			purl:              "pkg:maven/org.springframework/spring-core@5.3.8",
			expectedEcosystem: "maven",
		},
		{
			name:              "cargo_ecosystem",
			purl:              "pkg:cargo/serde@1.0.136",
			expectedEcosystem: "cargo",
		},
		{
			name:              "golang_ecosystem",
			purl:              "pkg:golang/github.com/gorilla/mux@v1.8.0",
			expectedEcosystem: "golang",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser()
			parsed, err := parser.Parse(tt.purl)
			if err != nil {
				t.Fatalf("Failed to parse PURL: %v", err)
			}

			ecosystem := parsed.GetEcosystem()
			if ecosystem != tt.expectedEcosystem {
				t.Errorf("GetEcosystem() = %q, want %q", ecosystem, tt.expectedEcosystem)
			}
		})
	}
}

func TestParsedPURL_GetPackageName(t *testing.T) {
	tests := []struct {
		name                string
		purl                string
		expectedPackageName string
	}{
		{
			name:                "simple_package_name",
			purl:                "pkg:npm/lodash@4.17.21",
			expectedPackageName: "lodash",
		},
		{
			name:                "package_with_namespace",
			purl:                "pkg:npm/@types/node@16.11.7",
			expectedPackageName: "node",
		},
		{
			name:                "maven_package_name",
			purl:                "pkg:maven/org.springframework/spring-core@5.3.8",
			expectedPackageName: "spring-core",
		},
		{
			name:                "golang_package_name",
			purl:                "pkg:golang/github.com/gorilla/mux@v1.8.0",
			expectedPackageName: "github.com%2Fgorilla%2Fmux",
		},
		{
			name:                "package_without_version",
			purl:                "pkg:npm/lodash",
			expectedPackageName: "lodash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser()
			parsed, err := parser.Parse(tt.purl)
			if err != nil {
				t.Fatalf("Failed to parse PURL: %v", err)
			}

			packageName := parsed.GetPackageName()
			if packageName != tt.expectedPackageName {
				t.Errorf("GetPackageName() = %q, want %q", packageName, tt.expectedPackageName)
			}
		})
	}
}

func TestParsedPURL_ComponentMethods(t *testing.T) {
	tests := []struct {
		name              string
		purl              string
		expectedNamespace string
		expectedName      string
		expectedVersion   string
	}{
		{
			name:              "npm_with_namespace",
			purl:              "pkg:npm/@types/node@16.11.7",
			expectedNamespace: "@types",
			expectedName:      "node",
			expectedVersion:   "16.11.7",
		},
		{
			name:              "maven_with_namespace",
			purl:              "pkg:maven/org.springframework/spring-core@5.3.8",
			expectedNamespace: "org.springframework",
			expectedName:      "spring-core",
			expectedVersion:   "5.3.8",
		},
		{
			name:              "simple_npm_package",
			purl:              "pkg:npm/lodash@4.17.21",
			expectedNamespace: "",
			expectedName:      "lodash",
			expectedVersion:   "4.17.21",
		},
		{
			name:              "package_without_version",
			purl:              "pkg:npm/lodash",
			expectedNamespace: "",
			expectedName:      "lodash",
			expectedVersion:   "",
		},
		{
			name:              "golang_package",
			purl:              "pkg:golang/github.com/gorilla/mux@v1.8.0",
			expectedNamespace: "",
			expectedName:      "github.com/gorilla/mux",
			expectedVersion:   "v1.8.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser()
			parsed, err := parser.Parse(tt.purl)
			if err != nil {
				t.Fatalf("Failed to parse PURL: %v", err)
			}

			if parsed.Namespace() != tt.expectedNamespace {
				t.Errorf("Namespace() = %q, want %q", parsed.Namespace(), tt.expectedNamespace)
			}

			if parsed.Name() != tt.expectedName {
				t.Errorf("Name() = %q, want %q", parsed.Name(), tt.expectedName)
			}

			if parsed.Version() != tt.expectedVersion {
				t.Errorf("Version() = %q, want %q", parsed.Version(), tt.expectedVersion)
			}
		})
	}
}

func TestIsStableVersion(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		expected bool
	}{
		{
			name:     "stable_semantic_version",
			version:  "1.2.3",
			expected: true,
		},
		{
			name:     "stable_version_with_patch",
			version:  "2.15.1",
			expected: true,
		},
		{
			name:     "alpha_version",
			version:  "1.0.0-alpha.1",
			expected: false,
		},
		{
			name:     "beta_version",
			version:  "2.0.0-beta.3",
			expected: false,
		},
		{
			name:     "release_candidate_version",
			version:  "1.5.0-rc.2",
			expected: false,
		},
		{
			name:     "development_version",
			version:  "1.0.0-dev",
			expected: false,
		},
		{
			name:     "snapshot_version",
			version:  "1.0.0-SNAPSHOT",
			expected: false,
		},
		{
			name:     "prerelease_version",
			version:  "1.0.0-pre.1",
			expected: false,
		},
		{
			name:     "preview_version",
			version:  "1.0.0-preview.2",
			expected: false,
		},
		{
			name:     "empty_version",
			version:  "",
			expected: false,
		},
		{
			name:     "version_with_alpha_in_middle",
			version:  "1.0.alpha.2",
			expected: false,
		},
		{
			name:     "version_with_beta_uppercase",
			version:  "1.0.0-BETA.1",
			expected: false,
		},
		{
			name:     "stable_version_with_build_metadata",
			version:  "1.0.0+20210701",
			expected: true,
		},
		{
			name:     "stable_go_version",
			version:  "v1.8.0",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsStableVersion(tt.version)
			if result != tt.expected {
				t.Errorf("IsStableVersion(%q) = %v, want %v", tt.version, result, tt.expected)
			}
		})
	}
}

func TestParseError(t *testing.T) {
	tests := []struct {
		name          string
		message       string
		purl          string
		expectedError string
	}{
		{
			name:          "parse_error_with_message_and_purl",
			message:       "invalid format",
			purl:          "not-a-purl",
			expectedError: "invalid format: not-a-purl",
		},
		{
			name:          "parse_error_empty_message",
			message:       "",
			purl:          "pkg:invalid",
			expectedError: ": pkg:invalid",
		},
		{
			name:          "parse_error_empty_purl",
			message:       "missing PURL",
			purl:          "",
			expectedError: "missing PURL: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewParseError(tt.message, tt.purl)

			if err.Message != tt.message {
				t.Errorf("ParseError.Message = %q, want %q", err.Message, tt.message)
			}

			if err.PURL != tt.purl {
				t.Errorf("ParseError.PURL = %q, want %q", err.PURL, tt.purl)
			}

			if err.Error() != tt.expectedError {
				t.Errorf("ParseError.Error() = %q, want %q", err.Error(), tt.expectedError)
			}
		})
	}
}

func TestParser_Integration(t *testing.T) {
	tests := []struct {
		name        string
		testFunc    func() bool
		description string
	}{
		{
			name: "parse_and_extract_all_components",
			testFunc: func() bool {
				parser := NewParser()
				parsed, err := parser.Parse("pkg:npm/@types/node@16.11.7")
				if err != nil {
					return false
				}

				return parsed.GetEcosystem() == "npm" &&
					parsed.GetPackageName() == "node" &&
					parsed.Namespace() == "@types" &&
					parsed.Name() == "node" &&
					parsed.Version() == "16.11.7" &&
					parsed.Raw == "pkg:npm/@types/node@16.11.7"
			},
			description: "Parse PURL and extract all components correctly",
		},
		{
			name: "parse_golang_purl_with_url_encoding",
			testFunc: func() bool {
				parser := NewParser()
				parsed, err := parser.Parse("pkg:golang/github.com/gorilla/mux@v1.8.0")
				if err != nil {
					return false
				}

				// golang packages with slashes should be URL encoded in GetPackageName()
				return parsed.GetEcosystem() == "golang" &&
					parsed.GetPackageName() == "github.com%2Fgorilla%2Fmux" &&
					parsed.Name() == "github.com/gorilla/mux" &&
					parsed.Version() == "v1.8.0"
			},
			description: "Golang PURL with URL encoding for package name",
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
