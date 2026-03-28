package main

import (
	"context"
	"testing"

	urfcli "github.com/urfave/cli/v3"

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
