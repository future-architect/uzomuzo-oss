package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	urfcli "github.com/urfave/cli/v3"

	"github.com/future-architect/uzomuzo-oss/internal/common/logging"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depgraph"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/treesitter"
	"github.com/future-architect/uzomuzo-oss/internal/interfaces/cli"
)

// dietJSONOutput mirrors the top-level JSON output structure for validation.
type dietJSONOutput struct {
	Summary struct {
		TotalDirect         int `json:"total_direct"`
		TotalTransitive     int `json:"total_transitive"`
		TransitiveOnlyByOne int `json:"transitive_only_by_one"`
		UnusedDirect        int `json:"unused_direct"`
		EasyWins            int `json:"easy_wins"`
		ActionableDirect    int `json:"actionable_direct"`
		StaysAsIndirect     int `json:"stays_as_indirect"`
	} `json:"summary"`
	Dependencies []struct {
		Rank            int     `json:"rank"`
		PURL            string  `json:"purl"`
		Name            string  `json:"name"`
		Version         string  `json:"version"`
		Ecosystem       string  `json:"ecosystem"`
		PriorityScore   float64 `json:"priority_score"`
		Difficulty      string  `json:"difficulty"`
		ImportFileCount int     `json:"import_file_count"`
		CallSiteCount   int     `json:"call_site_count"`
		IsUnused        bool    `json:"is_unused"`
	} `json:"dependencies"`
	SBOMPath   string `json:"sbom_path"`
	SourceRoot string `json:"source_root"`
	AnalyzedAt string `json:"analyzed_at"`
}

const testSBOMPath = "../../testdata/diet/test-sbom.json"

// sourceRoot is the project root (relative to this test file).
const sourceRoot = "../.."

// runDiet invokes the diet pipeline with the given format and captures stdout.
func runDiet(t *testing.T, format string) string {
	t.Helper()

	configService := config.NewConfigService()
	cfg, err := configService.Load(context.Background())
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	logging.Initialize(cfg.App.LogLevel)

	var buf bytes.Buffer
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = oldStdout
		_ = r.Close()
		_ = w.Close()
	})

	graphAnalyzer := depgraph.NewAnalyzer()
	sourceAnalyzer := treesitter.NewAnalyzer()

	opts := cli.DietOptions{
		SBOMPath:   testSBOMPath,
		SourceRoot: sourceRoot,
		Format:     format,
	}

	// Read from the pipe concurrently to avoid deadlock when output exceeds
	// the OS pipe buffer size.
	readErrCh := make(chan error, 1)
	go func() {
		_, readErr := buf.ReadFrom(r)
		readErrCh <- readErr
	}()

	runErr := cli.RunDiet(context.Background(), cfg, opts, graphAnalyzer, sourceAnalyzer)

	_ = w.Close()
	os.Stdout = oldStdout
	if readErr := <-readErrCh; readErr != nil {
		t.Fatalf("failed to read captured stdout: %v", readErr)
	}

	if runErr != nil {
		t.Fatalf("RunDiet(%s) failed: %v", format, runErr)
	}
	return buf.String()
}

func TestE2E_DietTable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	out := runDiet(t, "table")

	// Should contain the header line
	if !strings.Contains(out, "Diet Plan") {
		t.Error("table output missing 'Diet Plan' header")
	}

	// Should contain column headers
	for _, col := range []string{"RANK", "SCORE", "EFFORT", "PURL"} {
		if !strings.Contains(out, col) {
			t.Errorf("table output missing column header %q", col)
		}
	}

	// Should list at least one known dependency
	if !strings.Contains(out, "packageurl-go") {
		t.Error("table output missing known dependency 'packageurl-go'")
	}

	// Should contain dependency tree summary
	if !strings.Contains(out, "Dependency Tree") || !strings.Contains(out, "Direct deps") {
		t.Error("table output missing Dependency Tree section")
	}
}

func TestE2E_DietJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	out := runDiet(t, "json")

	var result dietJSONOutput
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("JSON output is not valid JSON: %v\nOutput:\n%s", err, out[:min(len(out), 500)])
	}

	// Summary validation
	if result.Summary.TotalDirect == 0 {
		t.Error("JSON summary.total_direct should be > 0")
	}

	// Dependencies validation
	if len(result.Dependencies) == 0 {
		t.Fatal("JSON dependencies list is empty")
	}

	// Check ranks are sequential starting from 1
	for i, dep := range result.Dependencies {
		if dep.Rank != i+1 {
			t.Errorf("dependency[%d].rank = %d, want %d", i, dep.Rank, i+1)
		}
	}

	// All dependencies should have name and ecosystem
	for i, dep := range result.Dependencies {
		if dep.Name == "" {
			t.Errorf("dependency[%d].name is empty", i)
		}
		if dep.Ecosystem == "" {
			t.Errorf("dependency[%d].ecosystem is empty", i)
		}
		if dep.Difficulty == "" {
			t.Errorf("dependency[%d].difficulty is empty", i)
		}
	}

	// Some dependencies should have non-zero coupling (since source is the project itself)
	hasNonZeroCoupling := false
	for _, dep := range result.Dependencies {
		if dep.CallSiteCount > 0 || dep.ImportFileCount > 0 {
			hasNonZeroCoupling = true
			break
		}
	}
	if !hasNonZeroCoupling {
		t.Error("expected at least one dependency with non-zero coupling (call_site_count or import_file_count > 0)")
	}

	// Metadata fields
	if result.SBOMPath == "" {
		t.Error("JSON sbom_path is empty")
	}
	if result.SourceRoot == "" {
		t.Error("JSON source_root is empty")
	}
	if result.AnalyzedAt == "" {
		t.Error("JSON analyzed_at is empty")
	}
}

func TestE2E_DietDetailed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	out := runDiet(t, "detailed")

	// Detailed format should have per-dependency sections
	if !strings.Contains(out, "Graph Impact") {
		t.Error("detailed output missing 'Graph Impact' section")
	}
	if !strings.Contains(out, "Coupling") {
		t.Error("detailed output missing 'Coupling' section")
	}
	if !strings.Contains(out, "Health") {
		t.Error("detailed output missing 'Health' section")
	}

	// Should contain the summary section
	if !strings.Contains(out, "Summary") {
		t.Error("detailed output missing 'Summary' section")
	}

	// Should contain at least one dependency with actual import files listed
	if !strings.Contains(out, "Call sites:") {
		t.Error("detailed output missing 'Call sites:' label")
	}

	// Should show PURL for each dependency
	if !strings.Contains(out, "PURL:") {
		t.Error("detailed output missing 'PURL:' label")
	}
}

func TestE2E_DietCLIFlags(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	// Test that the CLI app rejects missing --sbom flag.
	configService := config.NewConfigService()
	cfg, err := configService.Load(context.Background())
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	graphAnalyzer := depgraph.NewAnalyzer()
	sourceAnalyzer := treesitter.NewAnalyzer()

	app := &urfcli.Command{
		Name: "uzomuzo-diet",
		Flags: []urfcli.Flag{
			&urfcli.StringFlag{
				Name:     "sbom",
				Required: true,
			},
			&urfcli.StringFlag{
				Name:  "source",
				Value: ".",
			},
			&urfcli.StringFlag{
				Name:    "format",
				Aliases: []string{"f"},
			},
		},
		Action: func(ctx context.Context, cmd *urfcli.Command) error {
			opts := cli.DietOptions{
				SBOMPath:   cmd.String("sbom"),
				SourceRoot: cmd.String("source"),
				Format:     cmd.String("format"),
			}
			return cli.RunDiet(ctx, cfg, opts, graphAnalyzer, sourceAnalyzer)
		},
	}

	// Missing --sbom should fail
	err = app.Run(context.Background(), []string{"uzomuzo-diet"})
	if err == nil {
		t.Error("expected error when --sbom is missing, got nil")
	}
}

func TestE2E_DietSourceValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	configService := config.NewConfigService()
	cfg, err := configService.Load(context.Background())
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	graphAnalyzer := depgraph.NewAnalyzer()
	sourceAnalyzer := treesitter.NewAnalyzer()

	// --source pointing to a file should fail
	opts := cli.DietOptions{
		SBOMPath:   testSBOMPath,
		SourceRoot: testSBOMPath, // a file, not a directory
		Format:     "json",
	}
	err = cli.RunDiet(context.Background(), cfg, opts, graphAnalyzer, sourceAnalyzer)
	if err == nil {
		t.Fatal("expected error when --source is a file, got nil")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("error should mention 'not a directory', got: %v", err)
	}

	// --source pointing to nonexistent path should fail
	opts.SourceRoot = "/nonexistent/path/that/does/not/exist"
	err = cli.RunDiet(context.Background(), cfg, opts, graphAnalyzer, sourceAnalyzer)
	if err == nil {
		t.Fatal("expected error when --source does not exist, got nil")
	}
}

func TestE2E_DietStdinSBOM(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	// Read the test SBOM into memory, then feed it via stdin
	sbomData, err := os.ReadFile(testSBOMPath)
	if err != nil {
		t.Fatalf("failed to read test SBOM: %v", err)
	}

	configService := config.NewConfigService()
	cfg, err := configService.Load(context.Background())
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	logging.Initialize(cfg.App.LogLevel)

	// Replace os.Stdin with a pipe containing the SBOM data
	oldStdin := os.Stdin
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe (stdin) failed: %v", err)
	}
	os.Stdin = stdinR
	t.Cleanup(func() {
		os.Stdin = oldStdin
		_ = stdinR.Close() // best-effort cleanup
	})

	go func() {
		_, _ = stdinW.Write(sbomData)
		_ = stdinW.Close()
	}()

	// Capture stdout
	var buf bytes.Buffer
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe (stdout) failed: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = oldStdout
		_ = w.Close() // best-effort cleanup
		_ = r.Close() // best-effort cleanup
	})

	done := make(chan struct{})
	go func() {
		_, _ = buf.ReadFrom(r)
		close(done)
	}()

	opts := cli.DietOptions{
		SBOMPath:   "-",
		SourceRoot: sourceRoot,
		Format:     "json",
	}

	graphAnalyzer := depgraph.NewAnalyzer()
	sourceAnalyzer := treesitter.NewAnalyzer()
	runErr := cli.RunDiet(context.Background(), cfg, opts, graphAnalyzer, sourceAnalyzer)

	if err := w.Close(); err != nil {
		t.Errorf("stdout write-end close: %v", err)
	}
	<-done
	if err := r.Close(); err != nil {
		t.Errorf("stdout read-end close: %v", err)
	}

	if runErr != nil {
		t.Fatalf("RunDiet with stdin failed: %v", runErr)
	}

	// Verify JSON output is valid and has data
	var result dietJSONOutput
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("stdin JSON output is not valid: %v", err)
	}
	if result.Summary.TotalDirect == 0 {
		t.Error("stdin: summary.total_direct should be > 0")
	}
	if result.SBOMPath != "-" {
		t.Errorf("stdin: sbom_path = %q, want %q", result.SBOMPath, "-")
	}
}

func TestE2E_DietDefaultFormat(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E test in short mode")
	}

	// When format is empty, RunDiet should default to table
	out := runDiet(t, "")

	if !strings.Contains(out, "RANK") {
		t.Error("default format should be table (expected RANK column header)")
	}
}
