package depsdev

import (
	"context"
)

// PURLDetailsProvider exposes operations used in the PURL flow (canonical entry)
// Flow: PURL
type PURLDetailsProvider interface {
	// GetDetailsForPURLs fetches detailed information for multiple PURLs
	// Flow: PURL
	GetDetailsForPURLs(ctx context.Context, purls []string) (map[string]*BatchResult, error)
}

// ReleaseInfoProvider exposes operations used in the GitHub URL flow (default version resolution)
// Flow: GitHub URL
type ReleaseInfoProvider interface {
	// GetLatestReleasesForPURLs fetches latest release information for multiple PURLs
	// Flow: GitHub URL
	GetLatestReleasesForPURLs(ctx context.Context, purls []string) (map[string]*ReleaseInfo, error)
}

// Client combines both providers; Integration layer depends on this for both flows
type Client interface {
	PURLDetailsProvider
	ReleaseInfoProvider
	// GetPackageVersionLicenses fetches license identifiers (SPDX preferred) for a single versioned PURL.
	// It returns a normalized, deduplicated, sorted slice of uppercase identifiers when available.
	GetPackageVersionLicenses(ctx context.Context, versionedPURL string) ([]string, error)
	// FetchDependentCountBatch fetches dependent counts for multiple PURLs in parallel.
	// Returns a map of canonical (versionless) PURL -> DependentsResponse.
	FetchDependentCountBatch(ctx context.Context, purls []string) map[string]*DependentsResponse
}

// Config is the configuration for depsdev clients
type Config struct {
	BaseURL        string
	TimeoutSeconds int
	MaxRetries     int
	BatchSize      int
	RateLimitDelay int
	UserAgent      string
}

// Factory is a factory for creating depsdev clients
type Factory interface {
	CreateClient(config *Config) Client
}
