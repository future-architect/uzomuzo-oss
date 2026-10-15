package cli

import (
	"context"
	"strings"
	"testing"

	domaincfg "github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

// TestRunScanRejectsRepositoryFactsWithoutToken pins that a tokenless scan
// refuses --fail-on archived / disabled instead of passing entries whose
// repository state was never read. See ADR-0029.
func TestRunScanRejectsRepositoryFactsWithoutToken(t *testing.T) {
	for _, raw := range []string{"archived", "disabled", "stalled,disabled"} {
		t.Run(raw, func(t *testing.T) {
			cfg := &domaincfg.Config{}
			err := RunScan(context.Background(), cfg, []string{"pkg:npm/through@2.3.8"}, ScanOptions{FailOnRaw: raw}, nil, nil, nil, nil, nil)
			if err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
				t.Fatalf("RunScan --fail-on %s without a token: err = %v, want a GITHUB_TOKEN error", raw, err)
			}
		})
	}
}
