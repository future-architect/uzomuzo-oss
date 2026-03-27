// Main entry point using Clean Architecture
package main

import (
	"bufio"
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	domaincfg "github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/spdx"
	"github.com/future-architect/uzomuzo-oss/internal/interfaces/cli"

	"github.com/joho/godotenv"
)

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

	if len(os.Args) < 2 {
		if !isTerminal(os.Stdin) {
			processStdin(ctx, cfg, nil)
			return
		}
		showUsage()
		os.Exit(1)
	}

	// Separate flags (starting with '-') from positional args to decide mode based on first positional
	var flags []string
	var positional []string
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
		} else {
			positional = append(positional, a)
		}
	}

	if len(positional) == 0 {
		if !isTerminal(os.Stdin) {
			processStdin(ctx, cfg, flags)
			return
		}
		slog.Error("No positional input provided (need PURL/GitHub URL, file path, or subcommand)")
		os.Exit(1)
	}

	first := strings.TrimSpace(positional[0])

	// Subcommands
	switch first {
	case "audit":
		// Combine flags and remaining positional args for the audit subcommand.
		// This avoids passing global flags (e.g., --log-level debug) that appear before "audit".
		auditArgs := append(flags, positional[1:]...)
		cli.RunAudit(ctx, cfg, auditArgs)
		return
	case "update-spdx":
		if err := runUpdateSPDX(ctx); err != nil {
			slog.Error("update-spdx failed", "error", err)
			os.Exit(1)
		}
		return
	}
	if first == "" {
		slog.Error("Input cannot be empty")
		os.Exit(1)
	}

	if isFilePath(first) {
		// Reconstruct arg list for file mode: file path first, then flags, then remaining positional (e.g., sample size)
		var fileModeArgs []string
		fileModeArgs = append(fileModeArgs, first)
		fileModeArgs = append(fileModeArgs, flags...)
		if len(positional) > 1 { // potential sample size or ignored extras
			fileModeArgs = append(fileModeArgs, positional[1:]...)
		}
		cli.ProcessFileMode(cfg, fileModeArgs)
		return
	}

	// Direct mode: combine flags and positional (order doesn't matter for our parser)
	combined := append(flags, positional...)
	cli.ProcessDirectMode(ctx, cfg, combined)
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
func processStdin(ctx context.Context, cfg *domaincfg.Config, flags []string) {
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
		slog.Error("Failed to read from stdin", "error", err)
		os.Exit(1)
	}
	if len(lines) == 0 {
		slog.Error("No valid input read from stdin")
		os.Exit(1)
	}
	slog.Info("Read inputs from stdin", "count", len(lines))
	combined := append(flags, lines...)
	cli.ProcessDirectMode(ctx, cfg, combined)
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

// showUsage displays usage information
func showUsage() {
	slog.Error("Usage error",
		"usage", "Direct mode: uzomuzo <purl_or_github_url> [more_inputs...]",
		"file_usage", "File mode: uzomuzo <purl_file> [sample_size]",
		"pipe_usage", "Pipe mode: <command> | uzomuzo [flags]",
		"subcommands", []string{
			"audit          — Audit dependencies from SBOM or go.mod for lifecycle health",
			"update-spdx    — Refresh embedded SPDX license list",
		},
		"examples", []string{
			"uzomuzo pkg:npm/express@4.18.2 pkg:pypi/django@4.2.0",
			"uzomuzo pkg:pypi/django@4.2.0",
			"uzomuzo https://github.com/expressjs/express",
			"uzomuzo github.com/django/django",
			"uzomuzo test_max.txt 100",
			"cat purls.txt | uzomuzo --only-eol",
			"uzomuzo audit --sbom bom.json",
			"syft . -o cyclonedx-json | uzomuzo audit --sbom -",
			"uzomuzo audit                     # auto-detect go.mod in cwd",
			"uzomuzo audit --format json",
		})
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

