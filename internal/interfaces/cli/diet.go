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
	// ToolDeps is a set of module paths declared in go.mod tool directives
	// (Go 1.24+). Populated by the composition root when a Go source root
	// is provided. Tool deps are excluded from unused-dependency detection.
	ToolDeps map[string]struct{}
}

// RunDiet is the entry point for the "diet" subcommand.
func RunDiet(
	ctx context.Context,
	cfg *domaincfg.Config,
	opts DietOptions,
	graphAnalyzer dietapp.GraphAnalyzer,
	sourceAnalyzer dietapp.SourceAnalyzer,
	pypiResolver dietapp.PyPIImportResolver,
) error {
	// Validate required options
	if opts.SBOMPath == "" {
		return fmt.Errorf("--sbom is required")
	}

	// Validate --source is a directory, not a file
	if opts.SourceRoot != "" {
		info, err := os.Stat(opts.SourceRoot)
		if err != nil {
			return fmt.Errorf("--source path %q: %w", opts.SourceRoot, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("--source %q is a file, not a directory — point it to the project root used to generate the SBOM", opts.SourceRoot)
		}
	}

	// Read SBOM data
	sbomData, err := readSBOMData(opts.SBOMPath)
	if err != nil {
		return err
	}

	// Create analysis service for health signals
	analysisService := createAnalysisService(cfg)

	// Create diet service
	svc := dietapp.NewService(graphAnalyzer, sourceAnalyzer, pypiResolver, analysisService)

	// Run diet pipeline
	plan, err := svc.Run(ctx, dietapp.DietInput{
		SBOMData:   sbomData,
		SBOMPath:   opts.SBOMPath,
		SourceRoot: opts.SourceRoot,
		ToolDeps:   opts.ToolDeps,
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
