package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	urfcli "github.com/urfave/cli/v3"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	domaincfg "github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depparser/cyclonedx"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depparser/gomod"
	"github.com/future-architect/uzomuzo-oss/internal/interfaces/cli"
)

// commonFlags returns the filtering/output flags shared by both the analyze
// subcommand and the deprecated root action.
// Returns a fresh slice each call; safe to append command-specific flags.
func commonFlags() []urfcli.Flag {
	return []urfcli.Flag{
		&urfcli.BoolFlag{Name: "only-review-needed", Usage: "Show only 'Review Needed' results"},
		&urfcli.BoolFlag{Name: "only-eol", Usage: "Show only 'EOL-*' results"},
		&urfcli.StringFlag{Name: "ecosystem", Usage: "Filter to a single ecosystem (npm, pypi, maven, etc.)"},
		&urfcli.StringFlag{Name: "export-license-csv", Usage: "Write license CSV to path"},
		&urfcli.IntFlag{Name: "sample", Usage: "Randomly sample up to N inputs (file mode only)"},
		&urfcli.StringFlag{Name: "line-range", Usage: "Limit to line range START:END (file mode only)"},
	}
}

// analyzeFlags returns commonFlags plus analyze-specific flags (--file).
func analyzeFlags() []urfcli.Flag {
	return append(commonFlags(),
		&urfcli.StringFlag{Name: "file", Usage: "Path to input file containing PURLs/URLs (one per line)"},
	)
}

// analyzeCommand builds the "analyze" subcommand.
func analyzeCommand(cfg *domaincfg.Config) *urfcli.Command {
	return &urfcli.Command{
		Name:  "analyze",
		Usage: "Analyze PURLs or GitHub URLs for lifecycle health",
		UsageText: `uzomuzo analyze <purl> [more...]                Direct mode
   uzomuzo analyze https://github.com/owner/repo   GitHub URL mode
   uzomuzo analyze --file purls.txt                 File mode
   uzomuzo analyze --file purls.txt --sample 10     File mode with sampling
   <command> | uzomuzo analyze                      Pipe mode
   <command> | uzomuzo analyze -                    Pipe mode (explicit)`,
		Flags: analyzeFlags(),
		Action: func(ctx context.Context, cmd *urfcli.Command) error {
			return analyzeAction(ctx, cfg, cmd)
		},
	}
}

// buildApp constructs the urfave/cli command tree.
func buildApp(cfg *domaincfg.Config) *urfcli.Command {
	return &urfcli.Command{
		Name:    "uzomuzo",
		Usage:   "OSS dependency health checker",
		Version: version,
		UsageText: `uzomuzo analyze <purl> [more...]       Direct mode
   uzomuzo analyze --file purls.txt      File mode
   <command> | uzomuzo analyze            Pipe mode
   uzomuzo audit --sbom bom.json         Audit mode

Examples:
   uzomuzo analyze pkg:npm/express@4.18.2 pkg:pypi/django@4.2.0
   uzomuzo analyze https://github.com/expressjs/express
   uzomuzo analyze --file input_purls.txt --sample 10
   uzomuzo analyze --file input_purls.txt --line-range 1:10
   cat purls.txt | uzomuzo analyze --only-eol
   uzomuzo audit --sbom bom.json
   syft . -o cyclonedx-json | uzomuzo audit --sbom -
   uzomuzo audit --format json`,
		// Root flags kept for backward compatibility (deprecated).
		// Uses commonFlags() to stay in sync with the analyze subcommand.
		Flags: commonFlags(),
		Action: func(ctx context.Context, cmd *urfcli.Command) error {
			return rootAction(ctx, cfg, cmd)
		},
		Commands: []*urfcli.Command{
			analyzeCommand(cfg),
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

// analyzeAction handles the "analyze" subcommand invocation.
func analyzeAction(ctx context.Context, cfg *domaincfg.Config, cmd *urfcli.Command) error {
	opts, err := buildProcessingOptions(cmd)
	if err != nil {
		return fmt.Errorf("invalid flags: %w", err)
	}

	filePath := cmd.String("file")
	args := cmd.Args().Slice()

	// --sample and --line-range require --file
	if filePath == "" {
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

	// File mode: --file flag is set
	if filePath != "" {
		if len(args) > 0 {
			return fmt.Errorf("positional arguments are not allowed with --file; pass the file path via --file only")
		}
		// Apply config default only if --sample was not explicitly provided.
		if !cmd.IsSet("sample") && opts.SampleSize == 0 {
			opts.SampleSize = cfg.App.SampleSize
		}
		return cli.ProcessFileMode(ctx, cfg, filePath, opts)
	}

	// Pipe mode: explicit "-" or stdin is not a terminal
	if (len(args) == 1 && args[0] == "-") || (len(args) == 0 && !isTerminal(os.Stdin)) {
		return processStdin(ctx, cfg, opts)
	}

	// Direct mode: positional args are PURLs/GitHub URLs
	if len(args) > 0 {
		return cli.ProcessDirectMode(ctx, cfg, args, opts)
	}

	// No input: return nil to let urfave/cli show default help.
	fmt.Fprintln(os.Stderr, "No input provided. Run 'uzomuzo analyze --help' for usage.")
	return nil
}

// rootAction handles the default (non-subcommand) invocation.
//
// DEPRECATED: Direct root invocation is deprecated. Use "uzomuzo analyze" instead.
// This shim prints a deprecation warning and delegates using the legacy heuristic
// for one release cycle of backward compatibility.
func rootAction(ctx context.Context, cfg *domaincfg.Config, cmd *urfcli.Command) error {
	opts, err := buildProcessingOptions(cmd)
	if err != nil {
		return fmt.Errorf("invalid flags: %w", err)
	}

	args := cmd.Args().Slice()

	// No positional args → check stdin
	if len(args) == 0 {
		if !isTerminal(os.Stdin) {
			fmt.Fprintln(os.Stderr, "WARNING: Piping without a subcommand is deprecated. Use 'uzomuzo analyze' instead.")
			return processStdin(ctx, cfg, opts)
		}
		// No args and no stdin: show help and exit cleanly (not a deprecated path).
		fmt.Fprintln(os.Stderr, "No input provided. Run 'uzomuzo analyze --help' for usage.")
		return nil
	}

	// Deprecation warning for direct root invocation with arguments.
	fmt.Fprintln(os.Stderr, "WARNING: Running without a subcommand is deprecated. Use 'uzomuzo analyze' instead.")

	first := strings.TrimSpace(args[0])
	if first == "" {
		return fmt.Errorf("input cannot be empty")
	}

	// Check for GitHub URL / owner/repo shorthand before falling back to file path heuristic,
	// because "owner/repo" contains "/" and would otherwise match as a file path.
	if common.IsValidGitHubURL(first) {
		return cli.ProcessDirectMode(ctx, cfg, args, opts)
	}

	if isFilePath(first) {
		// Apply config default only if --sample was not explicitly provided.
		if !cmd.IsSet("sample") && opts.SampleSize == 0 {
			opts.SampleSize = cfg.App.SampleSize
		}
		return cli.ProcessFileMode(ctx, cfg, first, opts)
	}

	// Direct mode: all positional args are PURLs/GitHub URLs
	return cli.ProcessDirectMode(ctx, cfg, args, opts)
}

// buildProcessingOptions maps urfave/cli flags to ProcessingOptions.
func buildProcessingOptions(cmd *urfcli.Command) (cli.ProcessingOptions, error) {
	opts := cli.ProcessingOptions{
		OnlyReviewNeeded: cmd.Bool("only-review-needed"),
		OnlyEOL:          cmd.Bool("only-eol"),
		Ecosystem:        cmd.String("ecosystem"),
		SampleSize:       int(cmd.Int("sample")),
		LicenseCSVPath:   cmd.String("export-license-csv"),
	}
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
