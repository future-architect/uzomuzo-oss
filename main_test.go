package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
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

// runScanWithFlags creates a minimal urfave/cli app with the scan subcommand,
// parses the given CLI args, and calls buildProcessingOptions inside the Action.
func runScanWithFlags(t *testing.T, args []string) (cli.ProcessingOptions, error) {
	t.Helper()

	var opts cli.ProcessingOptions
	var optsErr error

	app := &urfcli.Command{
		Name: "test",
		Commands: []*urfcli.Command{
			{
				Name:  "scan",
				Flags: scanFlags(),
				Action: func(_ context.Context, cmd *urfcli.Command) error {
					opts, optsErr = buildProcessingOptions(cmd)
					return nil
				},
			},
		},
	}

	fullArgs := append([]string{"test", "scan"}, args...)
	if err := app.Run(context.Background(), fullArgs); err != nil {
		t.Fatalf("urfave/cli Run failed: %v", err)
	}
	return opts, optsErr
}

func TestBuildScanProcessingOptions(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := runScanWithFlags(t, tt.args)
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

func TestScanAction_FlagValidation(t *testing.T) {
	cfg := &domaincfg.Config{}

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "sample without file rejects",
			args:    []string{"scan", "--sample", "10", "pkg:npm/express"},
			wantErr: "--sample requires --file",
		},
		{
			name:    "line-range without file rejects",
			args:    []string{"scan", "--line-range", "1:10", "pkg:npm/express"},
			wantErr: "--line-range requires --file",
		},
		{
			name:    "positional args with file rejects",
			args:    []string{"scan", "--file", "input.txt", "pkg:npm/express"},
			wantErr: "positional arguments are not allowed with --file",
		},
		{
			name:    "negative sample rejects",
			args:    []string{"scan", "--file", "input.txt", "--sample", "-1"},
			wantErr: "--sample must be a positive integer",
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
	// Create a temp file to use as a PURL input file so rootAction's isFilePath returns true.
	dir := t.TempDir()
	f := filepath.Join(dir, "purls.txt")
	if err := os.WriteFile(f, []byte("pkg:npm/express@4.18.2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Capture stderr to verify deprecation warning.
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	cfg := &domaincfg.Config{}
	app := buildApp(cfg)
	// Run with a PURL arg (will fail at network level, but we only care about the warning).
	_ = app.Run(context.Background(), []string{"uzomuzo", "pkg:npm/express@4.18.2"})

	w.Close()
	os.Stderr = oldStderr

	var buf bytes.Buffer
	buf.ReadFrom(r)
	if !bytes.Contains(buf.Bytes(), []byte("deprecated")) {
		t.Errorf("expected deprecation warning in stderr, got: %s", buf.String())
	}
}

func TestScanCommand_Registered(t *testing.T) {
	cfg := &domaincfg.Config{}
	app := buildApp(cfg)

	found := false
	for _, cmd := range app.Commands {
		if cmd.Name == "scan" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'scan' subcommand to be registered in buildApp()")
	}
}
