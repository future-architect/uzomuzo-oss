package integration

import (
	"context"
	"testing"

	"github.com/future-architect/uzomuzo/internal/domain/config"
)

func TestNewIntegrationService(t *testing.T) {
	tests := []struct {
		name    string
		wantNil bool
	}{
		{
			name:    "valid_service_creation",
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create service with nil clients for basic constructor testing
			service := NewIntegrationService(nil, nil)

			if (service == nil) != tt.wantNil {
				t.Errorf("NewIntegrationService() nil = %v, want %v", service == nil, tt.wantNil)
			}

			if service != nil && service.config != nil {
				t.Error("Expected config to be nil on new service")
			}
		})
	}
}

func TestIntegrationService_WithConfig(t *testing.T) {
	tests := []struct {
		name   string
		config *config.Config
	}{
		{
			name:   "valid_config",
			config: &config.Config{App: config.AppConfig{MaxPurls: 100}},
		},
		{
			name:   "nil_config",
			config: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewIntegrationService(nil, nil, WithConfig(tt.config))

			if service.config != tt.config {
				t.Errorf("Config not set correctly, got %v, want %v", service.config, tt.config)
			}
		})
	}
}

func TestIntegrationService_AnalyzeFromPURLs_EmptyInput(t *testing.T) {
	tests := []struct {
		name      string
		purls     []string
		wantCount int
		wantErr   bool
	}{
		{
			name:      "empty_slice",
			purls:     []string{},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:      "nil_slice",
			purls:     nil,
			wantCount: 0,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewIntegrationService(nil, nil)
			ctx := context.Background()

			result, err := service.AnalyzeFromPURLs(ctx, tt.purls)

			if (err != nil) != tt.wantErr {
				t.Errorf("AnalyzeFromPURLs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(result) != tt.wantCount {
				t.Errorf("AnalyzeFromPURLs() result count = %d, want %d", len(result), tt.wantCount)
			}
		})
	}
}

func TestIntegrationService_CreatePackageFromPURL(t *testing.T) {
	tests := []struct {
		name              string
		purlStr           string
		expectedEcosystem string
		expectedVersion   string
		expectValidPURL   bool
	}{
		{
			name:              "valid_npm_purl",
			purlStr:           "pkg:npm/express@4.18.2",
			expectedEcosystem: "npm",
			expectedVersion:   "4.18.2",
			expectValidPURL:   true,
		},
		{
			name:              "valid_pypi_purl",
			purlStr:           "pkg:pypi/django@3.2.0",
			expectedEcosystem: "pypi",
			expectedVersion:   "3.2.0",
			expectValidPURL:   true,
		},
		{
			name:              "invalid_purl_format",
			purlStr:           "invalid-purl-string",
			expectedEcosystem: "",
			expectedVersion:   "",
			expectValidPURL:   false,
		},
		{
			name:              "empty_purl",
			purlStr:           "",
			expectedEcosystem: "",
			expectedVersion:   "",
			expectValidPURL:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewIntegrationService(nil, nil)

			result := service.createPackageFromPURL(tt.purlStr)

			if result == nil {
				t.Error("createPackageFromPURL returned nil")
				return
			}

			if result.PURL != tt.purlStr {
				t.Errorf("Package PURL = %v, want %v", result.PURL, tt.purlStr)
			}

			if result.Ecosystem != tt.expectedEcosystem {
				t.Errorf("Package Ecosystem = %v, want %v", result.Ecosystem, tt.expectedEcosystem)
			}

			if result.Version != tt.expectedVersion {
				t.Errorf("Package Version = %v, want %v", result.Version, tt.expectedVersion)
			}
		})
	}
}

func TestIntegrationService_ParseGitHubURL(t *testing.T) {
	tests := []struct {
		name          string
		githubURL     string
		expectedOwner string
		expectedRepo  string
		expectError   bool
	}{
		{
			name:          "https_url",
			githubURL:     "https://github.com/owner/repo",
			expectedOwner: "owner",
			expectedRepo:  "repo",
			expectError:   false,
		},
		{
			name:          "http_url",
			githubURL:     "http://github.com/owner/repo",
			expectedOwner: "owner",
			expectedRepo:  "repo",
			expectError:   false,
		},
		{
			name:          "url_without_protocol",
			githubURL:     "github.com/owner/repo",
			expectedOwner: "owner",
			expectedRepo:  "repo",
			expectError:   false,
		},
		{
			name:          "url_with_git_suffix",
			githubURL:     "github.com/owner/repo.git",
			expectedOwner: "owner",
			expectedRepo:  "repo",
			expectError:   false,
		},
		{
			name:        "invalid_url_format",
			githubURL:   "invalid",
			expectError: true,
		},
		{
			name:        "empty_url",
			githubURL:   "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewIntegrationService(nil, nil)

			owner, repo, err := service.parseGitHubURL(tt.githubURL)

			if (err != nil) != tt.expectError {
				t.Errorf("parseGitHubURL() error = %v, expectError %v", err, tt.expectError)
				return
			}

			if !tt.expectError {
				if owner != tt.expectedOwner {
					t.Errorf("parseGitHubURL() owner = %v, want %v", owner, tt.expectedOwner)
				}
				if repo != tt.expectedRepo {
					t.Errorf("parseGitHubURL() repo = %v, want %v", repo, tt.expectedRepo)
				}
			}
		})
	}
}

func TestIntegrationService_GenerateVersionedPURL(t *testing.T) {
	tests := []struct {
		name     string
		basePURL string
		version  string
		expected string
	}{
		{
			name:     "add_version_to_unversioned_purl",
			basePURL: "pkg:npm/express",
			version:  "4.18.2",
			expected: "pkg:npm/express@4.18.2",
		},
		{
			name:     "replace_existing_version",
			basePURL: "pkg:npm/express@4.0.0",
			version:  "4.18.2",
			expected: "pkg:npm/express@4.18.2",
		},
		{
			name:     "empty_version",
			basePURL: "pkg:npm/express",
			version:  "",
			expected: "pkg:npm/express@",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewIntegrationService(nil, nil)

			result := service.generateVersionedPURL(tt.basePURL, tt.version)

			if result != tt.expected {
				t.Errorf("generateVersionedPURL() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestIntegrationService_AnalyzeFromGitHubURLs_EmptyInput(t *testing.T) {
	tests := []struct {
		name       string
		githubURLs []string
		wantCount  int
		wantErr    bool
	}{
		{
			name:       "empty_slice",
			githubURLs: []string{},
			wantCount:  0,
			wantErr:    false,
		},
		{
			name:       "nil_slice",
			githubURLs: nil,
			wantCount:  0,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewIntegrationService(nil, nil)
			ctx := context.Background()

			result, err := service.AnalyzeFromGitHubURLs(ctx, tt.githubURLs)

			if (err != nil) != tt.wantErr {
				t.Errorf("AnalyzeFromGitHubURLs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(result) != tt.wantCount {
				t.Errorf("AnalyzeFromGitHubURLs() result count = %d, want %d", len(result), tt.wantCount)
			}
		})
	}
}

func TestIntegrationService_MapPackageManagerToEcosystem(t *testing.T) {
	tests := []struct {
		name              string
		packageManager    string
		expectedEcosystem string
	}{
		{
			name:              "npm_package_manager",
			packageManager:    "NPM",
			expectedEcosystem: "npm",
		},
		{
			name:              "pip_package_manager",
			packageManager:    "PIP",
			expectedEcosystem: "pypi",
		},
		{
			name:              "unknown_package_manager",
			packageManager:    "UNKNOWN",
			expectedEcosystem: "",
		},
		{
			name:              "empty_package_manager",
			packageManager:    "",
			expectedEcosystem: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewIntegrationService(nil, nil)

			result := service.mapPackageManagerToEcosystem(tt.packageManager)

			if result != tt.expectedEcosystem {
				t.Errorf("mapPackageManagerToEcosystem() = %v, want %v", result, tt.expectedEcosystem)
			}
		})
	}
}

func TestIntegrationService_GeneratePURLForEcosystem(t *testing.T) {
	tests := []struct {
		name      string
		ecosystem string
		owner     string
		repo      string
		expected  string
	}{
		{
			name:      "npm_ecosystem",
			ecosystem: "npm",
			owner:     "owner",
			repo:      "repo",
			expected:  "pkg:npm/repo",
		},
		{
			name:      "pypi_ecosystem",
			ecosystem: "pypi",
			owner:     "owner",
			repo:      "REPO",
			expected:  "pkg:pypi/repo",
		},
		{
			name:      "golang_ecosystem",
			ecosystem: "golang",
			owner:     "owner",
			repo:      "repo",
			expected:  "pkg:golang/github.com/owner/repo",
		},
		{
			name:      "unknown_ecosystem",
			ecosystem: "unknown",
			owner:     "owner",
			repo:      "repo",
			expected:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewIntegrationService(nil, nil)

			result := service.generatePURLForEcosystem(tt.ecosystem, tt.owner, tt.repo)

			if result != tt.expected {
				t.Errorf("generatePURLForEcosystem() = %v, want %v", result, tt.expected)
			}
		})
	}
}
