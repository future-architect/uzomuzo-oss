// Package config implements configuration loading and management
package config

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	domainConfig "github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

// ConfigService implements domain config service interface
type ConfigService struct {
	config *domainConfig.Config
}

// NewConfigService creates a new configuration service instance
func NewConfigService() *ConfigService {
	return &ConfigService{}
}

// Load reads configuration from environment variables with defaults
func (s *ConfigService) Load(ctx context.Context) (*domainConfig.Config, error) {
	config := s.getDefaults()
	s.loadFromEnvironment(config)

	// Validate the configuration
	if err := s.Validate(ctx, config); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	// Store the configuration
	s.config = config
	return config, nil
}

// Validate validates the configuration
func (s *ConfigService) Validate(ctx context.Context, config *domainConfig.Config) error {
	// Token presence is validated later in configureAuthentication (batch.go)
	// where a user-visible banner is shown with actionable guidance.

	// Lifecycle assessment tuning validation
	if config.Lifecycle.MaintenanceScoreMin < 0 || config.Lifecycle.MaintenanceScoreMin > 10 {
		return common.NewValidationError("maintenance score min must be between 0 and 10").
			WithContext("maintenance_score_min", config.Lifecycle.MaintenanceScoreMin)
	}

	if config.Lifecycle.RecentStableWindowDays < 0 {
		return common.NewValidationError("recent stable window days must be non-negative").
			WithContext("recent_stable_window_days", config.Lifecycle.RecentStableWindowDays)
	}

	return nil
}

// getDefaults returns configuration with default values
func (s *ConfigService) getDefaults() *domainConfig.Config {
	return domainConfig.NewConfigWithDefaults()
}

// loadFromEnvironment loads configuration values from environment variables
func (s *ConfigService) loadFromEnvironment(config *domainConfig.Config) {
	// App configuration
	if val := os.Getenv("LOG_LEVEL"); val != "" {
		config.App.LogLevel = val
	}
	if val := os.Getenv("APP_TIMEOUT_SECONDS"); val != "" {
		if timeout, err := strconv.Atoi(val); err == nil {
			config.App.TimeoutSeconds = timeout
		}
	}
	if val := os.Getenv("APP_SAMPLE_SIZE"); val != "" {
		if size, err := strconv.Atoi(val); err == nil {
			config.App.SampleSize = size
		}
	}
	if val := os.Getenv("APP_OUTPUT_FORMAT"); val != "" {
		config.App.OutputFormat = val
	}
	if val := os.Getenv("APP_MAX_PURLS"); val != "" {
		if maxPurls, err := strconv.Atoi(val); err == nil {
			config.App.MaxPurls = maxPurls
		}
	}

	// GitHub configuration
	if val := os.Getenv("GITHUB_TOKEN"); val != "" {
		config.GitHub.Token = val
		slog.Debug("GitHub token loaded", "token_length", len(val))
	} else {
		slog.Debug("No GitHub token found in environment variables")
	}
	if val := os.Getenv("GITHUB_BASE_URL"); val != "" {
		config.GitHub.BaseURL = val
	}
	if val := os.Getenv("GITHUB_TIMEOUT"); val != "" {
		if timeout, err := time.ParseDuration(val); err == nil {
			config.GitHub.Timeout = timeout
		}
	}
	if val := os.Getenv("GITHUB_MAX_RETRIES"); val != "" {
		if retries, err := strconv.Atoi(val); err == nil {
			config.GitHub.MaxRetries = retries
		}
	}
	if val := os.Getenv("GITHUB_MAX_CONCURRENCY"); val != "" {
		if concurrency, err := strconv.Atoi(val); err == nil {
			config.GitHub.MaxConcurrency = concurrency
		}
	}

	// DepsDev configuration
	if val := os.Getenv("DEPSDEV_BASE_URL"); val != "" {
		config.DepsDev.BaseURL = val
	}

	// Maven configuration
	if val := os.Getenv("MAVEN_BASE_URL"); val != "" {
		config.Maven.BaseURL = val
	}
	if val := os.Getenv("DEPSDEV_TIMEOUT"); val != "" {
		if timeout, err := time.ParseDuration(val); err == nil {
			config.DepsDev.Timeout = timeout
		}
	}
	if val := os.Getenv("DEPSDEV_MAX_RETRIES"); val != "" {
		if retries, err := strconv.Atoi(val); err == nil {
			config.DepsDev.MaxRetries = retries
		}
	}
	if val := os.Getenv("DEPSDEV_BATCH_SIZE"); val != "" {
		if batchSize, err := strconv.Atoi(val); err == nil {
			config.DepsDev.BatchSize = batchSize
		}
	}
	if val := os.Getenv("DEPSDEV_REQUEST_INTERVAL_MS"); val != "" {
		if interval, err := strconv.Atoi(val); err == nil {
			config.DepsDev.RequestIntervalMS = interval
		}
	}

	// Lifecycle assessment configuration
	if val := os.Getenv("LIFECYCLE_ASSESS_TYPE"); val != "" {
		config.Lifecycle.Type = val
	}
	loadInt := func(env string, set func(int)) {
		if val := os.Getenv(env); val != "" {
			if v, err := strconv.Atoi(val); err == nil {
				set(v)
			}
		}
	}
	loadFloat := func(env string, set func(float64)) {
		if val := os.Getenv(env); val != "" {
			if v, err := strconv.ParseFloat(val, 64); err == nil {
				set(v)
			}
		}
	}
	loadInt("LIFECYCLE_ASSESS_RECENT_STABLE_WINDOW_DAYS", func(v int) { config.Lifecycle.RecentStableWindowDays = v })
	loadInt("LIFECYCLE_ASSESS_RECENT_PRERELEASE_WINDOW_DAYS", func(v int) { config.Lifecycle.RecentPrereleaseWindowDays = v })
	loadInt("LIFECYCLE_ASSESS_MAX_HUMAN_COMMIT_GAP_DAYS", func(v int) { config.Lifecycle.MaxHumanCommitGapDays = v })
	loadInt("LIFECYCLE_ASSESS_LEGACY_FROZEN_YEARS", func(v int) { config.Lifecycle.LegacyFrozenYears = v })
	loadInt("LIFECYCLE_ASSESS_EOL_INACTIVITY_DAYS", func(v int) { config.Lifecycle.EolInactivityDays = v })
	loadFloat("LIFECYCLE_ASSESS_MAINTENANCE_SCORE_MIN", func(v float64) { config.Lifecycle.MaintenanceScoreMin = v })
	loadFloat("LIFECYCLE_ASSESS_VULNERABILITY_SCORE_GOOD_MIN", func(v float64) { config.Lifecycle.VulnerabilityScoreGoodMin = v })
	loadFloat("LIFECYCLE_ASSESS_VULNERABILITY_SCORE_POOR_MAX", func(v float64) { config.Lifecycle.VulnerabilityScorePoorMax = v })
	loadFloat("LIFECYCLE_ASSESS_MAX_BOT_COMMIT_RATIO", func(v float64) { config.Lifecycle.MaxBotCommitRatio = v })
	loadInt("LIFECYCLE_ASSESS_RESIDUAL_ADVISORY_THRESHOLD", func(v int) { config.Lifecycle.ResidualAdvisoryThreshold = v })
	loadInt("LIFECYCLE_ASSESS_COMMIT_ACTIVITY_WINDOW_DAYS", func(v int) { config.Lifecycle.CommitActivityWindowDays = v })

	// Normalize zero-values to defaults
	domainConfig.NormalizeLifecycleConfig(&config.Lifecycle)

}

// GetConfig returns the current configuration
func (s *ConfigService) GetConfig() *domainConfig.Config {
	return s.config
}

// Reload reloads configuration from sources
func (s *ConfigService) Reload(ctx context.Context) error {
	config, err := s.Load(ctx)
	if err != nil {
		return fmt.Errorf("failed to reload configuration: %w", err)
	}
	s.config = config
	return nil
}
