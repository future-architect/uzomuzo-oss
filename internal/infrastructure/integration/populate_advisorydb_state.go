package integration

import (
	"context"
	"slices"
	"time"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// osvCratesEcosystem is OSV.dev's identifier for the Rust package registry.
// It is not the PURL type ("cargo") and OSV matches it case-sensitively.
const osvCratesEcosystem = "crates.io"

// enrichAdvisoryDBState populates Analysis.AdvisoryDBState for cargo analyses
// with the advisory database's package-level maintenance fact. No-op when the
// OSV client is unwired.
//
// Cargo only: crates.io publishes no deprecation signal of its own, so RustSec
// is the Rust ecosystem's only machine-readable "this package is unmaintained"
// statement. Other ecosystems already have a registry-native signal with a rule
// in the EOL evaluator. See ADR-0025.
//
// Best-effort: a fetch failure leaves AdvisoryDBState nil, which the lifecycle
// assessor reads as "not asked" rather than "not flagged". A successful lookup
// always writes it, including the Unmaintained=false case.
//
// DDD Layer: Infrastructure (best-effort parallel enrichment). The admission
// rules live in analysis.ClassifyUnmaintained, not here.
func (s *IntegrationService) enrichAdvisoryDBState(ctx context.Context, analyses map[string]*domain.Analysis) {
	if len(analyses) == 0 || s.osvClient == nil {
		return
	}
	// One evaluation time for the whole batch, so two versions of the same crate
	// cannot straddle the cooldown boundary within a single run.
	now := time.Now()
	jobs := collectPackageJobs(analyses, func(ecosystem string) packageFetch[domain.AdvisoryDBState] {
		if ecosystem != "cargo" {
			return nil
		}
		return func(ctx context.Context, name string) (domain.AdvisoryDBState, bool, error) {
			recs, err := s.osvClient.QueryPackage(ctx, osvCratesEcosystem, name)
			if err != nil {
				return domain.AdvisoryDBState{}, false, err
			}
			return domain.ClassifyUnmaintained(recs, osvCratesEcosystem, name, now), true, nil
		}
	})
	runPackageJobs(ctx, "advisory_db_state", jobs, func(a *domain.Analysis, state domain.AdvisoryDBState) {
		cp := state
		cp.MarkerIDs = slices.Clone(state.MarkerIDs)
		a.AdvisoryDBState = &cp
	})
}
