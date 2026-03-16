package cli

import (
	"context"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

func TestProcessPURLInputs(t *testing.T) {
	tests := []struct {
		name               string
		purls              []string
		cfg                *config.Config
		options            ProcessingOptions
		wantSupportedCount int
		wantSkippedCount   int
		wantError          bool
	}{
		{
			name:  "empty_purls",
			purls: []string{},
			cfg: &config.Config{
				App: config.AppConfig{MaxPurls: 100},
			},
			options:            ProcessingOptions{IsDirectInput: true},
			wantSupportedCount: 0,
			wantSkippedCount:   0,
			wantError:          false,
		},
		{
			name:  "valid_purls_direct_input",
			purls: []string{"pkg:npm/react@18.0.0", "pkg:pypi/django@4.1.0"},
			cfg: &config.Config{
				App: config.AppConfig{MaxPurls: 100},
			},
			options:            ProcessingOptions{IsDirectInput: true},
			wantSupportedCount: 2,
			wantSkippedCount:   0,
			wantError:          false,
		},
		{
			name:  "ecosystem_filter_maven",
			purls: []string{"pkg:maven/org.apache.logging.log4j/log4j-core@2.14.1", "pkg:npm/react@18.2.0"},
			cfg: &config.Config{
				App: config.AppConfig{MaxPurls: 100},
			},
			options:            ProcessingOptions{IsDirectInput: true, Ecosystem: "maven"},
			wantSupportedCount: 1,
			wantSkippedCount:   0,
			wantError:          false,
		},
		{
			name:  "ecosystem_filter_unsupported",
			purls: []string{"pkg:maven/org.apache.logging.log4j/log4j-core@2.14.1"},
			cfg: &config.Config{
				App: config.AppConfig{MaxPurls: 100},
			},
			options:            ProcessingOptions{IsDirectInput: true, Ecosystem: "unknown-eco"},
			wantSupportedCount: 0,
			wantSkippedCount:   0,
			wantError:          true,
		},
		{
			name:  "purls_with_sampling",
			purls: []string{"pkg:npm/react@18.0.0", "pkg:pypi/django@4.1.0", "pkg:golang/github.com/gin-gonic/gin@1.8.0"},
			cfg: &config.Config{
				App: config.AppConfig{MaxPurls: 100},
			},
			options:            ProcessingOptions{IsDirectInput: false, SampleSize: 2},
			wantSupportedCount: 2,
			wantSkippedCount:   0,
			wantError:          false,
		},
		{
			name:  "too_many_purls_file_mode",
			purls: []string{"pkg:npm/react@18.0.0", "pkg:pypi/django@4.1.0", "pkg:golang/github.com/gin-gonic/gin@1.8.0"},
			cfg: &config.Config{
				App: config.AppConfig{MaxPurls: 2},
			},
			options:            ProcessingOptions{IsDirectInput: false},
			wantSupportedCount: 0,
			wantSkippedCount:   0,
			wantError:          true,
		},
		{
			name:  "mixed_supported_and_unsupported",
			purls: []string{"pkg:npm/react@18.0.0", "pkg:unsupported/test@1.0.0"},
			cfg: &config.Config{
				App: config.AppConfig{MaxPurls: 100},
			},
			options:            ProcessingOptions{IsDirectInput: true},
			wantSupportedCount: 1,
			wantSkippedCount:   1,
			wantError:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := processPURLInputs(tt.purls, tt.cfg, tt.options)

			if (err != nil) != tt.wantError {
				t.Errorf("processPURLInputs() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if err == nil {
				if len(result.SupportedPURLs) != tt.wantSupportedCount {
					t.Errorf("processPURLInputs() supported count = %v, want %v", len(result.SupportedPURLs), tt.wantSupportedCount)
				}

				if len(result.SkippedPURLs) != tt.wantSkippedCount {
					t.Errorf("processPURLInputs() skipped count = %v, want %v", len(result.SkippedPURLs), tt.wantSkippedCount)
				}
			}
		})
	}
}

func TestProcessGitHubURLInputs(t *testing.T) {
	tests := []struct {
		name           string
		githubURLs     []string
		cfg            *config.Config
		options        ProcessingOptions
		wantValidCount int
		wantError      bool
	}{
		{
			name:           "empty_github_urls",
			githubURLs:     []string{},
			cfg:            &config.Config{},
			options:        ProcessingOptions{IsDirectInput: true},
			wantValidCount: 0,
			wantError:      false,
		},
		{
			name:           "valid_github_urls_direct_input",
			githubURLs:     []string{"https://github.com/owner/repo1", "https://github.com/owner/repo2"},
			cfg:            &config.Config{},
			options:        ProcessingOptions{IsDirectInput: true},
			wantValidCount: 2,
			wantError:      false,
		},
		{
			name:           "github_urls_with_sampling",
			githubURLs:     []string{"https://github.com/owner/repo1", "https://github.com/owner/repo2", "https://github.com/owner/repo3"},
			cfg:            &config.Config{},
			options:        ProcessingOptions{IsDirectInput: false, SampleSize: 2},
			wantValidCount: 2,
			wantError:      false,
		},
		{
			name:           "sample_size_larger_than_urls",
			githubURLs:     []string{"https://github.com/owner/repo1"},
			cfg:            &config.Config{},
			options:        ProcessingOptions{IsDirectInput: false, SampleSize: 5},
			wantValidCount: 1,
			wantError:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := processGitHubURLInputs(tt.githubURLs, tt.cfg, tt.options)

			if (err != nil) != tt.wantError {
				t.Errorf("processGitHubURLInputs() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if err == nil {
				if len(result.ValidGitHubURLs) != tt.wantValidCount {
					t.Errorf("processGitHubURLInputs() valid count = %v, want %v", len(result.ValidGitHubURLs), tt.wantValidCount)
				}
			}
		})
	}
}

func TestConfigureAuthentication(t *testing.T) {
	tests := []struct {
		name      string
		inputs    *ProcessingInputs
		cfg       *config.Config
		options   ProcessingOptions
		wantError bool
	}{
		{
			name: "no_urls_no_token_required",
			inputs: &ProcessingInputs{
				SupportedPURLs:  []string{"pkg:npm/react@18.0.0"},
				ValidGitHubURLs: []string{},
			},
			cfg: &config.Config{
				GitHub: config.GitHubConfig{Token: ""},
			},
			options:   ProcessingOptions{IsDirectInput: true},
			wantError: false,
		},
		{
			name: "github_urls_without_token",
			inputs: &ProcessingInputs{
				SupportedPURLs:  []string{},
				ValidGitHubURLs: []string{"https://github.com/owner/repo"},
			},
			cfg: &config.Config{
				GitHub: config.GitHubConfig{Token: ""},
			},
			options:   ProcessingOptions{IsDirectInput: true},
			wantError: false, // token absence is now a warning, not an error
		},
		{
			name: "github_urls_with_token",
			inputs: &ProcessingInputs{
				SupportedPURLs:  []string{},
				ValidGitHubURLs: []string{"https://github.com/owner/repo"},
			},
			cfg: &config.Config{
				GitHub: config.GitHubConfig{Token: "valid_token"},
			},
			options:   ProcessingOptions{IsDirectInput: true},
			wantError: false,
		},
		{
			name: "purls_without_token_warning_only",
			inputs: &ProcessingInputs{
				SupportedPURLs:  []string{"pkg:npm/react@18.0.0"},
				ValidGitHubURLs: []string{},
			},
			cfg: &config.Config{
				GitHub: config.GitHubConfig{Token: ""},
			},
			options:   ProcessingOptions{IsDirectInput: true},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := configureAuthentication(tt.inputs, tt.cfg, tt.options)

			if (err != nil) != tt.wantError {
				t.Errorf("configureAuthentication() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestSetupProcessingContext(t *testing.T) {
	tests := []struct {
		name              string
		ctx               context.Context
		cfg               *config.Config
		options           ProcessingOptions
		wantContextNil    bool
		wantCancelFuncNil bool
	}{
		{
			name: "direct_input_uses_provided_context",
			ctx:  context.Background(),
			cfg:  &config.Config{},
			options: ProcessingOptions{
				IsDirectInput: true,
			},
			wantContextNil:    false,
			wantCancelFuncNil: true, // No cancel function for direct input
		},
		{
			name: "file_input_with_timeout",
			ctx:  context.Background(),
			cfg: &config.Config{
				App: config.AppConfig{
					TimeoutSeconds: 30,
				},
			},
			options: ProcessingOptions{
				IsDirectInput: false,
			},
			wantContextNil:    false,
			wantCancelFuncNil: false, // Should have cancel function
		},
		{
			name: "file_input_without_timeout",
			ctx:  context.Background(),
			cfg: &config.Config{
				App: config.AppConfig{
					TimeoutSeconds: 0,
				},
			},
			options: ProcessingOptions{
				IsDirectInput: false,
			},
			wantContextNil:    false,
			wantCancelFuncNil: true, // No timeout, no cancel function
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputs := &ProcessingInputs{}
			err := setupProcessingContext(inputs, tt.ctx, tt.cfg, tt.options)

			if err != nil {
				t.Errorf("setupProcessingContext() error = %v", err)
				return
			}

			if (inputs.ProcessingCtx == nil) != tt.wantContextNil {
				t.Errorf("setupProcessingContext() context nil = %v, want %v", inputs.ProcessingCtx == nil, tt.wantContextNil)
			}

			if (inputs.CancelFunc == nil) != tt.wantCancelFuncNil {
				t.Errorf("setupProcessingContext() cancel func nil = %v, want %v", inputs.CancelFunc == nil, tt.wantCancelFuncNil)
			}

			// Clean up cancel function if it exists
			if inputs.CancelFunc != nil {
				inputs.CancelFunc()
			}
		})
	}
}

func TestDisplayProcessingStartInfo(t *testing.T) {
	tests := []struct {
		name    string
		inputs  *ProcessingInputs
		options ProcessingOptions
	}{
		{
			name: "direct_input_no_display",
			inputs: &ProcessingInputs{
				SupportedPURLs:  []string{"pkg:npm/react@18.0.0"},
				ValidGitHubURLs: []string{"https://github.com/owner/repo"},
			},
			options: ProcessingOptions{
				IsDirectInput: true,
			},
		},
		{
			name: "file_input_with_display",
			inputs: &ProcessingInputs{
				SupportedPURLs:  []string{"pkg:npm/react@18.0.0"},
				ValidGitHubURLs: []string{"https://github.com/owner/repo"},
				SkippedPURLs:    []string{"pkg:unsupported/test@1.0.0"},
			},
			options: ProcessingOptions{
				IsDirectInput: false,
				Filename:      "test.txt",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This function primarily produces output, so we just test it doesn't panic
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("displayProcessingStartInfo() panicked: %v", r)
				}
			}()

			displayProcessingStartInfo(tt.inputs, tt.options)
		})
	}
}

func TestValidateAndPreprocessInputs_Integration(t *testing.T) {
	tests := []struct {
		name       string
		purls      []string
		githubURLs []string
		cfg        *config.Config
		options    ProcessingOptions
		wantError  bool
	}{
		{
			name:       "valid_mixed_inputs",
			purls:      []string{"pkg:npm/react@18.0.0"},
			githubURLs: []string{"https://github.com/owner/repo"},
			cfg: &config.Config{
				App:    config.AppConfig{MaxPurls: 100, TimeoutSeconds: 30},
				GitHub: config.GitHubConfig{Token: "valid_token"},
			},
			options:   ProcessingOptions{IsDirectInput: true},
			wantError: false,
		},
		{
			name:       "no_valid_inputs",
			purls:      []string{},
			githubURLs: []string{},
			cfg: &config.Config{
				App:    config.AppConfig{MaxPurls: 100},
				GitHub: config.GitHubConfig{Token: ""},
			},
			options:   ProcessingOptions{IsDirectInput: true},
			wantError: true,
		},
		{
			name:       "github_urls_without_token",
			purls:      []string{},
			githubURLs: []string{"https://github.com/owner/repo"},
			cfg: &config.Config{
				App:    config.AppConfig{MaxPurls: 100},
				GitHub: config.GitHubConfig{Token: ""},
			},
			options:   ProcessingOptions{IsDirectInput: true},
			wantError: false, // token absence is now a warning, not an error
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			inputs, err := validateAndPreprocessInputs(ctx, tt.cfg, tt.purls, tt.githubURLs, tt.options)

			if (err != nil) != tt.wantError {
				t.Errorf("validateAndPreprocessInputs() error = %v, wantError %v", err, tt.wantError)
				return
			}

			if err == nil {
				if inputs == nil {
					t.Error("validateAndPreprocessInputs() returned nil inputs without error")
				} else {
					// Clean up cancel function if it exists
					if inputs.CancelFunc != nil {
						inputs.CancelFunc()
					}
				}
			}
		})
	}
}
