// Main entry point for the uzomuzo-diet binary (CGo + tree-sitter).
package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	urfcli "github.com/urfave/cli/v3"

	"github.com/future-architect/uzomuzo-oss/internal/common/logging"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depgraph"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depparser/gomod"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/treesitter"
	"github.com/future-architect/uzomuzo-oss/internal/interfaces/cli"

	"github.com/joho/godotenv"
)

// version is set by goreleaser via ldflags.
var version = "dev"

func init() {
	if err := godotenv.Load(); err != nil {
		slog.Debug("No .env file found")
	}
}

func main() {
	ctx := context.Background()

	configService := config.NewConfigService()
	cfg, err := configService.Load(ctx)
	if err != nil {
		slog.Error("Failed to load configuration", "error", err)
		os.Exit(1)
	}

	logging.Initialize(cfg.App.LogLevel)

	app := &urfcli.Command{
		Name:    "uzomuzo-diet",
		Usage:   "Analyze dependency removability and produce a prioritized diet plan",
		Version: version,
		Flags: []urfcli.Flag{
			&urfcli.StringFlag{
				Name:     "sbom",
				Usage:    "Path to CycloneDX SBOM JSON, or '-' for stdin (required)",
				Required: true,
			},
			&urfcli.StringFlag{
				Name:  "source",
				Usage: "Project root for source coupling analysis (must match the SBOM target)",
				Value: ".",
			},
			&urfcli.StringFlag{
				Name:    "format",
				Aliases: []string{"f"},
				Usage:   "Output format: json, table, detailed (default: table)",
			},
		},
		Action: func(ctx context.Context, cmd *urfcli.Command) error {
			sourceRoot := cmd.String("source")
			opts := cli.DietOptions{
				SBOMPath:   cmd.String("sbom"),
				SourceRoot: sourceRoot,
				Format:     cmd.String("format"),
				ToolDeps:   detectToolDeps(sourceRoot),
			}

			graphAnalyzer := depgraph.NewAnalyzer()
			sourceAnalyzer := treesitter.NewAnalyzer()

			return cli.RunDiet(ctx, cfg, opts, graphAnalyzer, sourceAnalyzer)
		},
	}

	if err := app.Run(ctx, os.Args); err != nil {
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}

// detectToolDeps reads go.mod from the source root (if present) and returns
// the set of module paths declared in tool directives (Go 1.24+).
// Returns nil if the source root has no go.mod or if parsing fails.
func detectToolDeps(sourceRoot string) map[string]struct{} {
	if sourceRoot == "" {
		return nil
	}
	goModPath := filepath.Join(sourceRoot, "go.mod")
	data, err := os.ReadFile(goModPath)
	if err != nil {
		// No go.mod or unreadable — not a Go project; skip.
		return nil
	}
	toolPaths, err := gomod.ParseToolPaths(data)
	if err != nil {
		slog.Debug("failed to parse go.mod for tool directives", "error", err)
		return nil
	}
	if len(toolPaths) > 0 {
		slog.Debug("detected go.mod tool directives", "count", len(toolPaths))
	}
	return toolPaths
}
