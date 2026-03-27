// Main entry point using Clean Architecture
package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	cli "github.com/urfave/cli/v3"

	domaincfg "github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depparser/cyclonedx"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depparser/gomod"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/spdx"
	cliiface "github.com/future-architect/uzomuzo-oss/internal/interfaces/cli"

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
	initializeLogger(cfg.App.LogLevel)

	// Set lifecycle assessment type environment variable
	if cfg.Lifecycle.Type != "" {
		os.Setenv("LIFECYCLE_ASSESS_TYPE", cfg.Lifecycle.Type)
	}

	app := buildApp(cfg)
	if err := app.Run(ctx, os.Args); err != nil {
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}

// buildApp constructs the urfave/cli v3 command tree.
func buildApp(cfg *domaincfg.Config) *cli.Command {
	// Shared ProcessingOptions populated by global flags.
	var opts cliiface.ProcessingOptions
	var lineRangeRaw string

	return &cli.Command{
		Name:    "uzomuzo",
		Usage:   "OSS dependency health checker",
		Version: version,
		UsageText: strings.Join([]string{
			"uzomuzo <purl_or_github_url> [more_inputs...] [flags]",
			"uzomuzo <purl_file> [flags]",
			"<command> | uzomuzo [flags]",
			"uzomuzo audit [--sbom <file>] [--file <go.mod>] [--format table|json|csv]",
			"uzomuzo update-spdx",
		}, "\n   "),
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:        "only-review-needed",
				Usage:       "Show only 'Review Needed' results",
				Destination: &opts.OnlyReviewNeeded,
			},
			&cli.BoolFlag{
				Name:        "only-eol",
				Usage:       "Show only 'EOL-*' results (Confirmed/Effective/Planned)",
				Destination: &opts.OnlyEOL,
			},
			&cli.StringFlag{
				Name:        "ecosystem",
				Usage:       "Filter to a single ecosystem (npm, pypi, maven, etc.)",
				Destination: &opts.Ecosystem,
			},
			&cli.IntFlag{
				Name:        "sample",
				Usage:       "Randomly sample up to N inputs (file mode only)",
				Destination: &opts.SampleSize,
			},
			&cli.StringFlag{
				Name:        "export-license-csv",
				Usage:       "Write license CSV to `path`",
				Destination: &opts.LicenseCSVPath,
			},
			&cli.StringFlag{
				Name:        "line-range",
				Usage:       "Limit to line range START:END (file mode only)",
				Destination: &lineRangeRaw,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			// Apply line-range post-parse
			if lineRangeRaw != "" {
				ls, le, err := cliiface.ParseLineRange(lineRangeRaw)
				if err != nil {
					return err
				}
				opts.LineStart = ls
				opts.LineEnd = le
			}

			args := cmd.Args().Slice()

			// No positional args: try stdin pipe
			if len(args) == 0 {
				if !isTerminal(os.Stdin) {
					return processStdin(ctx, cfg, &opts)
				}
				// Show help when invoked with no args and no pipe
				cli.ShowAppHelp(cmd)
				return cli.Exit("", 1)
			}

			first := strings.TrimSpace(args[0])
			if first == "" {
				return fmt.Errorf("input cannot be empty")
			}

			if isFilePath(first) {
				return processFileMode(ctx, cfg, first, args[1:], &opts)
			}

			// Direct mode
			opts.IsDirectInput = true
			if opts.LineStart > 0 || opts.LineEnd > 0 {
				return fmt.Errorf("--line-range is only valid in file mode")
			}
			cliiface.ProcessDirectMode(ctx, cfg, args, opts)
			return nil
		},
		Commands: []*cli.Command{
			buildAuditCommand(cfg),
			buildUpdateSPDXCommand(),
		},
	}
}

// processFileMode handles file mode from the root action.
func processFileMode(_ context.Context, cfg *domaincfg.Config, filePath string, remaining []string, opts *cliiface.ProcessingOptions) error {
	opts.IsDirectInput = false
	if opts.SampleSize == 0 {
		opts.SampleSize = cfg.App.SampleSize
	}
	// remaining positional args (e.g. legacy sample size) are ignored when --sample is set
	cliiface.ProcessFileMode(cfg, filePath, *opts)
	return nil
}

// buildAuditCommand constructs the "audit" subcommand.
func buildAuditCommand(cfg *domaincfg.Config) *cli.Command {
	var (
		sbomPath string
		filePath string
		format   string
	)
	return &cli.Command{
		Name:  "audit",
		Usage: "Audit dependencies from SBOM or go.mod for lifecycle health",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "sbom",
				Usage:       "Path to CycloneDX SBOM JSON (use '-' for stdin)",
				Destination: &sbomPath,
			},
			&cli.StringFlag{
				Name:        "file",
				Usage:       "Path to go.mod file",
				Destination: &filePath,
			},
			&cli.StringFlag{
				Name:        "format",
				Aliases:     []string{"f"},
				Value:       "table",
				Usage:       "Output format: table, json, csv",
				Destination: &format,
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			parsers := map[string]depparser.DependencyParser{
				"sbom":  &cyclonedx.Parser{},
				"gomod": &gomod.Parser{},
			}
			cliiface.RunAudit(ctx, cfg, sbomPath, filePath, format, parsers)
			return nil
		},
	}
}

// buildUpdateSPDXCommand constructs the "update-spdx" subcommand.
func buildUpdateSPDXCommand() *cli.Command {
	return &cli.Command{
		Name:  "update-spdx",
		Usage: "Refresh embedded SPDX license list",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return runUpdateSPDX(ctx)
		},
	}
}

// runUpdateSPDX downloads latest SPDX licenses.json, writes it, and regenerates tables.
func runUpdateSPDX(ctx context.Context) error {
	path := "third_party/spdx/licenses.json"
	slog.Info("fetching SPDX", "url", spdx.UpstreamURL)
	data, err := spdx.FetchLatest(ctx, nil)
	if err != nil {
		return err
	}
	ver, err := spdx.ValidatePayload(data)
	if err != nil {
		return err
	}
	if err := spdx.WriteAtomic(path, data); err != nil {
		return err
	}
	slog.Info("wrote SPDX json", "path", path, "version", ver, "bytes", len(data))
	cmd := exec.CommandContext(ctx, "go", "generate", "./internal/domain/licenses")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	slog.Info("running go generate for licenses")
	if err := cmd.Run(); err != nil {
		return err
	}
	slog.Info("SPDX update complete")
	return nil
}

// isTerminal reports whether f is connected to a terminal (not a pipe).
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return true // conservative: assume terminal on error
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// processStdin reads PURLs/GitHub URLs from stdin (one per line) and delegates to direct mode.
func processStdin(ctx context.Context, cfg *domaincfg.Config, opts *cliiface.ProcessingOptions) error {
	var lines []string
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("failed to read from stdin: %w", err)
	}
	if len(lines) == 0 {
		return fmt.Errorf("no valid input read from stdin")
	}
	slog.Info("Read inputs from stdin", "count", len(lines))
	opts.IsDirectInput = true
	cliiface.ProcessDirectMode(ctx, cfg, lines, *opts)
	return nil
}

// isFilePath determines if the input is a file path or a direct PURL/GitHub URL
func isFilePath(input string) bool {
	// Check if it's a PURL
	if strings.HasPrefix(input, "pkg:") {
		return false
	}

	// Check if it's a GitHub URL
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		return false
	}

	// Check if it's a GitHub shorthand (github.com/owner/repo)
	if strings.HasPrefix(input, "github.com/") {
		return false
	}

	// Check if file exists
	if _, err := os.Stat(input); err == nil {
		return true
	}

	// If it doesn't exist as a file but looks like a path, treat as file
	return strings.Contains(input, "/") || strings.Contains(input, "\\") || strings.Contains(input, ".")
}

// initializeLogger sets up structured logging based on configuration
func initializeLogger(logLevel string) {
	// Allow environment variable to override config for batch operation scenarios
	if v := strings.ToLower(os.Getenv("LOG_LEVEL")); v != "" {
		logLevel = v
	}

	level := slog.LevelInfo // default
	switch strings.ToLower(logLevel) {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	format := strings.ToLower(os.Getenv("LOG_FORMAT")) // "json" or "text"
	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	} else {
		handler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	}

	slog.SetDefault(slog.New(handler))
}
