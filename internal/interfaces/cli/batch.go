package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/application"
	"github.com/future-architect/uzomuzo-oss/internal/common"
	"github.com/future-architect/uzomuzo-oss/internal/common/collections"
	commonpurl "github.com/future-architect/uzomuzo-oss/internal/common/purl"

	analysispkg "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	domaincfg "github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

// maxDescriptionLen limits the length of repository/project descriptions in CLI output.
const maxDescriptionLen = 150

// truncateDescription normalizes to a single line and truncates with an ellipsis when too long.
func truncateDescription(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	// single line normalization
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxDescriptionLen {
		return s[:maxDescriptionLen-1] + "…"
	}
	return s
}

// filterPackageTypes filters PURLs by allowed package types
func filterPackageTypes(purls []string) (allowed []string, notAllowed []string) {
	processor := common.NewResultProcessor()
	return processor.FilterPackageTypes(purls)
}

// randomSample randomly selects a subset of strings (works for PURLs, GitHub URLs, etc.)
func randomSample(items []string, sampleSize int) []string {
	if sampleSize <= 0 || sampleSize >= len(items) {
		return items // return all if sample size is invalid or >= total
	}

	// Create a copy to avoid modifying the original slice
	itemsCopy := make([]string, len(items))
	copy(itemsCopy, items)

	// Shuffle using Go 1.20+ auto-seeded random generation
	rand.Shuffle(len(itemsCopy), func(i, j int) {
		itemsCopy[i], itemsCopy[j] = itemsCopy[j], itemsCopy[i]
	})

	return itemsCopy[:sampleSize]
}

// validateLineRange validates line range options and returns an error if invalid.
func validateLineRange(opts *ProcessingOptions) error {
	if opts.LineStart < 0 || opts.LineEnd < 0 {
		return fmt.Errorf("--line-range values must be positive")
	}
	if opts.LineStart > 0 && opts.LineEnd > 0 && opts.LineEnd < opts.LineStart {
		return fmt.Errorf("--line-range end must be >= start (start=%d, end=%d)", opts.LineStart, opts.LineEnd)
	}
	return nil
}

// ProcessDirectMode handles direct (non-file) inputs: a list of PURLs and/or GitHub URLs provided inline.
//
// DDD Layer: Interface (unified entry point for direct multi-input processing)
// Responsibilities:
//   - Categorize raw inputs into PURLs and GitHub URLs
//   - Delegate to shared processing pipeline (processMixedContent)
//
// Constraints:
//   - --sample and --line-range are ignored / rejected (only file mode supports them)
func ProcessDirectMode(ctx context.Context, cfg *domaincfg.Config, inputs []string, opts ProcessingOptions) error {
	if len(inputs) == 0 {
		return fmt.Errorf("no inputs provided for batch processing")
	}
	opts.IsDirectInput = true
	if opts.LineStart > 0 || opts.LineEnd > 0 {
		return fmt.Errorf("--line-range is only valid in file mode")
	}
	purls, githubURLs := categorizeInputs(inputs)
	if len(purls) == 0 && len(githubURLs) == 0 {
		return fmt.Errorf("no valid PURLs or GitHub URLs found in inputs")
	}
	return processMixedContent(ctx, cfg, purls, githubURLs, opts)
}

// categorizeInputs separates PURLs and GitHub URLs from mixed input
func categorizeInputs(inputs []string) (purls []string, githubURLs []string) {
	for _, input := range inputs {
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		if strings.HasPrefix(input, "pkg:") {
			purls = append(purls, input)
		} else if common.IsValidGitHubURL(input) {
			githubURLs = append(githubURLs, input)
		} else {
			slog.Warn("Unsupported input format",
				"input", input,
				"suggestion", "Expected PURL (pkg:) or GitHub URL format")
		}
	}
	return purls, githubURLs
}

// ProcessFileMode handles file mode processing with unified line-by-line detection.
//
// DDD Layer: Interface (CLI handler, delegates to processMixedContent)
func ProcessFileMode(ctx context.Context, cfg *domaincfg.Config, filePath string, opts ProcessingOptions) error {
	opts.IsDirectInput = false
	opts.Filename = filePath
	if err := validateLineRange(&opts); err != nil {
		return fmt.Errorf("invalid line range: %w", err)
	}
	purls, githubURLs, err := categorizeFileLines(filePath, opts)
	if err != nil {
		return err
	}
	if len(purls) == 0 && len(githubURLs) == 0 {
		return fmt.Errorf("no valid PURLs or GitHub URLs found in file '%s'", filePath)
	}
	return processMixedContent(ctx, cfg, purls, githubURLs, opts)
}

// categorizeFileLines reads file and categorizes each line (unified function)
func categorizeFileLines(filename string, opts ProcessingOptions) (purls []string, githubURLs []string, err error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open file '%s': %w", filename, err)
	}
	defer func() {
		if cerr := file.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		// Apply pre-filter: skip until start
		if opts.LineStart > 0 && lineNum < opts.LineStart {
			continue
		}
		if opts.LineEnd > 0 && lineNum > opts.LineEnd {
			break // early stop once beyond end
		}

		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and comments (still count in line numbers above)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "pkg:") {
			purls = append(purls, line)
		} else if common.IsValidGitHubURL(line) {
			githubURLs = append(githubURLs, line)
		} else {
			slog.Warn("Unsupported line format",
				"file", filename,
				"line", lineNum,
				"content", line,
				"suggestion", "Expected PURL (pkg:) or GitHub URL format")
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("error reading file '%s': %w", filename, err)
	}

	return purls, githubURLs, nil
}

// ProcessingInputs contains validated and preprocessed inputs for processing
type ProcessingInputs struct {
	SupportedPURLs  []string
	SkippedPURLs    []string
	ValidGitHubURLs []string
	ProcessingCtx   context.Context
	CancelFunc      context.CancelFunc
}

// PURLProcessingResult contains the result of PURL processing
type PURLProcessingResult struct {
	SupportedPURLs []string
	SkippedPURLs   []string
}

// GitHubURLProcessingResult contains the result of GitHub URL processing
type GitHubURLProcessingResult struct {
	ValidGitHubURLs []string
}

// processMixedContent handles both direct input and file-based processing uniformly
//
// DDD Layer: Interface (unified processing entry point)
// Dependencies: Application layer services
// Reuses: All existing batch processing optimizations
func processMixedContent(ctx context.Context, cfg *domaincfg.Config, purls []string, githubURLs []string, options ProcessingOptions) error {
	// Normalize ecosystem filter (trim + lowercase) once at the entry point.
	options.Ecosystem = strings.ToLower(strings.TrimSpace(options.Ecosystem))

	// Global Maven collapsed coordinate normalization
	if len(purls) > 0 {
		allowMavenNorm := options.Ecosystem == "" || strings.EqualFold(options.Ecosystem, "maven")
		if allowMavenNorm {
			collapsedFixed := 0
			alreadyCanonical := 0
			for i, raw := range purls {
				if !strings.HasPrefix(raw, "pkg:maven/") {
					continue
				}
				norm := commonpurl.NormalizeMavenCollapsedCoordinates(raw)
				if norm == raw {
					// crude heuristic: count as already canonical if it has a '/' after prefix (namespace present)
					versionless := commonpurl.VersionlessPreserveCase(raw)
					core := strings.TrimPrefix(versionless, "pkg:maven/")
					if strings.Contains(core, "/") {
						alreadyCanonical++
					}
					continue
				}
				collapsedFixed++
				slog.Debug("purl_normalized", "original", raw, "normalized", norm)
				purls[i] = norm
			}
			if collapsedFixed > 0 {
				slog.Info("maven_norm_stats", "total", len(purls), "collapsed_fixed", collapsedFixed, "already_canonical", alreadyCanonical)
			}
		}
	}
	// Step 1: Validate and preprocess inputs
	inputs, err := validateAndPreprocessInputs(ctx, cfg, purls, githubURLs, options)
	if err != nil {
		return fmt.Errorf("input validation failed: %w", err)
	}
	defer func() {
		if inputs.CancelFunc != nil {
			inputs.CancelFunc()
		}
	}()

	// Step 2: Execute processing
	results, svc, err := executeProcessing(inputs.ProcessingCtx, cfg, inputs.SupportedPURLs, inputs.ValidGitHubURLs)
	if err != nil {
		return fmt.Errorf("processing failed: %w", err)
	}

	// Step 3: Display results
	if err := displayResults(cfg, results, inputs, options); err != nil {
		return err
	}

	// Step 4: Print GitHub API rate limit summary (when at least one call was made)
	printGitHubRateLimitSummary(svc)
	return nil
}

// validateAndPreprocessInputs validates inputs and applies filtering, sampling, and validation
func validateAndPreprocessInputs(ctx context.Context, cfg *domaincfg.Config, purls []string, githubURLs []string, options ProcessingOptions) (*ProcessingInputs, error) {
	inputs := &ProcessingInputs{}

	// Process PURLs if any
	purlResult, err := processPURLInputs(purls, cfg, options)
	if err != nil {
		return nil, fmt.Errorf("failed to process PURL inputs: %w", err)
	}
	inputs.SupportedPURLs = purlResult.SupportedPURLs
	inputs.SkippedPURLs = purlResult.SkippedPURLs

	// Process GitHub URLs if any
	githubResult, err := processGitHubURLInputs(githubURLs, cfg, options)
	if err != nil {
		return nil, fmt.Errorf("failed to process GitHub URL inputs: %w", err)
	}
	inputs.ValidGitHubURLs = githubResult.ValidGitHubURLs

	// Validate that we have some valid inputs after processing
	if len(inputs.ValidGitHubURLs) == 0 && len(inputs.SupportedPURLs) == 0 {
		err := common.NewValidationError("no valid PURLs or GitHub URLs found after filtering").
			WithContext("processed_purls", len(inputs.SupportedPURLs)).
			WithContext("processed_github_urls", len(inputs.ValidGitHubURLs))
		err.LogError()
		return nil, err
	}

	// Configure authentication and warnings
	if err := configureAuthentication(inputs, cfg, options); err != nil {
		authErr := common.NewAuthenticationError("authentication configuration failed", err).
			WithContext("github_urls_count", len(inputs.ValidGitHubURLs))
		authErr.LogError()
		return nil, authErr
	}

	// Setup processing context
	if err := setupProcessingContext(inputs, ctx, cfg, options); err != nil {
		contextErr := common.NewConfigError("context setup failed", err).
			WithContext("max_purls", cfg.App.MaxPurls)
		contextErr.LogError()
		return nil, contextErr
	}

	// Display processing start information
	displayProcessingStartInfo(inputs, options)

	return inputs, nil
}

// processPURLInputs handles PURL-specific filtering, sampling, and validation
func processPURLInputs(purls []string, cfg *domaincfg.Config, options ProcessingOptions) (*PURLProcessingResult, error) {
	result := &PURLProcessingResult{}

	if len(purls) == 0 {
		return result, nil
	}

	// Filter package types
	supportedPURLs, skippedPURLs := filterPackageTypes(purls)
	result.SupportedPURLs = supportedPURLs
	result.SkippedPURLs = skippedPURLs

	// Apply ecosystem filter (optional) before sampling and limits
	if options.Ecosystem != "" {
		eco := strings.ToLower(strings.TrimSpace(options.Ecosystem))
		if !commonpurl.IsEcosystemSupported(eco) {
			return nil, common.NewValidationError("unsupported ecosystem filter").WithContext("ecosystem", eco)
		}
		parser := commonpurl.NewParser()
		filtered := make([]string, 0, len(result.SupportedPURLs))
		for _, s := range result.SupportedPURLs {
			parsed, err := parser.Parse(s)
			if err != nil {
				// Skip unparsable here; it would fail later anyway
				continue
			}
			if strings.EqualFold(parsed.GetEcosystem(), eco) {
				filtered = append(filtered, s)
			}
		}
		result.SupportedPURLs = filtered
	}

	// Apply random sampling only for file mode and when specified (after ecosystem filtering)
	if !options.IsDirectInput && options.SampleSize > 0 && options.SampleSize < len(result.SupportedPURLs) {
		originalCount := len(result.SupportedPURLs)
		result.SupportedPURLs = randomSample(result.SupportedPURLs, options.SampleSize)
		fmt.Printf("🎲 Random sampling: %d PURLs selected from %d total\n", len(result.SupportedPURLs), originalCount)
	} else if !options.IsDirectInput && options.SampleSize > 0 {
		fmt.Printf("📝 Sample size (%d) >= total PURLs (%d), processing all\n", options.SampleSize, len(result.SupportedPURLs))
	}

	// Check PURL limits (only for file mode)
	if !options.IsDirectInput && len(result.SupportedPURLs) > cfg.App.MaxPurls {
		return nil, fmt.Errorf("too many PURLs specified: %d (max allowed: %d)", len(result.SupportedPURLs), cfg.App.MaxPurls)
	}

	return result, nil
}

// processGitHubURLInputs handles GitHub URL-specific sampling and validation
func processGitHubURLInputs(githubURLs []string, cfg *domaincfg.Config, options ProcessingOptions) (*GitHubURLProcessingResult, error) {
	result := &GitHubURLProcessingResult{}

	if len(githubURLs) == 0 {
		return result, nil
	}

	result.ValidGitHubURLs = githubURLs // Already validated

	// Apply random sampling to GitHub URLs only in file mode
	if !options.IsDirectInput && options.SampleSize > 0 && options.SampleSize < len(result.ValidGitHubURLs) {
		originalGitHubCount := len(result.ValidGitHubURLs)
		result.ValidGitHubURLs = randomSample(result.ValidGitHubURLs, options.SampleSize)
		fmt.Printf("🎲 Random sampling: %d GitHub URLs selected from %d total\n", len(result.ValidGitHubURLs), originalGitHubCount)
	} else if !options.IsDirectInput && options.SampleSize > 0 {
		fmt.Printf("📝 Sample size (%d) >= total GitHub URLs (%d), processing all\n", options.SampleSize, len(result.ValidGitHubURLs))
	}

	return result, nil
}

// configureAuthentication handles GitHub token validation and warnings
func configureAuthentication(inputs *ProcessingInputs, cfg *domaincfg.Config, options ProcessingOptions) error {
	if cfg.GitHub.Token != "" {
		return nil
	}

	totalInputs := len(inputs.ValidGitHubURLs) + len(inputs.SupportedPURLs)
	if totalInputs == 0 {
		return nil
	}

	fmt.Fprintf(os.Stderr, "\nNOTE: GITHUB_TOKEN is not set — analysis uses deps.dev and scorecard data only.\n")
	fmt.Fprintf(os.Stderr, "  For higher precision (commit history, archive detection): set GITHUB_TOKEN in .env or run 'gh auth login'\n\n")

	return nil
}

// setupProcessingContext handles context management for processing
func setupProcessingContext(inputs *ProcessingInputs, ctx context.Context, cfg *domaincfg.Config, options ProcessingOptions) error {
	if options.IsDirectInput {
		// For direct input, use the provided context
		inputs.ProcessingCtx = ctx
	} else {
		// For file processing, create context with extended timeout
		batchTimeout := time.Duration(cfg.App.TimeoutSeconds) * time.Second
		if batchTimeout > 0 {
			processingCtx, cancel := context.WithTimeout(context.Background(), batchTimeout)
			inputs.ProcessingCtx = processingCtx
			inputs.CancelFunc = cancel

			slog.Info("Batch processing started with timeout",
				"timeout", batchTimeout,
				"purls", len(inputs.SupportedPURLs),
				"github_urls", len(inputs.ValidGitHubURLs))
		} else {
			inputs.ProcessingCtx = context.Background()
		}
	}

	return nil
}

// displayProcessingStartInfo displays information about the processing that's about to start
func displayProcessingStartInfo(inputs *ProcessingInputs, options ProcessingOptions) {
	if options.IsDirectInput {
		return
	}
	slog.Info("Processing mixed file",
		"file", options.Filename,
		"purls", len(inputs.SupportedPURLs),
		"github_urls", len(inputs.ValidGitHubURLs))
	if options.LineStart > 0 || options.LineEnd > 0 {
		if options.LineEnd == 0 {
			slog.Debug("Applying line range", "start", options.LineStart, "end", "EOF")
		} else {
			slog.Debug("Applying line range", "start", options.LineStart, "end", options.LineEnd)
		}
	}
}

// ProcessingResults contains the results from processing operations
type ProcessingResults struct {
	AllAnalyses map[string]*analysispkg.Analysis
}

// ProcessingOptions govern how batch or direct processing behaves.
type ProcessingOptions struct {
	SampleSize       int
	Filename         string
	IsDirectInput    bool
	OnlyReviewNeeded bool
	OnlyEOL          bool
	Ecosystem        string
	LicenseCSVPath   string // optional path for license analysis CSV export (empty => skip)
	// LineStart and LineEnd define an optional 1-based inclusive line range filter for file mode.
	// If both are zero, no range filtering is applied. If LineEnd is zero, it means EOF.
	LineStart int
	LineEnd   int
}

// executeProcessing performs the actual processing of PURLs and GitHub URLs concurrently.
// It returns both the results and the AnalysisService so callers can inspect
// infrastructure state (e.g. GitHub API rate limits) after processing completes.
func executeProcessing(ctx context.Context, cfg *domaincfg.Config, supportedPURLs []string, validGitHubURLs []string) (*ProcessingResults, *application.AnalysisService, error) {
	// Process both types using concurrent processing (unified for all modes)
	analysisService := createAnalysisService(cfg)
	// For simplicity (and to retain diagnostics from a single evaluator instance), process sequentially.
	purlResults := make(map[string]*analysispkg.Analysis)
	githubResults := make(map[string]*analysispkg.Analysis)
	if len(supportedPURLs) > 0 {
		res, err := analysisService.ProcessBatchPURLs(ctx, supportedPURLs)
		if err != nil {
			return nil, nil, fmt.Errorf("error processing PURLs: %w", err)
		}
		purlResults = res
	}
	if len(validGitHubURLs) > 0 {
		res, err := analysisService.ProcessBatchGitHubURLs(ctx, validGitHubURLs)
		if err != nil {
			return nil, nil, fmt.Errorf("error processing GitHub URLs: %w", err)
		}
		githubResults = res
	}
	allAnalyses := collections.MergeMaps(purlResults, githubResults)
	return &ProcessingResults{AllAnalyses: allAnalyses}, analysisService, nil
}

// displayResults handles the display of processing results and statistics.
// Mode is already determined by the caller (ProcessDirectMode / ProcessFileMode)
// via options.IsDirectInput — no secondary dispatch needed.
func displayResults(cfg *domaincfg.Config, results *ProcessingResults, inputs *ProcessingInputs, options ProcessingOptions) error {
	// Single point of skipped PURL logging (start-phase duplicate removed per noise reduction policy)
	if len(inputs.SkippedPURLs) > 0 {
		slog.Debug("Skipped unsupported package types", "count", len(inputs.SkippedPURLs))
		for _, p := range inputs.SkippedPURLs {
			slog.Debug("Skipped PURL", "purl", p, "reason", "unsupported package type")
		}
	}

	// Display mode-specific output
	if options.IsDirectInput {
		displayDirectSummary(inputs, results, options)
	} else {
		displayBatchFileSummary(inputs, results, options)
	}

	// Post-display hooks (common to both modes)
	displayBatchErrors(results.AllAnalyses)

	if strings.TrimSpace(options.LicenseCSVPath) == "" {
		displayBatchAnalysesSummary(results.AllAnalyses)
	}

	if strings.ToLower(os.Getenv("LOG_LEVEL")) == "debug" {
		printReviewNeededArgs(results.AllAnalyses)
	}

	// CSV export (file mode only)
	if !options.IsDirectInput {
		analysisService := createAnalysisService(cfg)
		if err := analysisService.WriteScoreCardCSV(results.AllAnalyses, "scorecard.csv"); err != nil {
			slog.Error("Failed to write CSV file", "error", err)
		}
		if strings.TrimSpace(options.LicenseCSVPath) != "" {
			if err := analysisService.WriteLicenseCSV(results.AllAnalyses, options.LicenseCSVPath); err != nil {
				slog.Error("Failed to write license CSV file", "error", err, "path", options.LicenseCSVPath)
			} else {
				fmt.Printf("\n📄 License CSV written: %s\n", options.LicenseCSVPath)
				slog.Info("License CSV exported", "path", options.LicenseCSVPath)
			}
		}
	}
	return nil
}

// displayDirectSummary prints a compact summary for direct (non-file) mode.
func displayDirectSummary(inputs *ProcessingInputs, results *ProcessingResults, opts ProcessingOptions) {
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
	if opts.ShouldShowPerPURLDetails() {
		displayBatchAnalysesFull(results.AllAnalyses, opts)
	}
}

// displayBatchFileSummary prints a summary header for file-based batch mode.
func displayBatchFileSummary(inputs *ProcessingInputs, results *ProcessingResults, opts ProcessingOptions) {
	fmt.Print("\n" + strings.Repeat("=", separatorLength) + "\n")
	if len(inputs.SupportedPURLs) > 0 && len(inputs.ValidGitHubURLs) > 0 {
		fmt.Printf("📊 MIXED FILE BATCH ANALYSIS RESULTS\n")
		fmt.Printf("📝 PURLs: %d | GitHub URLs: %d | Total: %d\n", len(inputs.SupportedPURLs), len(inputs.ValidGitHubURLs), len(results.AllAnalyses))
	} else if len(inputs.SupportedPURLs) > 0 {
		fmt.Printf("📊 INDIVIDUAL PURL ANALYSIS RESULTS\n")
	} else if len(inputs.ValidGitHubURLs) > 0 {
		fmt.Printf("📊 GITHUB URL BATCH ANALYSIS RESULTS\n")
	}
	fmt.Print(strings.Repeat("=", separatorLength) + "\n")
	if opts.ShouldShowPerPURLDetails() {
		displayBatchAnalysesFull(results.AllAnalyses, opts)
	}
}

// printGitHubRateLimitSummary prints remaining GitHub API quota and reset time
// if at least one GitHub GraphQL API call was made during this execution.
func printGitHubRateLimitSummary(svc *application.AnalysisService) {
	if svc == nil {
		return
	}
	remaining, resetAt := svc.GitHubClient().RateLimitSummary()
	if resetAt == "" {
		return
	}
	resetLocal := resetAt
	if t, err := time.Parse(time.RFC3339, resetAt); err == nil {
		resetLocal = t.Local().Format("15:04 MST")
	}
	fmt.Printf("📊 GitHub API: remaining=%d, resets at %s\n", remaining, resetLocal)
}

// printReviewNeededArgs prints "Review Needed" PURLs as a VS Code launch.json args block.
// It prefers the package PURL when available; otherwise falls back to the map key.
func printReviewNeededArgs(analyses map[string]*analysispkg.Analysis) {
	if len(analyses) == 0 {
		return
	}

	// Collect PURLs that resulted in "Review Needed" or had no lifecycle assessment (treated as Review Needed in CLI).
	var purls []string
	for key, a := range analyses {
		if a == nil || a.Error != nil {
			continue
		}
		isReviewNeeded := false
		if a.AxisResults == nil || a.AxisResults[analysispkg.LifecycleAxis] == nil {
			isReviewNeeded = true
		} else if a.AxisResults[analysispkg.LifecycleAxis].Label == analysispkg.LabelReviewNeeded {
			isReviewNeeded = true
		}
		if !isReviewNeeded {
			continue
		}
		candidate := key
		if a.Package != nil && a.Package.PURL != "" {
			candidate = a.Package.PURL
		}
		purls = append(purls, candidate)
	}

	if len(purls) == 0 {
		return
	}
	sort.Strings(purls)

	// Print in a launch.json-friendly format
	fmt.Printf("\n🧪 Debug: Review Needed PURLs (paste into launch.json \"args\")\n")
	for _, p := range purls {
		fmt.Printf("                \"%s\",\n", p)
	}
}

// ============================================================================
// Output selection
// ============================================================================

// displayBatchAnalysesFull displays individual analysis results (legacy full output)
func displayBatchAnalysesFull(analyses map[string]*analysispkg.Analysis, options ProcessingOptions) {
	purlCount := 0

	shouldShow := func(label string) bool {
		if options.OnlyReviewNeeded || options.OnlyEOL {
			allowed := false
			if options.OnlyReviewNeeded {
				if label == "" || label == string(analysispkg.LabelReviewNeeded) {
					allowed = true
				}
			}
			if options.OnlyEOL {
				if label == string(analysispkg.LabelEOLConfirmed) || label == string(analysispkg.LabelEOLEffective) || label == string(analysispkg.LabelEOLScheduled) {
					allowed = true
				}
			}
			return allowed
		}
		return true
	}

	type item struct {
		key string
		a   *analysispkg.Analysis
	}
	var actives, stalled, eols, reviews, others []item
	for key, a := range analyses {
		if a == nil || a.Error != nil {
			continue
		}
		lbl := ""
		if a.AxisResults != nil && a.AxisResults[analysispkg.LifecycleAxis] != nil {
			lbl = string(a.AxisResults[analysispkg.LifecycleAxis].Label)
		}
		if !shouldShow(lbl) {
			continue
		}
		switch lbl {
		case string(analysispkg.LabelActive):
			actives = append(actives, item{key, a})
		case string(analysispkg.LabelStalled):
			stalled = append(stalled, item{key, a})
		case string(analysispkg.LabelEOLConfirmed), string(analysispkg.LabelEOLEffective), string(analysispkg.LabelEOLScheduled):
			eols = append(eols, item{key, a})
		case string(analysispkg.LabelReviewNeeded), "":
			reviews = append(reviews, item{key, a})
		default:
			others = append(others, item{key, a})
		}
	}
	// Sort each bucket by key for stable output
	sortByKey := func(items []item) {
		sort.Slice(items, func(i, j int) bool { return items[i].key < items[j].key })
	}
	for _, bucket := range [][]item{actives, stalled, eols, reviews, others} {
		sortByKey(bucket)
	}

	// Ordered printing
	for _, bucket := range [][]item{actives, stalled, eols, reviews, others} {
		for _, it := range bucket {
			printFullAnalysis(os.Stdout, it.key, it.a, &purlCount)
		}
	}

	if purlCount == 0 {
		fmt.Printf("⚠️  No valid results to display\n")
	} else {
		fmt.Printf("\n📊 Displayed %d individual results\n", purlCount)
	}
}

// printFullAnalysis orchestrates printing of a single analysis entry.
func printFullAnalysis(w io.Writer, purl string, analysis *analysispkg.Analysis, counter *int) {
	*counter++
	fmt.Fprintf(w, "\n--- PURL %d ---\n", *counter)
	printHeader(w, purl, analysis)
	printLifecycle(w, analysis)
	printEOLEvidence(w, analysis)
	printEOLCatalog(w, analysis)
	printRepoHint(w, analysis)
	printRepoState(w, analysis)
	printDependentCount(w, analysis)
	printScores(w, analysis)
	printReleaseInfo(w, analysis)
	printRequestedVersion(w, analysis)
	printLicenses(w, analysis)
	printCommitActivity(w, analysis)
	printRepositoryLinks(w, analysis)
}

func printHeader(w io.Writer, original string, a *analysispkg.Analysis) {
	displayPackage := original
	if a != nil {
		if dp := a.DisplayPURL(); dp != "" && dp != original {
			displayPackage = dp
		}
	}
	fmt.Fprintf(w, "📦 Package: %s\n", displayPackage)
	if a.Repository != nil && a.Repository.Description != "" {
		if desc := truncateDescription(a.Repository.Description); desc != "" {
			fmt.Fprintf(w, "🧾 Description: %s\n", desc)
		}
	}
	if a.PackageLinks != nil {
		if a.PackageLinks.HomepageURL != "" {
			fmt.Fprintf(w, "   🔗 Homepage: %s\n", a.PackageLinks.HomepageURL)
		}
		if a.PackageLinks.RegistryURL != "" {
			fmt.Fprintf(w, "   🗃 Registry: %s\n", a.PackageLinks.RegistryURL)
		}
	}
}

func printLifecycle(w io.Writer, a *analysispkg.Analysis) {
	if a.AxisResults != nil && a.AxisResults[analysispkg.LifecycleAxis] != nil {
		res := a.AxisResults[analysispkg.LifecycleAxis]
		fmt.Fprintf(w, "⚖️  Result: %s\n", common.ColorizeResult(string(res.Label)))
		fmt.Fprintf(w, "💭 Reason: %s\n", res.Reason)
		if strings.EqualFold(os.Getenv("LOG_LEVEL"), "debug") && len(res.Trace) > 0 {
			for i, step := range res.Trace {
				fmt.Fprintf(w, "   🧪 Trace[%d]: %s\n", i, step)
			}
		}
	} else {
		fmt.Fprintf(w, "⚖️  Result: %s\n", common.ColorizeResult("Review Needed"))
		fmt.Fprintf(w, "💭 Reason: %s\n", "No lifecycle assessment available")
	}
}

func printEOLEvidence(w io.Writer, a *analysispkg.Analysis) {
	if len(a.EOL.Evidences) == 0 {
		return
	}
	fmt.Fprintf(w, "📚 EOL Evidence (%d):\n", len(a.EOL.Evidences))
	for _, ev := range a.EOL.Evidences {
		if ev.Source != "" {
			fmt.Fprintf(w, "   • [%s] %s", ev.Source, ev.Summary)
		} else {
			fmt.Fprintf(w, "   • %s", ev.Summary)
		}
		if ev.Confidence > 0 {
			fmt.Fprintf(w, " (confidence %.2f)", ev.Confidence)
		}
		fmt.Fprintf(w, "\n")
		if strings.TrimSpace(ev.Reference) != "" {
			fmt.Fprintf(w, "      ↳ %s\n", ev.Reference)
		}
	}
}

func printEOLCatalog(w io.Writer, a *analysispkg.Analysis) {
	// Simplified: only show planned date and successor (catalog struct removed)
	if a.EOL.ScheduledAt != nil && a.EOL.State == analysispkg.EOLScheduled {
		fmt.Fprintf(w, "🌅 Scheduled EOL: %s\n", a.EOL.ScheduledAt.Format(dateFormat))
	}
	if a.EOL.Successor != "" {
		fmt.Fprintf(w, "🔁 Successor: %s\n", a.EOL.Successor)
	}
	if a.EOL.Reason != "" {
		fmt.Fprintf(w, "📝 Catalog Reason: %s\n", a.EOL.Reason)
	}
}

func printRepoHint(w io.Writer, a *analysispkg.Analysis) {
	if a.RepoURL == "" {
		fmt.Fprintf(w, "🔎 Hint: No repository URL was found from deps.dev links; Scorecard data cannot be retrieved.\n")
	}
}

func printRepoState(w io.Writer, a *analysispkg.Analysis) {
	if a.RepoURL == "" {
		return
	}
	fmt.Fprintf(w, "📊 GitHub Info: ")
	isArchived, isDisabled, isFork := false, false, false
	if a.RepoState != nil {
		isArchived = a.RepoState.IsArchived
		isDisabled = a.RepoState.IsDisabled
		isFork = a.RepoState.IsFork
	}
	if isArchived {
		fmt.Fprintf(w, "📦 Archived ")
	}
	if isDisabled {
		fmt.Fprintf(w, "⛔ Disabled ")
	}
	if isFork {
		if a.RepoState != nil && a.RepoState.ForkSource != "" {
			fmt.Fprintf(w, "🔀 Fork of %s ", a.RepoState.ForkSource)
		} else {
			fmt.Fprintf(w, "🔀 Fork ")
		}
	}
	if !isArchived && !isDisabled && !isFork {
		fmt.Fprintf(w, "Normal ")
	}
	if a.Repository != nil && a.Repository.StarsCount > 0 {
		fmt.Fprintf(w, "(⭐ %d stars)", a.Repository.StarsCount)
	}
	fmt.Fprintf(w, "\n")
}

func printDependentCount(w io.Writer, a *analysispkg.Analysis) {
	if a == nil {
		return
	}
	// CLI intentionally omits zero counts (unknown/unsupported ecosystem).
	// CSV always emits "0" for machine-readable consistency.
	if a.DependentCount > 0 {
		fmt.Fprintf(w, "👥 Used by: %d packages\n", a.DependentCount)
	}
	if a.DirectDepsCount > 0 || a.TransitiveDepsCount > 0 {
		fmt.Fprintf(w, "📦 Depends on: %d direct, %d transitive\n", a.DirectDepsCount, a.TransitiveDepsCount)
	}
}

func printScores(w io.Writer, a *analysispkg.Analysis) {
	if len(a.Scores) == 0 {
		return
	}
	fmt.Fprintf(w, "🏆 Overall Score: %.*f/10\n", scorePrecision, a.OverallScore)
	for name, scoreEntity := range a.Scores {
		if scoreEntity == nil {
			slog.Debug("Skipping nil score entity", "check", name)
			continue
		}
		if name == "Maintained" && scoreEntity.Value() >= 0 {
			fmt.Fprintf(w, "  🔧 Maintained: %.*f/10\n", scorePrecision, float64(scoreEntity.Value()))
		}
		if name == "Vulnerabilities" && scoreEntity.Value() >= 0 {
			fmt.Fprintf(w, "  🛡️ Vulnerabilities: %.*f/10\n", scorePrecision, float64(scoreEntity.Value()))
		}
	}
}

func printReleaseInfo(w io.Writer, a *analysispkg.Analysis) {
	if a.ReleaseInfo == nil {
		return
	}
	if a.ReleaseInfo.StableVersion != nil && !a.ReleaseInfo.StableVersion.PublishedAt.IsZero() {
		stable := a.ReleaseInfo.StableVersion
		deprecatedTag := ""
		if stable.IsDeprecated {
			deprecatedTag = " [DEPRECATED]"
		}
		fmt.Fprintf(w, "📦 Latest Stable Release: %s (%s)%s\n", stable.Version, stable.PublishedAt.Format(dateFormat), deprecatedTag)
		if stable.RegistryURL != "" {
			fmt.Fprintf(w, "   ↳ Version Page: %s\n", stable.RegistryURL)
		}
		advCount := len(stable.Advisories)
		if advCount > 0 {
			fmt.Fprintf(w, "   ↳ Stable Advisories: %d\n", advCount)
			for _, adv := range stable.Advisories {
				fmt.Fprintf(w, "      • [%s] %s (%s)\n", adv.Source, adv.ID, adv.URL)
			}
		} else {
			fmt.Fprintf(w, "   ↳ Stable Advisories: 0\n")
		}
	}
	if a.ReleaseInfo.PreReleaseVersion != nil && !a.ReleaseInfo.PreReleaseVersion.PublishedAt.IsZero() {
		pre := a.ReleaseInfo.PreReleaseVersion
		deprecatedTag := ""
		if pre.IsDeprecated {
			deprecatedTag = " [DEPRECATED]"
		}
		fmt.Fprintf(w, "📦 Latest Pre-release: %s (%s)%s\n", pre.Version, pre.PublishedAt.Format(dateFormat), deprecatedTag)
		if pre.RegistryURL != "" {
			fmt.Fprintf(w, "   ↳ Version Page: %s\n", pre.RegistryURL)
		}
	}
	if a.ReleaseInfo.MaxSemverVersion != nil && a.ReleaseInfo.MaxSemverVersion.Version != "" {
		maxv := a.ReleaseInfo.MaxSemverVersion
		deprecatedTag := ""
		if maxv.IsDeprecated {
			deprecatedTag = " [DEPRECATED]"
		}
		if !maxv.PublishedAt.IsZero() {
			fmt.Fprintf(w, "📦 Highest Version (SemVer): %s (%s)%s\n", maxv.Version, maxv.PublishedAt.Format(dateFormat), deprecatedTag)
		} else {
			fmt.Fprintf(w, "📦 Highest Version (SemVer): %s%s\n", maxv.Version, deprecatedTag)
		}
		if maxv.RegistryURL != "" {
			fmt.Fprintf(w, "   ↳ Version Page: %s\n", maxv.RegistryURL)
		}
	}
}

func printRequestedVersion(w io.Writer, a *analysispkg.Analysis) {
	if a.ReleaseInfo == nil || a.ReleaseInfo.RequestedVersion == nil || a.ReleaseInfo.RequestedVersion.PublishedAt.IsZero() {
		return
	}
	rv := a.ReleaseInfo.RequestedVersion
	deprecatedTag := ""
	if rv.IsDeprecated {
		deprecatedTag = " [DEPRECATED]"
	}
	fmt.Fprintf(w, "📋 Requested Version: %s (%s)%s\n", rv.Version, rv.PublishedAt.Format(dateFormat), deprecatedTag)
	if rv.RegistryURL != "" {
		fmt.Fprintf(w, "   ↳ Version Page: %s\n", rv.RegistryURL)
	}
}

func printLicenses(w io.Writer, a *analysispkg.Analysis) {
	proj := a.ProjectLicense
	reqs := a.RequestedVersionLicenses
	if proj.IsZero() && len(reqs) == 0 {
		return
	}
	collapse := proj.Identifier != "" && len(reqs) == 1 && strings.EqualFold(proj.Identifier, reqs[0].Identifier)
	if collapse {
		if proj.Source != "" {
			fmt.Fprintf(w, "📄 License: %s (source: %s / %s)\n", proj.Identifier, proj.Source, reqs[0].Source)
		} else {
			fmt.Fprintf(w, "📄 License: %s\n", proj.Identifier)
		}
		return
	}
	fmt.Fprintf(w, "📄 Licenses:\n")
	if proj.Identifier != "" {
		if proj.Source != "" {
			fmt.Fprintf(w, "   Project: %s (source: %s)\n", proj.Identifier, proj.Source)
		} else {
			fmt.Fprintf(w, "   Project: %s\n", proj.Identifier)
		}
	} else if proj.IsNonStandard() && proj.Raw != "" {
		fmt.Fprintf(w, "   Project: (non-standard raw=%s source=%s)\n", proj.Raw, proj.Source)
	} else if proj.IsZero() {
		fmt.Fprintf(w, "   Project: (not detected)\n")
	} else {
		fmt.Fprintf(w, "   Project: (unclassified source=%s raw=%s)\n", proj.Source, proj.Raw)
	}
	if len(reqs) > 0 {
		allSameSource := true
		firstSource := reqs[0].Source
		for _, rl := range reqs {
			if rl.Source != firstSource {
				allSameSource = false
				break
			}
		}
		if allSameSource {
			ids := make([]string, 0, len(reqs))
			for _, rl := range reqs {
				ids = append(ids, rl.Identifier)
			}
			if firstSource != "" {
				fmt.Fprintf(w, "   Requested Version: %s (source: %s)\n", strings.Join(ids, ", "), firstSource)
			} else {
				fmt.Fprintf(w, "   Requested Version: %s\n", strings.Join(ids, ", "))
			}
		} else {
			for i, rl := range reqs {
				if rl.Source != "" {
					fmt.Fprintf(w, "   Requested Version[%d]: %s (source: %s)\n", i, rl.Identifier, rl.Source)
				} else {
					fmt.Fprintf(w, "   Requested Version[%d]: %s\n", i, rl.Identifier)
				}
			}
		}
	} else {
		fmt.Fprintf(w, "   Requested Version: (none)\n")
	}
}

func printCommitActivity(w io.Writer, a *analysispkg.Analysis) {
	if a.RepoState != nil && a.RepoState.LatestHumanCommit != nil && !a.RepoState.LatestHumanCommit.IsZero() {
		fmt.Fprintf(w, "💻 Latest Commit: %s\n", a.RepoState.LatestHumanCommit.Format(dateFormat))
	}
}

func printRepositoryLinks(w io.Writer, a *analysispkg.Analysis) {
	if a.RepoURL != "" {
		repoURL := a.RepoURL
		if !strings.HasPrefix(repoURL, "http://") && !strings.HasPrefix(repoURL, "https://") {
			repoURL = "https://" + repoURL
		}
		fmt.Fprintf(w, "🔗 Repository: %s\n", repoURL)
	}
	if a.ScorecardURL != "" {
		fmt.Fprintf(w, "🔗 Scorecard: %s\n", a.ScorecardURL)
	}
	if a.ScorecardAPIURL != "" {
		fmt.Fprintf(w, "🔗 Scorecard API: %s\n", a.ScorecardAPIURL)
	}
}

// displayBatchAnalysesSummary displays summary statistics for batch processing results from domain.Analysis
func displayBatchAnalysesSummary(analyses map[string]*analysispkg.Analysis) {
	fmt.Print("\n" + strings.Repeat("=", separatorLength) + "\n")
	fmt.Printf("📈 BATCH PROCESSING SUMMARY\n")
	fmt.Print(strings.Repeat("=", separatorLength) + "\n")

	// Count by labels and collect label-reason combinations
	labelCounts := make(map[string]int)
	labelReasons := make(map[string]map[string]int) // label -> reason -> count
	successfulCount := 0

	// Collect not-found packages
	var notFoundPURLs []string
	for key, analysis := range analyses {
		if analysis == nil {
			continue
		}
		if analysis.Error != nil {
			if common.IsResourceNotFoundError(analysis.Error) {
				p := analysis.DisplayPURL()
				if p == "" {
					p = key
				}
				notFoundPURLs = append(notFoundPURLs, p)
			}
			continue // Skip errors and unsupported package types entirely
		}

		successfulCount++

		if analysis.AxisResults != nil && analysis.AxisResults[analysispkg.LifecycleAxis] != nil {
			label := string(analysis.AxisResults[analysispkg.LifecycleAxis].Label)
			reason := analysis.AxisResults[analysispkg.LifecycleAxis].Reason

			labelCounts[label]++

			if labelReasons[label] == nil {
				labelReasons[label] = make(map[string]int)
			}
			labelReasons[label][reason]++
		}
	}
	sort.Strings(notFoundPURLs)
	notFoundCount := len(notFoundPURLs)

	// 1. Overall statistics first — gives the user the big picture
	fmt.Printf("\n📊 OVERALL STATISTICS:\n")
	fmt.Print(strings.Repeat("-", shortSeparatorLength) + "\n")
	fmt.Printf("  Total Input: %d packages\n", successfulCount+notFoundCount)
	fmt.Printf("  Evaluated: %d packages\n", successfulCount)
	if notFoundCount > 0 {
		fmt.Printf("  Not Found: %d packages\n", notFoundCount)
	}

	// 2. Not-found details (immediately after statistics)
	if notFoundCount > 0 {
		fmt.Printf("\n🔍 NOT FOUND in deps.dev: %d packages\n", notFoundCount)
		for _, p := range notFoundPURLs {
			fmt.Printf("   • %s\n", p)
		}
	}

	// Skip label/reason breakdown when no packages were evaluated
	if successfulCount == 0 {
		return
	}

	// Sort labels by count (most common first), then alphabetically for stable output
	type labelCount struct {
		label string
		count int
	}
	var sortedLabels []labelCount
	for label, count := range labelCounts {
		sortedLabels = append(sortedLabels, labelCount{label, count})
	}
	sort.Slice(sortedLabels, func(i, j int) bool {
		if sortedLabels[i].count != sortedLabels[j].count {
			return sortedLabels[i].count > sortedLabels[j].count
		}
		return sortedLabels[i].label < sortedLabels[j].label
	})

	// 3. Label summary
	fmt.Printf("\n🏷️  LABEL SUMMARY:\n")
	fmt.Print(strings.Repeat("-", shortSeparatorLength) + "\n")
	for _, lc := range sortedLabels {
		percentage := float64(lc.count) / float64(successfulCount) * 100
		fmt.Printf("  %s: %d packages (%.1f%%)\n", common.ColorizeResult(lc.label), lc.count, percentage)
	}

	// 4. Reasons grouped by label
	fmt.Printf("\n💭 REASONS BY LABEL:\n")
	fmt.Print(strings.Repeat("-", shortSeparatorLength) + "\n")

	for _, labelInfo := range sortedLabels {
		label := labelInfo.label
		totalForLabel := labelInfo.count

		fmt.Printf("\n  %s (%d packages):\n", common.ColorizeResult(label), totalForLabel)

		// Sort reasons within this label by count
		type reasonCount struct {
			reason string
			count  int
		}
		var sortedReasons []reasonCount
		for reason, count := range labelReasons[label] {
			sortedReasons = append(sortedReasons, reasonCount{reason, count})
		}
		sort.Slice(sortedReasons, func(i, j int) bool {
			if sortedReasons[i].count != sortedReasons[j].count {
				return sortedReasons[i].count > sortedReasons[j].count
			}
			return sortedReasons[i].reason < sortedReasons[j].reason
		})

		for _, reasonInfo := range sortedReasons {
			percentage := float64(reasonInfo.count) / float64(successfulCount) * 100
			fmt.Printf("    • %s (%d packages, %.1f%%)\n", reasonInfo.reason, reasonInfo.count, percentage)
		}
	}

	// Extra: Surface README-based EOL candidates to allow quick manual verification
	// We list all evidences where Source == "GitHubREADME".
	var readmeHits []struct{ pkg, url, phrase string }
	for pkg, analysis := range analyses {
		if analysis == nil || analysis.Error != nil {
			continue
		}
		if analysis.EOL.Evidences == nil {
			continue
		}
		for _, ev := range analysis.EOL.Evidences {
			if ev.Source == "GitHubREADME" && ev.Reference != "" {
				// Phrase is present in ev.Summary as "... (phrase)"
				phrase := ""
				s := ev.Summary
				if i := strings.LastIndex(s, "("); i >= 0 && strings.HasSuffix(s, ")") && i+1 < len(s)-1 {
					phrase = s[i+1 : len(s)-1]
				}
				pkgDisplay := pkg
				if dp := analysis.DisplayPURL(); dp != "" {
					pkgDisplay = dp
				}
				readmeHits = append(readmeHits, struct{ pkg, url, phrase string }{pkgDisplay, ev.Reference, phrase})
			}
		}
	}
	sort.Slice(readmeHits, func(i, j int) bool {
		return readmeHits[i].pkg < readmeHits[j].pkg
	})
	if len(readmeHits) > 0 {
		fmt.Print("\n" + strings.Repeat("-", separatorLength) + "\n")
		fmt.Printf("🔎 README-BASED EOL CANDIDATES (%d)\n", len(readmeHits))
		fmt.Print(strings.Repeat("-", separatorLength) + "\n")
		for i, h := range readmeHits {
			if h.phrase != "" {
				fmt.Printf("%d. %s\n   ↳ %s\n   phrase: \"%s\"\n", i+1, h.pkg, h.url, h.phrase)
			} else {
				fmt.Printf("%d. %s\n   ↳ %s\n", i+1, h.pkg, h.url)
			}
		}
	}
}

// displayBatchErrors displays processing errors for failed analyses
func displayBatchErrors(analyses map[string]*analysispkg.Analysis) {
	type batchError struct {
		url    string
		errMsg string
		err    error
	}

	// Collect all errors, categorizing auth errors separately
	var authCount int
	var otherErrors []batchError
	for url, analysis := range analyses {
		if analysis == nil || analysis.Error == nil {
			continue
		}
		// Skip Not Found here; shown in a dedicated section
		if common.IsResourceNotFoundError(analysis.Error) {
			continue
		}
		if common.IsAuthenticationError(analysis.Error) {
			authCount++
			continue
		}
		otherErrors = append(otherErrors, batchError{
			url:    url,
			errMsg: analysis.Error.Error(),
			err:    analysis.Error,
		})
	}
	sort.Slice(otherErrors, func(i, j int) bool {
		return otherErrors[i].url < otherErrors[j].url
	})

	totalErrors := authCount + len(otherErrors)
	if totalErrors == 0 {
		return
	}

	fmt.Print("\n" + strings.Repeat("!", separatorLength) + "\n")
	fmt.Printf("❌ PROCESSING ERRORS (%d failed)\n", totalErrors)
	fmt.Print(strings.Repeat("!", separatorLength) + "\n")

	// Show auth errors as a single summary instead of listing each one
	if authCount > 0 {
		fmt.Printf("\n🔑 GitHub authentication failed (%d packages affected)\n", authCount)
		fmt.Printf("   Set a valid GITHUB_TOKEN in .env or run 'gh auth login' to fix this.\n")
	}

	for i, e := range otherErrors {
		fmt.Printf("\n%d. 🔗 URL: %s\n", i+1, e.url)
		fmt.Printf("   ❌ Error: %s\n", e.errMsg)
	}

	if len(otherErrors) > 0 {
		fmt.Printf("\n💡 Common causes:\n")
		fmt.Printf("   • Repository not found or private\n")
		fmt.Printf("   • Package not available in deps.dev\n")
		fmt.Printf("   • Network connectivity issues\n")
		fmt.Printf("   • GitHub API rate limits\n")
	}
	fmt.Print(strings.Repeat("!", separatorLength) + "\n")
}

// purlHasVersion returns true if the PURL string contains a version component (i.e., has '@').
func purlHasVersion(p string) bool {
	// Strip qualifiers/fragment first to avoid false positives from '@' in qualifiers
	if qi := strings.Index(p, "?"); qi >= 0 {
		p = p[:qi]
	}
	return strings.Contains(p, "@")
}

// pickVersionedPURL selects a versioned PURL from an Analysis for use with version-requiring APIs.
// Priority: EffectivePURL (typically resolved with version) > OriginalPURL > construct from StableVersion.
// Returns "" if no versioned PURL is available.
func pickVersionedPURL(a *analysispkg.Analysis) string {
	if a == nil {
		return ""
	}
	// EffectivePURL is the resolved form, typically includes version
	if a.EffectivePURL != "" && purlHasVersion(a.EffectivePURL) {
		return a.EffectivePURL
	}
	// OriginalPURL may include a version if the user specified one
	if a.OriginalPURL != "" && purlHasVersion(a.OriginalPURL) {
		return a.OriginalPURL
	}
	// Fallback: construct a versioned PURL from base PURL + StableVersion
	if a.ReleaseInfo != nil && a.ReleaseInfo.StableVersion != nil && a.ReleaseInfo.StableVersion.Version != "" {
		base := a.OriginalPURL
		if base == "" {
			base = a.EffectivePURL
		}
		if base != "" {
			if versioned, err := commonpurl.WithVersion(base, a.ReleaseInfo.StableVersion.Version); err == nil {
				return versioned
			}
		}
	}
	return ""
}

// composerVendorNameFromPURL extracts vendor and package name from a composer PURL.
// E.g. "pkg:composer/monolog/monolog" -> ("monolog", "monolog")
// E.g. "pkg:composer/fzaninotto/faker@1.9.2" -> ("fzaninotto", "faker")
// Returns ("", "") if the PURL is not a valid composer PURL with vendor/name.
func composerVendorNameFromPURL(purl string) (string, string) {
	s := strings.TrimPrefix(purl, "pkg:")
	idx := strings.Index(s, "/")
	if idx < 0 {
		return "", ""
	}
	rest := s[idx+1:]
	// Strip version/qualifiers
	if vi := strings.Index(rest, "@"); vi > 0 {
		rest = rest[:vi]
	}
	if qi := strings.Index(rest, "?"); qi > 0 {
		rest = rest[:qi]
	}
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", ""
	}
	return parts[0], parts[1]
}
