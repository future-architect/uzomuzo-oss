// Main entry point for the uzomuzo-diet binary (CGo + tree-sitter).
package main

import (
	"context"
	"log/slog"
	"os"

	urfcli "github.com/urfave/cli/v3"

	"github.com/future-architect/uzomuzo-oss/internal/common/logging"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depgraph"
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
				Usage:    "Path to CycloneDX SBOM JSON (required)",
				Required: true,
			},
			&urfcli.StringFlag{
				Name:  "source",
				Usage: "Root directory for source code coupling analysis",
				Value: ".",
			},
			&urfcli.StringFlag{
				Name:    "format",
				Aliases: []string{"f"},
				Usage:   "Output format: json, table, detailed (default: table)",
			},
		},
		Action: func(ctx context.Context, cmd *urfcli.Command) error {
			opts := cli.DietOptions{
				SBOMPath:   cmd.String("sbom"),
				SourceRoot: cmd.String("source"),
				Format:     cmd.String("format"),
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

