package main

import (
	"bytes"
	"context"
	"os"
	"testing"

	urfcli "github.com/urfave/cli/v3"

	domaincfg "github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/interfaces/cli"
)

// runWithFlags creates a minimal urfave/cli app with the root flags, parses
// the given CLI args, and calls buildProcessingOptions inside the Action.
// This avoids reaching into urfave internals and exercises the real flag
// parsing path.
func runWithFlags(t *testing.T, args []string) (cli.ProcessingOptions, error) {
	t.Helper()

	var opts cli.ProcessingOptions
	var optsErr error

	app := &urfcli.Command{
		Name: "test",
		Flags: []urfcli.Flag{
			&urfcli.BoolFlag{Name: "only-review-needed"},
			&urfcli.BoolFlag{Name: "only-eol"},
			&urfcli.StringFlag{Name: "ecosystem"},
			&urfcli.IntFlag{Name: "sample"},
			&urfcli.StringFlag{Name: "export-license-csv"},
			&urfcli.StringFlag{Name: "line-range"},
		},
		Action: func(_ context.Context, cmd *urfcli.Command) error {
			opts, optsErr = buildProcessingOptions(cmd)
			return nil
		},
	}

	// Prepend program name as urfave/cli expects os.Args layout.
	fullArgs := append([]string{"test"}, args...)
	if err := app.Run(context.Background(), fullArgs); err != nil {
		t.Fatalf("urfave/cli Run failed: %v", err)
	}
	return opts, optsErr
}

func TestBuildProcessingOptions(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
		check   func(t *testing.T, opts cli.ProcessingOptions)
	}{
		{
			name: "zero/default flags",
			args: nil,
			check: func(t *testing.T, opts cli.ProcessingOptions) {
				if opts.OnlyReviewNeeded {
					t.Error("OnlyReviewNeeded should be false")
				}
				if opts.OnlyEOL {
					t.Error("OnlyEOL should be false")
				}
				if opts.Ecosystem != "" {
					t.Errorf("Ecosystem should be empty, got %q", opts.Ecosystem)
				}
				if opts.SampleSize != 0 {
					t.Errorf("SampleSize should be 0, got %d", opts.SampleSize)
				}
				if opts.LicenseCSVPath != "" {
					t.Errorf("LicenseCSVPath should be empty, got %q", opts.LicenseCSVPath)
				}
				if opts.LineStart != 0 || opts.LineEnd != 0 {
					t.Errorf("LineStart/LineEnd should be 0/0, got %d/%d", opts.LineStart, opts.LineEnd)
				}
			},
		},
		{
			name: "all flags set",
			args: []string{
				"--only-review-needed",
				"--only-eol",
				"--ecosystem", "npm",
				"--sample", "42",
				"--export-license-csv", "/tmp/lic.csv",
				"--line-range", "5:20",
			},
			check: func(t *testing.T, opts cli.ProcessingOptions) {
				if !opts.OnlyReviewNeeded {
					t.Error("OnlyReviewNeeded should be true")
				}
				if !opts.OnlyEOL {
					t.Error("OnlyEOL should be true")
				}
				if opts.Ecosystem != "npm" {
					t.Errorf("Ecosystem = %q, want %q", opts.Ecosystem, "npm")
				}
				if opts.SampleSize != 42 {
					t.Errorf("SampleSize = %d, want 42", opts.SampleSize)
				}
				if opts.LicenseCSVPath != "/tmp/lic.csv" {
					t.Errorf("LicenseCSVPath = %q, want %q", opts.LicenseCSVPath, "/tmp/lic.csv")
				}
				if opts.LineStart != 5 || opts.LineEnd != 20 {
					t.Errorf("LineStart/LineEnd = %d/%d, want 5/20", opts.LineStart, opts.LineEnd)
				}
			},
		},
		{
			name:    "invalid line-range missing colon",
			args:    []string{"--line-range", "10-20"},
			wantErr: true,
		},
		{
			name:    "invalid line-range end less than start",
			args:    []string{"--line-range", "20:5"},
			wantErr: true,
		},
		{
			name: "valid line-range open end",
			args: []string{"--line-range", "3:"},
			check: func(t *testing.T, opts cli.ProcessingOptions) {
				if opts.LineStart != 3 {
					t.Errorf("LineStart = %d, want 3", opts.LineStart)
				}
				if opts.LineEnd != 0 {
					t.Errorf("LineEnd = %d, want 0 (meaning EOF)", opts.LineEnd)
				}
			},
		},
		{
			name: "sample size stays zero when flag not given",
			args: []string{"--only-eol"},
			check: func(t *testing.T, opts cli.ProcessingOptions) {
				// buildProcessingOptions should NOT apply config SampleSize;
				// that is deferred to rootAction for file mode only.
				if opts.SampleSize != 0 {
					t.Errorf("SampleSize = %d, want 0 (config default deferred to file mode)", opts.SampleSize)
				}
			},
		},
		{
			name: "sample size from flag overrides zero",
			args: []string{"--sample", "5"},
			check: func(t *testing.T, opts cli.ProcessingOptions) {
				if opts.SampleSize != 5 {
					t.Errorf("SampleSize = %d, want 5", opts.SampleSize)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := runWithFlags(t, tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, opts)
			}
		})
	}
}

// runAnalyzeWithFlags creates a minimal urfave/cli app with the analyze subcommand,
// parses the given CLI args, and calls buildProcessingOptions inside the Action.
func runAnalyzeWithFlags(t *testing.T, args []string) (cli.ProcessingOptions, error) {
	t.Helper()

	var opts cli.ProcessingOptions
	var optsErr error

	app := &urfcli.Command{
		Name: "test",
		Commands: []*urfcli.Command{
			{
				Name:  "analyze",
				Flags: analyzeFlags(),
				Action: func(_ context.Context, cmd *urfcli.Command) error {
					opts, optsErr = buildProcessingOptions(cmd)
					return nil
				},
			},
		},
	}

	fullArgs := append([]string{"test", "analyze"}, args...)
	if err := app.Run(context.Background(), fullArgs); err != nil {
		t.Fatalf("urfave/cli Run failed: %v", err)
	}
	return opts, optsErr
}

func TestBuildAnalyzeProcessingOptions(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
		check   func(t *testing.T, opts cli.ProcessingOptions)
	}{
		{
			name: "zero/default flags",
			args: nil,
			check: func(t *testing.T, opts cli.ProcessingOptions) {
				if opts.OnlyReviewNeeded {
					t.Error("OnlyReviewNeeded should be false")
				}
				if opts.SampleSize != 0 {
					t.Errorf("SampleSize should be 0, got %d", opts.SampleSize)
				}
			},
		},
		{
			name: "all shared flags set",
			args: []string{
				"--only-review-needed",
				"--only-eol",
				"--ecosystem", "npm",
				"--export-license-csv", "/tmp/lic.csv",
			},
			check: func(t *testing.T, opts cli.ProcessingOptions) {
				if !opts.OnlyReviewNeeded {
					t.Error("OnlyReviewNeeded should be true")
				}
				if !opts.OnlyEOL {
					t.Error("OnlyEOL should be true")
				}
				if opts.Ecosystem != "npm" {
					t.Errorf("Ecosystem = %q, want %q", opts.Ecosystem, "npm")
				}
				if opts.LicenseCSVPath != "/tmp/lic.csv" {
					t.Errorf("LicenseCSVPath = %q, want %q", opts.LicenseCSVPath, "/tmp/lic.csv")
				}
			},
		},
		{
			name: "file mode flags",
			args: []string{
				"--file", "input.txt",
				"--sample", "10",
				"--line-range", "5:20",
			},
			check: func(t *testing.T, opts cli.ProcessingOptions) {
				if opts.SampleSize != 10 {
					t.Errorf("SampleSize = %d, want 10", opts.SampleSize)
				}
				if opts.LineStart != 5 || opts.LineEnd != 20 {
					t.Errorf("LineStart/LineEnd = %d/%d, want 5/20", opts.LineStart, opts.LineEnd)
				}
			},
		},
		{
			name:    "invalid line-range format",
			args:    []string{"--file", "input.txt", "--line-range", "10-20"},
			wantErr: true,
		},
		{
			name: "sample zero accepted as process all",
			args: []string{"--file", "input.txt", "--sample", "0"},
			check: func(t *testing.T, opts cli.ProcessingOptions) {
				if opts.SampleSize != 0 {
					t.Errorf("SampleSize = %d, want 0 (process all)", opts.SampleSize)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := runAnalyzeWithFlags(t, tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, opts)
			}
		})
	}
}

func TestAnalyzeAction_FlagValidation(t *testing.T) {
	cfg := &domaincfg.Config{}

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "sample without file rejects",
			args:    []string{"analyze", "--sample", "10", "pkg:npm/express"},
			wantErr: "--sample requires --file",
		},
		{
			name:    "line-range without file rejects",
			args:    []string{"analyze", "--line-range", "1:10", "pkg:npm/express"},
			wantErr: "--line-range requires --file",
		},
		{
			name:    "positional args with file rejects",
			args:    []string{"analyze", "--file", "input.txt", "pkg:npm/express"},
			wantErr: "positional arguments are not allowed with --file",
		},
		{
			name:    "negative sample rejects",
			args:    []string{"analyze", "--file", "input.txt", "--sample", "-1"},
			wantErr: "--sample must be zero (process all) or a positive integer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := buildApp(cfg)
			fullArgs := append([]string{"uzomuzo"}, tt.args...)
			err := app.Run(context.Background(), fullArgs)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if got := err.Error(); got != tt.wantErr {
				// The error may be wrapped; check it contains the expected message.
				if !bytes.Contains([]byte(got), []byte(tt.wantErr)) {
					t.Errorf("error = %q, want containing %q", got, tt.wantErr)
				}
			}
		})
	}
}

// TestRootAction_DeprecationWarning verifies stderr deprecation output.
// Not safe for t.Parallel() — mutates os.Stderr.
func TestRootAction_DeprecationWarning(t *testing.T) {
	// Capture stderr to verify deprecation warning.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() failed: %v", err)
	}

	oldStderr := os.Stderr
	os.Stderr = w
	t.Cleanup(func() {
		os.Stderr = oldStderr
		_ = r.Close() // best-effort cleanup
	})

	cfg := &domaincfg.Config{}
	app := buildApp(cfg)
	// Use "not-a-purl" — fails categorization early without network access,
	// but still exercises the rootAction deprecation warning path.
	_ = app.Run(context.Background(), []string{"uzomuzo", "not-a-purl"})

	_ = w.Close() // close write end so ReadFrom sees EOF

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("failed to read captured stderr: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("deprecated")) {
		t.Errorf("expected deprecation warning in stderr, got: %s", buf.String())
	}
}

// TestRootAction_SampleWithoutFileRejects verifies that --sample in direct mode
// is rejected by the deprecated root action, matching analyzeAction behavior.
func TestRootAction_SampleWithoutFileRejects(t *testing.T) {
	cfg := &domaincfg.Config{}
	app := buildApp(cfg)
	err := app.Run(context.Background(), []string{"uzomuzo", "--sample", "10", "pkg:npm/express"})
	if err == nil {
		t.Fatal("expected error for --sample without file in root action, got nil")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("--sample requires file input")) {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRootAction_NegativeSampleRejects verifies that rootAction rejects negative
// --sample values, matching analyzeAction behavior.
func TestRootAction_NegativeSampleRejects(t *testing.T) {
	cfg := &domaincfg.Config{}
	app := buildApp(cfg)
	err := app.Run(context.Background(), []string{"uzomuzo", "--sample", "-1", "input.txt"})
	if err == nil {
		t.Fatal("expected error for negative --sample in root action, got nil")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("--sample must be non-negative")) {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestRootAction_SampleWithGitHubURLRejects verifies that --sample is rejected
// when used with a GitHub URL in the deprecated root action.
func TestRootAction_SampleWithGitHubURLRejects(t *testing.T) {
	cfg := &domaincfg.Config{}
	app := buildApp(cfg)
	err := app.Run(context.Background(), []string{"uzomuzo", "--sample", "10", "https://github.com/expressjs/express"})
	if err == nil {
		t.Fatal("expected error for --sample with GitHub URL in root action, got nil")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("--sample requires file input")) {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestAnalyzeAction_NoInputReturnsNil verifies that "uzomuzo analyze" with no args
// returns nil (no error). It prints a guidance message but does not show full help.
func TestAnalyzeAction_NoInputReturnsNil(t *testing.T) {
	cfg := &domaincfg.Config{}
	app := buildApp(cfg)
	err := app.Run(context.Background(), []string{"uzomuzo", "analyze"})
	if err != nil {
		t.Errorf("expected nil error for analyze with no input, got: %v", err)
	}
}

// TestRootAction_NoInputReturnsNil verifies that bare "uzomuzo" with no args
// returns nil (no error), matching the no-input behavior of analyzeAction.
func TestRootAction_NoInputReturnsNil(t *testing.T) {
	cfg := &domaincfg.Config{}
	app := buildApp(cfg)
	err := app.Run(context.Background(), []string{"uzomuzo"})
	if err != nil {
		t.Errorf("expected nil error for root with no input, got: %v", err)
	}
}

func TestAnalyzeAction_FileNotFoundReturnsError(t *testing.T) {
	cfg := &domaincfg.Config{}
	app := buildApp(cfg)
	err := app.Run(context.Background(), []string{"uzomuzo", "analyze", "--file", "nonexistent.txt"})
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestAnalyzeCommand_Registered(t *testing.T) {
	cfg := &domaincfg.Config{}
	app := buildApp(cfg)

	found := false
	for _, cmd := range app.Commands {
		if cmd.Name == "analyze" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'analyze' subcommand to be registered in buildApp()")
	}
}
