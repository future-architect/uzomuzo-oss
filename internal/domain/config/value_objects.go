// Package config defines configuration domain models and interfaces
package config

import (
	"context"
	"time"
)

// DefaultConfigValues represents the structure for holding default configuration values
// This eliminates type duplication while maintaining type safety
type DefaultConfigValues struct {
	App       AppConfig
	GitHub    GitHubConfig
	DepsDev   DepsDevConfig
	Scorecard ScorecardConfig
	Lifecycle LifecycleAssessmentConfig
	Maven     MavenConfig
}

// DefaultValues provides centralized default configuration values
// This eliminates scattered hardcoding throughout the infrastructure layer
var DefaultValues = DefaultConfigValues{
	App: AppConfig{
		LogLevel:       "info",
		TimeoutSeconds: 14400,
		SampleSize:     0,
		OutputFormat:   "csv",
		MaxPurls:       5000,
	},
	GitHub: GitHubConfig{
		BaseURL:        "https://api.github.com",
		Timeout:        45 * time.Second,
		MaxRetries:     3,
		MaxConcurrency: 20,
	},
	DepsDev: DepsDevConfig{
		BaseURL:           "https://api.deps.dev",
		Timeout:           10 * time.Second,
		MaxRetries:        3,
		BatchSize:         1000,
		RequestIntervalMS: 500,
	},
	Scorecard: ScorecardConfig{
		BaseURL:        "https://api.scorecard.dev",
		Timeout:        15 * time.Second,
		MaxRetries:     2,
		MaxConcurrency: 10,
	},
	Lifecycle: LifecycleAssessmentConfig{
		Type:                       "lifecycle",
		RecentStableWindowDays:     365,
		RecentPrereleaseWindowDays: 180,
		MaxHumanCommitGapDays:      180,
		LegacyFrozenYears:          3,
		EolInactivityDays:          730,
		MaintenanceScoreMin:        3.0,
		VulnerabilityScoreGoodMin:  8.0,
		VulnerabilityScorePoorMax:  3.0,
		MaxBotCommitRatio:          0.7,
		ResidualAdvisoryThreshold:  1,
		HighSeverityCVSSThreshold:  7.0,
		CommitActivityWindowDays:   365,
	},
	Maven: MavenConfig{
		// Empty means use client default (repo1.maven.org/maven2). Override via MAVEN_BASE_URL.
		BaseURL: "",
	},
}

// NewConfigWithDefaults creates a new config instance with default values
// This provides a clean factory method using the centralized default values
func NewConfigWithDefaults() *Config {
	return &Config{
		App:       DefaultValues.App,
		GitHub:    DefaultValues.GitHub,
		DepsDev:   DefaultValues.DepsDev,
		Scorecard: DefaultValues.Scorecard,
		Lifecycle: DefaultValues.Lifecycle,
		Maven:     DefaultValues.Maven,
	}
}

// GetDefaultApp returns a copy of the default AppConfig
func GetDefaultApp() AppConfig {
	return DefaultValues.App
}

// GetDefaultGitHub returns a copy of the default GitHubConfig
func GetDefaultGitHub() GitHubConfig {
	return DefaultValues.GitHub
}

// GetDefaultDepsDev returns a copy of the default DepsDevConfig
func GetDefaultDepsDev() DepsDevConfig {
	return DefaultValues.DepsDev
}

// GetDefaultScorecard returns a copy of the default ScorecardConfig
func GetDefaultScorecard() ScorecardConfig {
	return DefaultValues.Scorecard
}

// GetDefaultLifecycle returns lifecycle assessment defaults
func GetDefaultLifecycle() LifecycleAssessmentConfig { return DefaultValues.Lifecycle }

// Config represents the complete application configuration
type Config struct {
	App       AppConfig                 `yaml:"app" json:"app"`
	GitHub    GitHubConfig              `yaml:"github" json:"github"`
	DepsDev   DepsDevConfig             `yaml:"depsdev" json:"depsdev"`
	Scorecard ScorecardConfig           `yaml:"scorecard" json:"scorecard"`
	Lifecycle LifecycleAssessmentConfig `yaml:"lifecycle" json:"lifecycle"`
	Maven     MavenConfig               `yaml:"maven" json:"maven"`
}

// AppConfig represents general application settings
type AppConfig struct {
	LogLevel       string `yaml:"log_level" json:"log_level"`
	TimeoutSeconds int    `yaml:"timeout_seconds" json:"timeout_seconds"`
	SampleSize     int    `yaml:"sample_size" json:"sample_size"`
	OutputFormat   string `yaml:"output_format" json:"output_format"`
	MaxPurls       int    `yaml:"max_purls" json:"max_purls"`
}

// GitHubConfig represents GitHub API configuration
type GitHubConfig struct {
	Token          string        `yaml:"token" json:"token"`
	BaseURL        string        `yaml:"base_url" json:"base_url"`
	Timeout        time.Duration `yaml:"timeout" json:"timeout"`
	MaxRetries     int           `yaml:"max_retries" json:"max_retries"`
	MaxConcurrency int           `yaml:"max_concurrency" json:"max_concurrency"`
}

// DepsDevConfig represents deps.dev API configuration
type DepsDevConfig struct {
	BaseURL           string        `yaml:"base_url" json:"base_url"`
	Timeout           time.Duration `yaml:"timeout" json:"timeout"`
	MaxRetries        int           `yaml:"max_retries" json:"max_retries"`
	BatchSize         int           `yaml:"batch_size" json:"batch_size"`
	RequestIntervalMS int           `yaml:"request_interval_ms" json:"request_interval_ms"`
}

// ScorecardConfig represents scorecard.dev API configuration.
//
// Rationale: scorecard.dev returns all 18 OpenSSF Scorecard checks (deps.dev returns only 14).
// Needs to vary per deployment: base URL differs for self-hosted instances.
type ScorecardConfig struct {
	BaseURL        string        `yaml:"base_url" json:"base_url"`
	Timeout        time.Duration `yaml:"timeout" json:"timeout"`
	MaxRetries     int           `yaml:"max_retries" json:"max_retries"`
	MaxConcurrency int           `yaml:"max_concurrency" json:"max_concurrency"`
}

// MavenConfig represents Maven repository access configuration
type MavenConfig struct {
	// BaseURL is the Maven repository base (e.g., https://repo.maven.apache.org/maven2 or a regional mirror)
	BaseURL string `yaml:"base_url" json:"base_url"`
}

// LifecycleAssessmentConfig represents lifecycle assessment tuning parameters
type LifecycleAssessmentConfig struct {
	Type                       string  `yaml:"type" json:"type"`
	RecentStableWindowDays     int     `yaml:"recent_stable_window_days" json:"recent_stable_window_days"`
	RecentPrereleaseWindowDays int     `yaml:"recent_prerelease_window_days" json:"recent_prerelease_window_days"`
	MaxHumanCommitGapDays      int     `yaml:"max_human_commit_gap_days" json:"max_human_commit_gap_days"`
	LegacyFrozenYears          int     `yaml:"legacy_frozen_years" json:"legacy_frozen_years"`
	EolInactivityDays          int     `yaml:"eol_inactivity_days" json:"eol_inactivity_days"`
	MaintenanceScoreMin        float64 `yaml:"maintenance_score_min" json:"maintenance_score_min"`
	VulnerabilityScoreGoodMin  float64 `yaml:"vulnerability_score_good_min" json:"vulnerability_score_good_min"`
	VulnerabilityScorePoorMax  float64 `yaml:"vulnerability_score_poor_max" json:"vulnerability_score_poor_max"`
	MaxBotCommitRatio          float64 `yaml:"max_bot_commit_ratio" json:"max_bot_commit_ratio"`
	ResidualAdvisoryThreshold  int     `yaml:"residual_advisory_threshold" json:"residual_advisory_threshold"`
	HighSeverityCVSSThreshold  float64 `yaml:"high_severity_cvss_threshold" json:"high_severity_cvss_threshold"`
	CommitActivityWindowDays   int     `yaml:"commit_activity_window_days" json:"commit_activity_window_days"`
}

func NormalizeLifecycleConfig(c *LifecycleAssessmentConfig) {
	def := DefaultValues.Lifecycle
	if c.Type == "" {
		c.Type = def.Type
	}
	if c.RecentStableWindowDays == 0 {
		c.RecentStableWindowDays = def.RecentStableWindowDays
	}
	if c.RecentPrereleaseWindowDays == 0 {
		c.RecentPrereleaseWindowDays = def.RecentPrereleaseWindowDays
	}
	if c.MaxHumanCommitGapDays == 0 {
		c.MaxHumanCommitGapDays = def.MaxHumanCommitGapDays
	}
	if c.LegacyFrozenYears == 0 {
		c.LegacyFrozenYears = def.LegacyFrozenYears
	}
	if c.EolInactivityDays == 0 {
		c.EolInactivityDays = def.EolInactivityDays
	}
	if c.MaintenanceScoreMin == 0 {
		c.MaintenanceScoreMin = def.MaintenanceScoreMin
	}
	if c.VulnerabilityScoreGoodMin == 0 {
		c.VulnerabilityScoreGoodMin = def.VulnerabilityScoreGoodMin
	}
	if c.VulnerabilityScorePoorMax == 0 {
		c.VulnerabilityScorePoorMax = def.VulnerabilityScorePoorMax
	}
	if c.MaxBotCommitRatio == 0 {
		c.MaxBotCommitRatio = def.MaxBotCommitRatio
	}
	if c.ResidualAdvisoryThreshold == 0 {
		c.ResidualAdvisoryThreshold = def.ResidualAdvisoryThreshold
	}
	if c.HighSeverityCVSSThreshold == 0 {
		c.HighSeverityCVSSThreshold = def.HighSeverityCVSSThreshold
	}
	if c.CommitActivityWindowDays == 0 {
		c.CommitActivityWindowDays = def.CommitActivityWindowDays
	}
}

// Service defines configuration management operations
type Service interface {
	// Load loads configuration from various sources
	Load(ctx context.Context) (*Config, error)

	// Validate validates configuration values
	Validate(ctx context.Context, config *Config) error

	// GetConfig returns current configuration
	GetConfig() *Config

	// Reload reloads configuration from sources
	Reload(ctx context.Context) error
}

// Source defines a configuration source
type Source interface {
	// Name returns the source name
	Name() string

	// Load loads configuration data from this source
	Load(ctx context.Context) (map[string]interface{}, error)

	// Priority returns the priority of this source (higher = more important)
	Priority() int
}

// Validator defines configuration validation interface
type Validator interface {
	// Validate validates a configuration section
	Validate(ctx context.Context, config interface{}) error
}
