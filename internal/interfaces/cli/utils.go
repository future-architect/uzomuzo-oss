package cli

import (
	"github.com/future-architect/uzomuzo/internal/application"
	"github.com/future-architect/uzomuzo/internal/domain/config"
)

// createAnalysisService creates an AnalysisService instance from configuration.
// This is a shared utility function used by batch processing.
// Optional application.Option values are forwarded to the constructor.
func createAnalysisService(cfg *config.Config, opts ...application.Option) *application.AnalysisService {
	return application.NewAnalysisServiceFromConfig(cfg, opts...)
}
