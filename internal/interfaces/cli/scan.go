package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	scanapp "github.com/future-architect/uzomuzo-oss/internal/application/scan"
	domaincfg "github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
	domainscan "github.com/future-architect/uzomuzo-oss/internal/domain/scan"
)

// FileDetector inspects a file path and returns the matching parser and file data.
// Returns (nil, nil, nil) when the file is not a recognized structured format.
type FileDetector func(filePath string, parsers map[string]depparser.DependencyParser) (depparser.DependencyParser, []byte, error)

// WorkflowDetector checks whether filePath is a GitHub Actions workflow YAML.
// If it is, it returns (data, true, nil). Otherwise (nil, false, nil).
// The data is the full file content, ready for parsing.
type WorkflowDetector func(filePath string) (data []byte, ok bool, err error)

// WorkflowParser extracts GitHub repository URLs from workflow YAML data.
type WorkflowParser func(data []byte) ([]string, error)

// ErrScanFailPolicy is returned by RunScan when at least one dependency
// matches the --fail-on policy, signaling the caller to exit with code 1.
var ErrScanFailPolicy = errors.New("scan: one or more dependencies matched --fail-on policy")

// ScanOptions contains all scan-specific options parsed from CLI flags.
type ScanOptions struct {
	ProcessingOptions

	Format              string // "detailed", "table", "json", "csv" (empty = smart default)
	FailOnRaw           string // raw --fail-on CSV string
	SBOMPath            string // --sbom flag
	ConfigSampleDefault int    // Config-level default sample size (applied only to PURL/URL list files)
	IncludeActions      bool   // --include-actions: scan GitHub Actions referenced by input repos
}

// ActionsDiscovererFactory creates an ActionsDiscoverer from the scan service's analysis service.
// This avoids importing infrastructure packages directly in the interfaces layer.
type ActionsDiscovererFactory func(svc *scanapp.Service) scanapp.ActionsDiscoverer

// RunScan is the entry point for the "scan" subcommand.
//
// Input resolution order:
//  1. --sbom: CycloneDX SBOM JSON (or "-" for stdin)
//  2. --file: go.mod, GitHub Actions workflow YAML, or PURL/URL list file
//  3. Positional args: PURLs or GitHub URLs (direct mode)
//  4. Stdin pipe
//  5. Auto-detect: go.mod in current directory
//
// DDD Layer: Interfaces (CLI handler, delegates to Application)
func RunScan(ctx context.Context, cfg *domaincfg.Config, args []string, opts ScanOptions, parsers map[string]depparser.DependencyParser, detectFile FileDetector, detectWorkflow WorkflowDetector, parseWorkflow WorkflowParser, actionsFactory ActionsDiscovererFactory) error {
	// Parse fail-on policy
	policy, err := domainscan.ParseFailPolicy(opts.FailOnRaw)
	if err != nil {
		return fmt.Errorf("parse fail policy: %w", err)
	}

	analysisService := createAnalysisService(cfg)
	scanService, err := scanapp.NewService(analysisService)
	if err != nil {
		return fmt.Errorf("failed to initialize scan service: %w", err)
	}

	// Build actions config for modes that support --include-actions.
	var actionsCfg scanapp.ActionsConfig
	if opts.IncludeActions {
		if actionsFactory == nil {
			return fmt.Errorf("include actions: actions discoverer factory is required")
		}
		discoverer := actionsFactory(scanService)
		if discoverer == nil {
			return fmt.Errorf("include actions: actions discoverer factory returned nil")
		}
		actionsCfg = scanapp.ActionsConfig{
			Enabled:    true,
			Discoverer: discoverer,
		}
	}

	// Route to the appropriate input handler
	switch {
	case opts.SBOMPath != "":
		return runScanSBOM(ctx, scanService, opts, parsers, policy)

	case opts.Filename != "":
		if detectFile == nil {
			return fmt.Errorf("file detector is required for --file mode")
		}
		return runScanFile(ctx, scanService, opts, parsers, policy, detectFile, detectWorkflow, parseWorkflow, actionsCfg)

	case len(args) > 0:
		return runScanDirect(ctx, scanService, args, opts, policy, actionsCfg)

	case !isStdinTerminal():
		return runScanStdin(ctx, scanService, opts, policy, actionsCfg)

	default:
		// Auto-detect go.mod in cwd
		return runScanAutoDetect(ctx, scanService, opts, parsers, policy)
	}
}

// runScanSBOM handles --sbom input.
func runScanSBOM(ctx context.Context, svc *scanapp.Service, opts ScanOptions, parsers map[string]depparser.DependencyParser, policy domainscan.FailPolicy) error {
	var data []byte
	var err error
	if opts.SBOMPath == "-" {
		data, err = io.ReadAll(os.Stdin)
	} else {
		data, err = os.ReadFile(opts.SBOMPath)
	}
	if err != nil {
		return fmt.Errorf("failed to read SBOM from '%s': %w", opts.SBOMPath, err)
	}

	parser, ok := parsers["sbom"]
	if !ok {
		return fmt.Errorf("SBOM parser not available")
	}

	result, err := svc.RunFromParser(ctx, parser, data, policy)
	if err != nil {
		return fmt.Errorf("scan failed: %w", err)
	}

	return finalizeScanOutput(svc, result, opts, len(result.Entries))
}

// runScanFile handles --file input (go.mod, SBOM, GitHub Actions workflow YAML, or PURL/URL list).
func runScanFile(ctx context.Context, svc *scanapp.Service, opts ScanOptions, parsers map[string]depparser.DependencyParser, policy domainscan.FailPolicy, detectFile FileDetector, detectWorkflow WorkflowDetector, parseWorkflow WorkflowParser, actionsCfg scanapp.ActionsConfig) error {
	filePath := opts.Filename

	// Try structured format (go.mod / CycloneDX SBOM) first
	parser, data, err := detectFile(filePath, parsers)
	if err != nil {
		return fmt.Errorf("failed to detect file format for '%s': %w", filePath, err)
	}
	if parser != nil {
		// --sample, --line-range, and --include-actions are only meaningful for
		// PURL/URL list files; reject them for structured formats (go.mod / CycloneDX).
		if opts.SampleSize > 0 {
			return fmt.Errorf("--sample is not supported for %s files", parser.FormatName())
		}
		if opts.LineStart > 0 || opts.LineEnd > 0 {
			return fmt.Errorf("--line-range is not supported for %s files", parser.FormatName())
		}
		if opts.IncludeActions {
			return fmt.Errorf("--include-actions is not supported for %s files", parser.FormatName())
		}
		result, err := svc.RunFromParser(ctx, parser, data, policy)
		if err != nil {
			return fmt.Errorf("scan failed: %w", err)
		}
		return finalizeScanOutput(svc, result, opts, len(result.Entries))
	}

	// Try GitHub Actions workflow YAML before falling back to PURL/URL list.
	if detectWorkflow != nil && parseWorkflow != nil {
		wfData, ok, wfErr := detectWorkflow(filePath)
		if wfErr != nil {
			return fmt.Errorf("failed to read workflow file '%s': %w", filePath, wfErr)
		}
		if ok {
			// --sample, --line-range, and --include-actions are only meaningful for
			// PURL/URL list files; reject them for workflow YAML.
			if opts.SampleSize > 0 {
				return fmt.Errorf("--sample is not supported for workflow files")
			}
			if opts.LineStart > 0 || opts.LineEnd > 0 {
				return fmt.Errorf("--line-range is not supported for workflow files")
			}
			if opts.IncludeActions {
				return fmt.Errorf("--include-actions is not supported for workflow files")
			}
			return runScanWorkflow(ctx, svc, wfData, opts, policy, parseWorkflow)
		}
	}

	// Fall back to PURL/URL list
	return runScanPURLList(ctx, svc, opts, filePath, policy, actionsCfg)
}

// runScanPURLList reads a file as a PURL/URL line list and runs the scan.
func runScanPURLList(ctx context.Context, svc *scanapp.Service, opts ScanOptions, filePath string, policy domainscan.FailPolicy, actionsCfg scanapp.ActionsConfig) error {
	if err := validateLineRange(&opts.ProcessingOptions); err != nil {
		return fmt.Errorf("invalid line range: %w", err)
	}
	purls, githubURLs, err := categorizeFileLines(filePath, opts.ProcessingOptions)
	if err != nil {
		return fmt.Errorf("failed to read file '%s': %w", filePath, err)
	}

	// Apply config default sample size only for plain PURL/URL list files
	sampleSize := opts.SampleSize
	if sampleSize == 0 && opts.ConfigSampleDefault > 0 {
		sampleSize = opts.ConfigSampleDefault
	}
	if sampleSize > 0 {
		purls = randomSample(purls, sampleSize)
		githubURLs = randomSample(githubURLs, sampleSize)
	}

	if len(purls) == 0 && len(githubURLs) == 0 {
		return fmt.Errorf("no valid PURLs or GitHub URLs found in file '%s'", filePath)
	}

	result, err := svc.RunFromPURLsWithActions(ctx, purls, githubURLs, policy, actionsCfg)
	if err != nil {
		return fmt.Errorf("scan failed: %w", err)
	}

	return finalizeScanOutput(svc, result, opts, len(purls)+len(githubURLs))
}

// runScanDirect handles positional args (PURLs / GitHub URLs).
func runScanDirect(ctx context.Context, svc *scanapp.Service, args []string, opts ScanOptions, policy domainscan.FailPolicy, actionsCfg scanapp.ActionsConfig) error {
	purls, githubURLs := categorizeInputs(args)
	if len(purls) == 0 && len(githubURLs) == 0 {
		return fmt.Errorf("no valid PURLs or GitHub URLs found in arguments")
	}

	result, err := svc.RunFromPURLsWithActions(ctx, purls, githubURLs, policy, actionsCfg)
	if err != nil {
		return fmt.Errorf("scan failed: %w", err)
	}

	return finalizeScanOutput(svc, result, opts, len(purls)+len(githubURLs))
}

// runScanStdin reads PURLs/GitHub URLs from stdin.
func runScanStdin(ctx context.Context, svc *scanapp.Service, opts ScanOptions, policy domainscan.FailPolicy, actionsCfg scanapp.ActionsConfig) error {
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

	purls, githubURLs := categorizeInputs(lines)
	if len(purls) == 0 && len(githubURLs) == 0 {
		return fmt.Errorf("no valid PURLs or GitHub URLs found in stdin")
	}

	result, err := svc.RunFromPURLsWithActions(ctx, purls, githubURLs, policy, actionsCfg)
	if err != nil {
		return fmt.Errorf("scan failed: %w", err)
	}

	return finalizeScanOutput(svc, result, opts, len(purls)+len(githubURLs))
}

// runScanAutoDetect auto-detects go.mod in the current directory.
func runScanAutoDetect(ctx context.Context, svc *scanapp.Service, opts ScanOptions, parsers map[string]depparser.DependencyParser, policy domainscan.FailPolicy) error {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no input provided and no go.mod found in current directory; run 'uzomuzo scan --help' for usage")
		}
		return fmt.Errorf("failed to read go.mod: %w", err)
	}
	slog.Info("auto-detected go.mod in current directory")

	parser, ok := parsers["gomod"]
	if !ok {
		return fmt.Errorf("go.mod parser not available")
	}

	result, err := svc.RunFromParser(ctx, parser, data, policy)
	if err != nil {
		return fmt.Errorf("scan failed: %w", err)
	}

	return finalizeScanOutput(svc, result, opts, len(result.Entries))
}

// finalizeScanOutput resolves format, renders output, prints rate limit, and returns exit error if needed.
func finalizeScanOutput(svc *scanapp.Service, result *scanapp.Result, opts ScanOptions, inputCount int) error {
	format, err := ResolveFormat(opts.Format, inputCount)
	if err != nil {
		return fmt.Errorf("invalid output format: %w", err)
	}

	if err := renderScanOutput(os.Stdout, result.Entries, format); err != nil {
		return fmt.Errorf("failed to render output: %w", err)
	}

	// Print GitHub API rate limit summary
	if as := svc.AnalysisService(); as != nil {
		remaining, resetAt := as.GitHubClient().RateLimitSummary()
		if resetAt != "" {
			resetLocal := resetAt
			if t, err := time.Parse(time.RFC3339, resetAt); err == nil {
				resetLocal = t.Local().Format("15:04 MST")
			}
			fmt.Fprintf(os.Stderr, "GitHub API: remaining=%d, resets at %s\n", remaining, resetLocal)
		}
	}

	if result.HasFailure {
		return ErrScanFailPolicy
	}
	return nil
}

// runScanWorkflow parses a GitHub Actions workflow YAML and evaluates referenced Actions.
func runScanWorkflow(ctx context.Context, svc *scanapp.Service, data []byte, opts ScanOptions, policy domainscan.FailPolicy, parseWorkflow WorkflowParser) error {
	githubURLs, err := parseWorkflow(data)
	if err != nil {
		return fmt.Errorf("failed to parse workflow: %w", err)
	}
	if len(githubURLs) == 0 {
		slog.Info("scan: no supported GitHub Actions references found in workflow; returning empty result")
		return finalizeScanOutput(svc, &scanapp.Result{}, opts, 0)
	}

	slog.Info("scan: found GitHub Actions references in workflow", "count", len(githubURLs))

	result, err := svc.RunFromPURLs(ctx, nil, githubURLs, policy)
	if err != nil {
		return fmt.Errorf("scan failed: %w", err)
	}
	return finalizeScanOutput(svc, result, opts, len(githubURLs))
}

// isStdinTerminal reports whether stdin is connected to a terminal.
func isStdinTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return true
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
