package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	domaincfg "github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/spdx"
	"github.com/future-architect/uzomuzo-oss/internal/interfaces/cli"
)

// runUpdateSPDX downloads latest SPDX licenses.json, writes it, and regenerates tables.
func runUpdateSPDX(ctx context.Context) error {
	path := "third_party/spdx/licenses.json"
	slog.Info("fetching SPDX", "url", spdx.UpstreamURL)
	data, err := spdx.FetchLatest(ctx, nil)
	if err != nil {
		return fmt.Errorf("fetching SPDX licenses: %w", err)
	}
	ver, err := spdx.ValidatePayload(data)
	if err != nil {
		return fmt.Errorf("validating SPDX payload: %w", err)
	}
	if err := spdx.WriteAtomic(path, data); err != nil {
		return fmt.Errorf("writing SPDX json to %s: %w", path, err)
	}
	slog.Info("wrote SPDX json", "path", path, "version", ver, "bytes", len(data))
	cmd := exec.CommandContext(ctx, "go", "generate", "./internal/domain/licenses")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	slog.Info("running go generate for licenses")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("running go generate for licenses: %w", err)
	}
	slog.Info("SPDX update complete")
	return nil
}

// isTerminal reports whether f is connected to a terminal (not a pipe).
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return true // conservative: assume terminal on error
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// processStdin reads PURLs/GitHub URLs from stdin (one per line) and delegates to direct mode.
func processStdin(ctx context.Context, cfg *domaincfg.Config, opts cli.ProcessingOptions) error {
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
	return cli.ProcessDirectMode(ctx, cfg, lines, opts)
}

// isFilePath determines if the input is a file path or a direct PURL/GitHub URL.
//
// DEPRECATED: Used only by the legacy root action shim for backward compatibility.
// The "analyze" subcommand uses explicit --file flags instead.
// Do not reuse this function in new code. Remove after deprecation cycle.
func isFilePath(input string) bool {
	// Check if it's a PURL
	if strings.HasPrefix(input, "pkg:") {
		return false
	}

	// Check if it's a GitHub URL
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		return false
	}

	// Check if it's a GitHub shorthand (github.com/owner/repo)
	if strings.HasPrefix(input, "github.com/") {
		return false
	}

	// Check if file exists
	if _, err := os.Stat(input); err == nil {
		return true
	}

	// If it doesn't exist as a file but looks like a path, treat as file
	return strings.Contains(input, "/") || strings.Contains(input, "\\") || strings.Contains(input, ".")
}
