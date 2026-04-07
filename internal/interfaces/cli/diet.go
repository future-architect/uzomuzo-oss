package cli

import (
	"context"
	"fmt"
	"os"

	dietapp "github.com/future-architect/uzomuzo-oss/internal/application/diet"
	domaincfg "github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

// DietOptions contains all diet-specific options parsed from CLI flags.
type DietOptions struct {
	SBOMPath   string // --sbom flag (required)
	SourceRoot string // --source flag (optional; empty = skip source analysis, CLI defaults to ".")
	Format     string // --format flag (json, table, detailed)
}

// RunDiet is the entry point for the "diet" subcommand.
func RunDiet(
	ctx context.Context,
	cfg *domaincfg.Config,
	opts DietOptions,
	graphAnalyzer dietapp.GraphAnalyzer,
	sourceAnalyzer dietapp.SourceAnalyzer,
) error {
	// Validate required options
	if opts.SBOMPath == "" {
		return fmt.Errorf("--sbom is required")
	}

	// Read SBOM data
	sbomData, err := os.ReadFile(opts.SBOMPath)
	if err != nil {
		return fmt.Errorf("failed to read SBOM file: %w", err)
	}

	// Create analysis service for health signals
	analysisService := createAnalysisService(cfg)

	// Create diet service
	svc := dietapp.NewService(graphAnalyzer, sourceAnalyzer, analysisService)

	// Run diet pipeline
	plan, err := svc.Run(ctx, dietapp.DietInput{
		SBOMData:   sbomData,
		SBOMPath:   opts.SBOMPath,
		SourceRoot: opts.SourceRoot,
	})
	if err != nil {
		return fmt.Errorf("diet analysis failed: %w", err)
	}

	// Resolve output format
	format := opts.Format
	if format == "" {
		format = "table"
	}

	// Render output
	return renderDietOutput(os.Stdout, plan, format)
}
