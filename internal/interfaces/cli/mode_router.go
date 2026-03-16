package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

// RunMode enumerates high-level execution modes for batch CLI.
type RunMode int

const (
	ModeBatchFile RunMode = iota
	ModeDirect
)

func (m RunMode) String() string {
	switch m {
	case ModeDirect:
		return "direct"
	case ModeBatchFile:
		return "batch-file"
	default:
		return "unknown"
	}
}

// Normalize applies trimming + validates mutually exclusive flags.
func (o *ProcessingOptions) Normalize() error {
	if o == nil {
		return fmt.Errorf("nil options")
	}
	o.Ecosystem = strings.ToLower(strings.TrimSpace(o.Ecosystem))
	return nil
}

// ResolveMode determines the execution mode from normalized options.
func ResolveMode(o ProcessingOptions) (RunMode, error) {
	if err := o.Normalize(); err != nil {
		return 0, err
	}
	if o.IsDirectInput {
		return ModeDirect, nil
	}
	return ModeBatchFile, nil
}

// CommandHandler encapsulates one mode's primary output behavior.
type CommandHandler func(ctx context.Context, cfg *config.Config, inputs *ProcessingInputs, results *ProcessingResults, opts ProcessingOptions) error

// CommandRouter maps modes to handlers & executes post hooks.
type CommandRouter struct {
	registry  *ModeRegistry
	postHooks []func(cfg *config.Config, inputs *ProcessingInputs, results *ProcessingResults, opts ProcessingOptions)
}

// ModeRegistry provides a table-driven way to register and retrieve mode handlers.
type ModeRegistry struct {
	handlers map[RunMode]CommandHandler
}

func NewModeRegistry() *ModeRegistry {
	return &ModeRegistry{handlers: map[RunMode]CommandHandler{
		ModeDirect:    handleDirect,
		ModeBatchFile: handleBatchFile,
	}}
}

func (r *ModeRegistry) Add(mode RunMode, h CommandHandler) {
	r.handlers[mode] = h
}

func (r *ModeRegistry) Get(m RunMode) (CommandHandler, bool) {
	h, ok := r.handlers[m]
	return h, ok
}

// NewCommandRouter builds a router with default handlers registered via registry.
func NewCommandRouter() *CommandRouter {
	reg := NewModeRegistry()
	r := &CommandRouter{registry: reg}
	r.postHooks = []func(*config.Config, *ProcessingInputs, *ProcessingResults, ProcessingOptions){
		postErrors,
		postSummary,
		postDebugBlock,
		postCSVExport,
	}
	return r
}

// Run executes the handler then appropriate post hooks.
func (r *CommandRouter) Run(mode RunMode, ctx context.Context, cfg *config.Config, inputs *ProcessingInputs, results *ProcessingResults, opts ProcessingOptions) error {
	h, ok := r.registry.Get(mode)
	if !ok {
		return fmt.Errorf("no handler for mode %s", mode)
	}
	if err := h(ctx, cfg, inputs, results, opts); err != nil {
		return err
	}
	for _, hook := range r.postHooks {
		hook(cfg, inputs, results, opts)
	}
	return nil
}

// ---------------- Handlers ----------------

func handleDirect(_ context.Context, _ *config.Config, inputs *ProcessingInputs, results *ProcessingResults, opts ProcessingOptions) error {
	fmt.Printf("\n📊 Processing Summary:\n")
	if len(inputs.SupportedPURLs) > 0 {
		fmt.Printf("   PURLs processed: %d\n", len(inputs.SupportedPURLs))
	}
	if len(inputs.ValidGitHubURLs) > 0 {
		fmt.Printf("   GitHub URLs processed: %d\n", len(inputs.ValidGitHubURLs))
	}
	if len(inputs.SkippedPURLs) > 0 {
		fmt.Printf("   Skipped (unsupported): %d\n", len(inputs.SkippedPURLs))
	}
	if opts.OnlyReviewNeeded {
		fmt.Printf("   Filter: only 'Review Needed' results will be shown\n")
	}
	if opts.Ecosystem != "" {
		fmt.Printf("   Filter: ecosystem = %s\n", opts.Ecosystem)
	}
	if opts.OnlyEOL {
		fmt.Printf("   Filter: only 'EOL-*' results will be shown\n")
	}
	// Verbose per-PURL output shown only when predicate allows.
	if opts.ShouldShowPerPURLDetails() {
		displayBatchAnalysesFull(results.AllAnalyses, opts)
	}
	return nil
}

func handleBatchFile(_ context.Context, _ *config.Config, inputs *ProcessingInputs, results *ProcessingResults, opts ProcessingOptions) error {
	fmt.Printf("\n" + strings.Repeat("=", separatorLength) + "\n")
	if len(inputs.SupportedPURLs) > 0 && len(inputs.ValidGitHubURLs) > 0 {
		fmt.Printf("📊 MIXED FILE BATCH ANALYSIS RESULTS\n")
		fmt.Printf("📝 PURLs: %d | GitHub URLs: %d | Total: %d\n", len(inputs.SupportedPURLs), len(inputs.ValidGitHubURLs), len(results.AllAnalyses))
	} else if len(inputs.SupportedPURLs) > 0 {
		fmt.Printf("📊 INDIVIDUAL PURL ANALYSIS RESULTS\n")
	} else if len(inputs.ValidGitHubURLs) > 0 {
		fmt.Printf("📊 GITHUB URL BATCH ANALYSIS RESULTS\n")
	}
	fmt.Printf(strings.Repeat("=", separatorLength) + "\n")
	if opts.ShouldShowPerPURLDetails() {
		displayBatchAnalysesFull(results.AllAnalyses, opts)
	}
	return nil
}

// ---------------- Post Hooks ----------------

// Axis registry for future analysis axes (e.g., lifecycle, security, etc.)
type AxisEvaluator interface {
	Key() string
	Evaluate(a *ProcessingResults) error
}

var axisRegistry []AxisEvaluator

func RegisterAxis(e AxisEvaluator) {
	axisRegistry = append(axisRegistry, e)
}

func AllAxes() []AxisEvaluator {
	return axisRegistry
}

func postErrors(_ *config.Config, _ *ProcessingInputs, results *ProcessingResults, _ ProcessingOptions) {
	displayBatchErrors(results.AllAnalyses)
}

func postSummary(_ *config.Config, _ *ProcessingInputs, results *ProcessingResults, opts ProcessingOptions) {
	if strings.TrimSpace(opts.LicenseCSVPath) != "" { // suppress summary in license export mode
		return
	}
	displayBatchAnalysesSummary(results.AllAnalyses)
}

func postDebugBlock(_ *config.Config, _ *ProcessingInputs, results *ProcessingResults, _ ProcessingOptions) {
	if strings.ToLower(os.Getenv("LOG_LEVEL")) == "debug" {
		printReviewNeededArgs(results.AllAnalyses)
	}
}

func postCSVExport(cfg *config.Config, inputs *ProcessingInputs, results *ProcessingResults, opts ProcessingOptions) {
	if opts.IsDirectInput { // only file mode previously exported CSV
		return
	}
	analysisService := createAnalysisService(cfg)
	if err := analysisService.WriteScoreCardCSV(results.AllAnalyses, "scorecard.csv"); err != nil {
		slog.Error("Failed to write CSV file", "error", err)
	}
	// Optional extended license CSV export (user-specified path)
	if strings.TrimSpace(opts.LicenseCSVPath) != "" {
		if err := analysisService.WriteLicenseCSV(results.AllAnalyses, opts.LicenseCSVPath); err != nil {
			slog.Error("Failed to write license CSV file", "error", err, "path", opts.LicenseCSVPath)
		} else {
			// Print human-visible path (stdout) and structured log.
			fmt.Printf("\n📄 License CSV written: %s\n", opts.LicenseCSVPath)
			slog.Info("License CSV exported", "path", opts.LicenseCSVPath)
			// Skip subsequent EOL detail & summary output phases entirely when license export mode is active.
			// Early return ensures no other summary / per-PURL display functions run.
			return
		}
	}
}
