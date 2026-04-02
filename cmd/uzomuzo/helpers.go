package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"

	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/spdx"
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
