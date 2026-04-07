// Main entry point using Clean Architecture
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"github.com/future-architect/uzomuzo-oss/internal/common/logging"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/config"
	"github.com/future-architect/uzomuzo-oss/internal/interfaces/cli"

	"github.com/joho/godotenv"
)

// version is set by goreleaser via ldflags.
var version = "dev"

func init() {
	// Load .env file if available
	if err := godotenv.Load(); err != nil {
		slog.Debug("No .env file found")
	}
}

// main is the entry point for the uzomuzo CLI.
func main() {
	ctx := context.Background()

	// Load configuration
	configService := config.NewConfigService()
	cfg, err := configService.Load(ctx)
	if err != nil {
		slog.Error("Failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Initialize logger
	logging.Initialize(cfg.App.LogLevel)

	// Set lifecycle assessment type environment variable
	if cfg.Lifecycle.Type != "" {
		if err := os.Setenv("LIFECYCLE_ASSESS_TYPE", cfg.Lifecycle.Type); err != nil {
			slog.Warn("failed to set LIFECYCLE_ASSESS_TYPE env var", "error", err)
		}
	}

	app := buildApp(cfg)
	if err := app.Run(ctx, os.Args); err != nil {
		// ErrScanFailPolicy is a signal, not a failure — exit silently with code 1.
		if errors.Is(err, cli.ErrScanFailPolicy) {
			os.Exit(1)
		}
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}

