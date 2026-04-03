package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	urfcli "github.com/urfave/cli/v3"

	scanapp "github.com/future-architect/uzomuzo-oss/internal/application/scan"
	domaincfg "github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/actionscan"
	infradepparser "github.com/future-architect/uzomuzo-oss/internal/infrastructure/depparser"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depparser/cyclonedx"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depparser/ghaworkflow"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depparser/gomod"
	"github.com/future-architect/uzomuzo-oss/internal/interfaces/cli"
)

// scanFlags returns all flags for the scan subcommand.
func scanFlags() []urfcli.Flag {
	return []urfcli.Flag{
		// Input source
		&urfcli.StringFlag{Name: "sbom", Usage: "Path to CycloneDX SBOM JSON (use '-' for stdin)"},
		&urfcli.StringFlag{Name: "file", Usage: "Path to input file (PURL list, go.mod, CycloneDX SBOM, or GitHub Actions workflow YAML)"},

		// Output format
		&urfcli.StringFlag{Name: "format", Aliases: []string{"f"}, Usage: "Output format: detailed, table, json, csv (default: auto)"},

		// CI gate
		&urfcli.StringFlag{Name: "fail-on", Usage: "Comma-separated lifecycle labels that trigger exit 1 (eol-confirmed,eol-effective,eol-scheduled,stalled,legacy-safe)"},

		// File mode options
		&urfcli.IntFlag{Name: "sample", Usage: "Randomly sample up to N PURLs and N GitHub URLs (file mode only)"},
		&urfcli.StringFlag{Name: "line-range", Usage: "Limit to line range START:END (file mode only)"},

		// Actions scanning
		&urfcli.BoolFlag{Name: "include-actions", Usage: "Also scan GitHub Actions referenced in target repositories' workflows"},
	}
}

// scanCommand builds the "scan" subcommand.
func scanCommand(cfg *domaincfg.Config) *urfcli.Command {
	return &urfcli.Command{
		Name:  "scan",
		Usage: "Scan dependencies for lifecycle health",
		UsageText: `uzomuzo scan pkg:npm/express@4.18.2                          Single package
   uzomuzo scan pkg:npm/express@4.18.2 pkg:golang/...            Multiple PURLs
   uzomuzo scan https://github.com/expressjs/express              GitHub URL
   uzomuzo scan --file purls.txt                                  PURL list file
   uzomuzo scan --sbom bom.json                                   CycloneDX SBOM
   trivy fs . --format cyclonedx | uzomuzo scan --sbom -          Pipe SBOM
   uzomuzo scan --file go.mod                                     go.mod
   uzomuzo scan --file .github/workflows/ci.yml                   GitHub Actions workflow
   uzomuzo scan                                                   Auto-detect go.mod
   cat purls.txt | uzomuzo scan                                   Pipe PURLs
   uzomuzo scan https://github.com/owner/repo --include-actions   Scan repo + its Actions

CI gate examples:
   uzomuzo scan --sbom bom.json --fail-on eol-confirmed
   uzomuzo scan --sbom bom.json --fail-on eol-confirmed,eol-effective,stalled`,
		Flags: scanFlags(),
		Action: func(ctx context.Context, cmd *urfcli.Command) error {
			return scanAction(ctx, cfg, cmd)
		},
	}
}

// buildApp constructs the urfave/cli command tree.
func buildApp(cfg *domaincfg.Config) *urfcli.Command {
	return &urfcli.Command{
		Name:    "uzomuzo",
		Usage:   "OSS dependency health checker",
		Version: version,
		UsageText: `uzomuzo scan pkg:npm/express@4.18.2            Single package
   uzomuzo scan --file purls.txt                 File mode
   cat purls.txt | uzomuzo scan                  Pipe mode
   uzomuzo scan --sbom bom.json                  SBOM scan
   uzomuzo scan --sbom bom.json --fail-on eol-confirmed   CI gate

Examples:
   uzomuzo scan pkg:npm/express@4.18.2 pkg:pypi/django@4.2.0
   uzomuzo scan https://github.com/expressjs/express
   uzomuzo scan --file input_purls.txt --sample 10
   uzomuzo scan --sbom bom.json --format json
   uzomuzo scan --sbom bom.json --fail-on eol-confirmed,eol-effective`,
		Action: func(ctx context.Context, cmd *urfcli.Command) error {
			// Root invocation without subcommand: show help
			fmt.Fprintln(os.Stderr, "No subcommand provided. Run 'uzomuzo scan --help' for usage.")
			return nil
		},
		Commands: []*urfcli.Command{
			scanCommand(cfg),
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

// scanAction handles the "scan" subcommand invocation.
func scanAction(ctx context.Context, cfg *domaincfg.Config, cmd *urfcli.Command) error {
	opts, err := buildScanOptions(cmd)
	if err != nil {
		return fmt.Errorf("invalid flags: %w", err)
	}

	// --file and --sbom are mutually exclusive
	if opts.Filename != "" && opts.SBOMPath != "" {
		return fmt.Errorf("--file and --sbom are mutually exclusive; use one or the other")
	}

	// --sample and --line-range require --file specifically
	if opts.Filename == "" {
		if cmd.IsSet("sample") {
			return fmt.Errorf("--sample requires --file")
		}
		if cmd.IsSet("line-range") {
			return fmt.Errorf("--line-range requires --file")
		}
	}
	if cmd.IsSet("sample") && opts.SampleSize < 0 {
		return fmt.Errorf("--sample must be zero (process all) or a positive integer")
	}

	// Pass config default sample size; applied only to PURL/URL list files
	// (not to structured formats like go.mod/CycloneDX which reject --sample)
	if !cmd.IsSet("sample") {
		opts.ConfigSampleDefault = cfg.App.SampleSize
	}

	args := cmd.Args().Slice()

	// Reject positional args when --file or --sbom is set
	if (opts.Filename != "" || opts.SBOMPath != "") && len(args) > 0 {
		return fmt.Errorf("positional arguments are not allowed with --file or --sbom")
	}

	// --include-actions is only supported for GitHub URL inputs (positional args, file list, stdin).
	// Reject it for --sbom early; --file with structured formats is rejected in runScanFile
	// after file type detection.
	if opts.IncludeActions {
		if opts.SBOMPath != "" {
			return fmt.Errorf("--include-actions is not supported with --sbom")
		}
		if cfg.GitHub.Token == "" {
			return fmt.Errorf("--include-actions requires GITHUB_TOKEN to fetch workflow files via the Contents API")
		}
	}

	parsers := map[string]depparser.DependencyParser{
		"sbom":  &cyclonedx.Parser{},
		"gomod": &gomod.Parser{},
	}

	// Factory creates an ActionsDiscoverer from the scan service's GitHub client.
	actionsFactory := func(svc *scanapp.Service) scanapp.ActionsDiscoverer {
		githubClient := svc.AnalysisService().GitHubClient()
		return actionscan.NewDiscoveryService(githubClient, cfg.GitHub.MaxConcurrency)
	}

	return cli.RunScan(ctx, cfg, args, opts, parsers, infradepparser.DetectFileParser, detectWorkflowFile, ghaworkflow.ParseGitHubURLs, actionsFactory)
}

// detectWorkflowFile checks whether filePath is a GitHub Actions workflow YAML.
// Returns the full file data and true if it is a workflow, or (nil, false, nil) otherwise.
//
// Detection is delegated to ghaworkflow.IsWorkflowYAML (single source of truth).
// To avoid unnecessary I/O for non-workflow YAML files (e.g., docker-compose.yml),
// only a small prefix is read for content-based marker detection. The full file is
// read only after confirming the file is a workflow.
func detectWorkflowFile(filePath string) ([]byte, bool, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext != ".yml" && ext != ".yaml" {
		return nil, false, nil
	}

	// Read a small prefix for content-based detection by IsWorkflowYAML.
	// Path-based detection (inside .github/workflows/) also happens inside
	// IsWorkflowYAML, so we still need to call it even with an empty prefix.
	var prefix []byte
	if !ghaworkflow.IsWorkflowYAMLByPath(filePath) {
		f, err := os.Open(filePath)
		if err != nil {
			return nil, false, fmt.Errorf("failed to open file '%s': %w", filePath, err)
		}
		prefix = make([]byte, 1024)
		n, readErr := f.Read(prefix)
		_ = f.Close() // best-effort cleanup, original error preserved
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, false, fmt.Errorf("failed to read file '%s': %w", filePath, readErr)
		}
		prefix = prefix[:n]
		if !ghaworkflow.IsWorkflowYAML(filePath, prefix) {
			return nil, false, nil
		}
	}

	// Confirmed workflow — read the full file.
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read file '%s': %w", filePath, err)
	}
	return data, true, nil
}

// buildScanOptions maps urfave/cli flags to ScanOptions.
func buildScanOptions(cmd *urfcli.Command) (cli.ScanOptions, error) {
	opts := cli.ScanOptions{
		ProcessingOptions: cli.ProcessingOptions{
			SampleSize: int(cmd.Int("sample")),
			Filename:   cmd.String("file"),
		},
		Format:         cmd.String("format"),
		FailOnRaw:      cmd.String("fail-on"),
		SBOMPath:       cmd.String("sbom"),
		IncludeActions: cmd.Bool("include-actions"),
	}
	if raw := cmd.String("line-range"); raw != "" {
		ls, le, err := cli.ParseLineRange(raw)
		if err != nil {
			return cli.ScanOptions{}, err
		}
		opts.LineStart = ls
		opts.LineEnd = le
	}
	return opts, nil
}
