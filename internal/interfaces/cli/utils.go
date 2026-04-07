package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/future-architect/uzomuzo-oss/internal/application"
	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

// readSBOMData reads SBOM bytes from a file path, or from stdin when path is "-".
func readSBOMData(sbomPath string) ([]byte, error) {
	if sbomPath == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("failed to read SBOM from stdin: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(sbomPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read SBOM from '%s': %w", sbomPath, err)
	}
	return data, nil
}

// createAnalysisService creates an AnalysisService instance from configuration.
// This is a shared utility function used by batch processing.
// Optional application.Option values are forwarded to the constructor.
func createAnalysisService(cfg *config.Config, opts ...application.Option) *application.AnalysisService {
	return application.NewAnalysisServiceFromConfig(cfg, opts...)
}
