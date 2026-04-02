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

// runScanWithFlags creates a minimal urfave/cli app with the scan subcommand flags,
// parses the given CLI args, and calls buildScanOptions inside the Action.
func runScanWithFlags(t *testing.T, args []string) (cli.ScanOptions, error) {
	t.Helper()

	var opts cli.ScanOptions
	var optsErr error

	app := &urfcli.Command{
		Name: "test",
		Commands: []*urfcli.Command{
			{
				Name:  "scan",
				Flags: scanFlags(),
				Action: func(_ context.Context, cmd *urfcli.Command) error {
					opts, optsErr = buildScanOptions(cmd)
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

func TestBuildScanOptions(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
		check   func(t *testing.T, opts cli.ScanOptions)
	}{
		{
			name: "zero/default flags",
			args: nil,
			check: func(t *testing.T, opts cli.ScanOptions) {
				if opts.OnlyReviewNeeded {
					t.Error("OnlyReviewNeeded should be false")
				}
				if opts.OnlyEOL {
					t.Error("OnlyEOL should be false")
				}
				if opts.Format != "" {
					t.Errorf("Format should be empty, got %q", opts.Format)
				}
				if opts.FailOnRaw != "" {
					t.Errorf("FailOnRaw should be empty, got %q", opts.FailOnRaw)
				}
				if opts.SBOMPath != "" {
					t.Errorf("SBOMPath should be empty, got %q", opts.SBOMPath)
				}
			},
		},
		{
			name: "all flags set",
			args: []string{
				"--sample", "42",
				"--line-range", "5:20",
				"--format", "json",
				"--fail-on", "eol-confirmed,stalled",
				"--sbom", "bom.json",
			},
			check: func(t *testing.T, opts cli.ScanOptions) {
				if opts.SampleSize != 42 {
					t.Errorf("SampleSize = %d, want 42", opts.SampleSize)
				}
				if opts.LineStart != 5 || opts.LineEnd != 20 {
					t.Errorf("LineStart/LineEnd = %d/%d, want 5/20", opts.LineStart, opts.LineEnd)
				}
				if opts.Format != "json" {
					t.Errorf("Format = %q, want %q", opts.Format, "json")
				}
				if opts.FailOnRaw != "eol-confirmed,stalled" {
					t.Errorf("FailOnRaw = %q, want %q", opts.FailOnRaw, "eol-confirmed,stalled")
				}
				if opts.SBOMPath != "bom.json" {
					t.Errorf("SBOMPath = %q, want %q", opts.SBOMPath, "bom.json")
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
			check: func(t *testing.T, opts cli.ScanOptions) {
				if opts.LineStart != 3 {
					t.Errorf("LineStart = %d, want 3", opts.LineStart)
				}
				if opts.LineEnd != 0 {
					t.Errorf("LineEnd = %d, want 0 (meaning EOF)", opts.LineEnd)
				}
			},
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
			wantErr: "positional arguments are not allowed with --file or --sbom",
		},
		{
			name:    "positional args with sbom rejects",
			args:    []string{"scan", "--sbom", "bom.json", "pkg:npm/express"},
			wantErr: "positional arguments are not allowed with --file or --sbom",
		},
		{
			name:    "negative sample rejects",
			args:    []string{"scan", "--file", "input.txt", "--sample", "-1"},
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
			if got := err.Error(); !bytes.Contains([]byte(got), []byte(tt.wantErr)) {
				t.Errorf("error = %q, want containing %q", got, tt.wantErr)
			}
		})
	}
}

// TestRootAction_NoInputReturnsNil verifies that bare "uzomuzo" with no args
// returns nil (no error).
func TestRootAction_NoInputReturnsNil(t *testing.T) {
	// Capture stderr to suppress output
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	t.Cleanup(func() {
		os.Stderr = oldStderr
		_ = r.Close()
	})

	cfg := &domaincfg.Config{}
	app := buildApp(cfg)
	err := app.Run(context.Background(), []string{"uzomuzo"})
	_ = w.Close()
	if err != nil {
		t.Errorf("expected nil error for root with no input, got: %v", err)
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

func TestScanAction_FileNotFoundReturnsError(t *testing.T) {
	cfg := &domaincfg.Config{}
	app := buildApp(cfg)
	err := app.Run(context.Background(), []string{"uzomuzo", "scan", "--file", "nonexistent.txt"})
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}
