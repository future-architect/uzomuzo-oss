package cli

import (
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

func TestCreateAnalysisService(t *testing.T) {
	tests := []struct {
		name    string
		config  *config.Config
		wantNil bool
	}{
		{
			name: "valid_config",
			config: &config.Config{
				App: config.AppConfig{
					MaxPurls:       100,
					TimeoutSeconds: 30,
				},
				GitHub: config.GitHubConfig{
					Token: "test-token",
				},
			},
			wantNil: false,
		},
		{
			name:    "nil_config",
			config:  nil,
			wantNil: false, // Set to false since we handle the panic
		},
		{
			name:    "empty_config",
			config:  &config.Config{},
			wantNil: false,
		},
		{
			name: "config_with_github_token",
			config: &config.Config{
				GitHub: config.GitHubConfig{
					Token: "github-token-123",
				},
			},
			wantNil: false,
		},
		{
			name: "config_without_github_token",
			config: &config.Config{
				App: config.AppConfig{
					MaxPurls: 50,
				},
			},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Handle potential panic for nil config
			if tt.config == nil {
				defer func() {
					if r := recover(); r != nil {
						// Panic is expected for nil config
						t.Log("createAnalysisService panicked with nil config as expected")
						return
					}
					t.Error("Expected panic for nil config, but no panic occurred")
				}()
			}

			service := createAnalysisService(tt.config)

			if (service == nil) != tt.wantNil {
				t.Errorf("createAnalysisService() nil = %v, want %v", service == nil, tt.wantNil)
			}

			if service != nil {
				// Test that the service is properly initialized
				// We can't access private fields, so we verify it's not nil
				t.Log("Analysis service created successfully")
			}
		})
	}
}

func TestCreateAnalysisService_Integration(t *testing.T) {
	// Integration test to verify service can be created and used
	config := &config.Config{
		App: config.AppConfig{
			MaxPurls:       10,
			TimeoutSeconds: 5,
		},
		GitHub: config.GitHubConfig{
			Token: "test-token",
		},
	}

	service := createAnalysisService(config)

	if service == nil {
		t.Fatal("createAnalysisService returned nil")
	}

	// Test basic service functionality - this would normally require mocking
	// but for now we just verify the service was created
	t.Log("Service integration test passed - service created successfully")
}

func TestCreateAnalysisService_ConfigTypes(t *testing.T) {
	tests := []struct {
		name   string
		config *config.Config
	}{
		{
			name: "minimal_app_config",
			config: &config.Config{
				App: config.AppConfig{
					MaxPurls: 1,
				},
			},
		},
		{
			name: "full_github_config",
			config: &config.Config{
				GitHub: config.GitHubConfig{
					Token:          "full-token",
					MaxConcurrency: 20,
					MaxRetries:     5,
				},
			},
		},
		{
			name: "comprehensive_config",
			config: &config.Config{
				App: config.AppConfig{
					MaxPurls:       1000,
					TimeoutSeconds: 120,
					SampleSize:     50,
				},
				GitHub: config.GitHubConfig{
					Token:          "comprehensive-token",
					MaxConcurrency: 30,
					MaxRetries:     5,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := createAnalysisService(tt.config)

			if service == nil {
				t.Errorf("createAnalysisService() returned nil for config type: %s", tt.name)
			}
		})
	}
}
