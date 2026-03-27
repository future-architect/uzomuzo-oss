// Main entry point using Clean Architecture
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	urfcli "github.com/urfave/cli/v3"

	domaincfg "github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depparser/cyclonedx"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depparser/gomod"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/spdx"
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

// main function: Entry point for Clean Architecture implementation
// Processes PURLs for scorecard analysis using Clean Architecture patterns
// Supports direct PURL/GitHub URL processing and batch file processing
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
		// ErrAuditReplaceFound is a signal, not a failure — exit silently with code 1.
		if errors.Is(err, cli.ErrAuditReplaceFound) {
			os.Exit(1)
		}
		slog.Error("command failed", "error", err)
		os.Exit(1)
	}
}

// buildApp constructs the urfave/cli command tree.
func buildApp(cfg *domaincfg.Config) *urfcli.Command {
	return &urfcli.Command{
		Name:    "uzomuzo",
		Usage:   "OSS dependency health checker",
		Version: version,
		UsageText: `uzomuzo <purl> [more...]          Direct mode
   uzomuzo <file>                    File mode
   <command> | uzomuzo [flags]       Pipe mode

Examples:
   uzomuzo pkg:npm/express@4.18.2 pkg:pypi/django@4.2.0
   uzomuzo https://github.com/expressjs/express
   uzomuzo input_purls.txt
   uzomuzo --line-range 1:10 input_purls.txt
   cat purls.txt | uzomuzo --only-eol
   uzomuzo audit --sbom bom.json
   syft . -o cyclonedx-json | uzomuzo audit --sbom -
   uzomuzo audit --format json`,
		Flags: []urfcli.Flag{
			&urfcli.BoolFlag{Name: "only-review-needed", Usage: "Show only 'Review Needed' results"},
			&urfcli.BoolFlag{Name: "only-eol", Usage: "Show only 'EOL-*' results"},
			&urfcli.StringFlag{Name: "ecosystem", Usage: "Filter to a single ecosystem (npm, pypi, maven, etc.)"},
			&urfcli.IntFlag{Name: "sample", Usage: "Randomly sample up to N inputs (file mode only)"},
			&urfcli.StringFlag{Name: "export-license-csv", Usage: "Write license CSV to path"},
			&urfcli.StringFlag{Name: "line-range", Usage: "Limit to line range START:END (file mode only)"},
		},
		Action: func(ctx context.Context, cmd *urfcli.Command) error {
			return rootAction(ctx, cfg, cmd)
		},
		Commands: []*urfcli.Command{
			{
				Name:  "audit",
				Usage: "Audit dependencies from SBOM or go.mod for lifecycle health",
				Flags: []urfcli.Flag{
					&urfcli.StringFlag{Name: "sbom", Usage: "Path to CycloneDX SBOM JSON (use '-' for stdin)"},
					&urfcli.StringFlag{Name: "file", Usage: "Path to go.mod file"},
					&urfcli.StringFlag{Name: "format", Aliases: []string{"f"}, Value: "table", Usage: "Output format: table, json, csv"},
				},
				Action: func(ctx context.Context, cmd *urfcli.Command) error {
					parsers := map[string]depparser.DependencyParser{
						"sbom":  &cyclonedx.Parser{},
						"gomod": &gomod.Parser{},
					}
					return cli.RunAudit(ctx, cfg, cmd.String("sbom"), cmd.String("file"), cmd.String("format"), parsers)
				},
			},
			{
				Name:  "update-spdx",
				Usage: "Refresh embedded SPDX license list",
				Action: func(ctx context.Context, _ *urfcli.Command) error {
					return runUpdateSPDX(ctx)
				},
			},
		},
	}
}

// rootAction handles the default (non-subcommand) invocation.
func rootAction(ctx context.Context, cfg *domaincfg.Config, cmd *urfcli.Command) error {
	opts, err := buildProcessingOptions(cfg, cmd)
	if err != nil {
		return fmt.Errorf("invalid flags: %w", err)
	}

	args := cmd.Args().Slice()

	// No positional args → check stdin
	if len(args) == 0 {
		if !isTerminal(os.Stdin) {
			return processStdin(ctx, cfg, opts)
		}
		// urfave/cli auto-generates help; show it when no args
		return fmt.Errorf("no input provided. Run 'uzomuzo --help' for usage")
	}

	first := strings.TrimSpace(args[0])
	if first == "" {
		return fmt.Errorf("input cannot be empty")
	}

	if isFilePath(first) {
		if opts.SampleSize == 0 {
			opts.SampleSize = cfg.App.SampleSize
		}
		return cli.ProcessFileMode(ctx, cfg, first, opts)
	}

	// Direct mode: all positional args are PURLs/GitHub URLs
	return cli.ProcessDirectMode(ctx, cfg, args, opts)
}

// buildProcessingOptions maps urfave/cli flags to ProcessingOptions.
func buildProcessingOptions(cfg *domaincfg.Config, cmd *urfcli.Command) (cli.ProcessingOptions, error) {
	opts := cli.ProcessingOptions{
		OnlyReviewNeeded: cmd.Bool("only-review-needed"),
		OnlyEOL:          cmd.Bool("only-eol"),
		Ecosystem:        cmd.String("ecosystem"),
		SampleSize:       int(cmd.Int("sample")),
		LicenseCSVPath:   cmd.String("export-license-csv"),
	}
	// SampleSize default from config is applied later in ProcessFileMode;
	// leave zero here so direct/stdin modes are not affected.
	if raw := cmd.String("line-range"); raw != "" {
		ls, le, err := cli.ParseLineRange(raw)
		if err != nil {
			return cli.ProcessingOptions{}, err
		}
		opts.LineStart = ls
		opts.LineEnd = le
	}
	return opts, nil
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
func processStdin(ctx context.Context, cfg *domaincfg.Config, opts cli.ProcessingOptions) error {
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
	return cli.ProcessDirectMode(ctx, cfg, lines, opts)
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
