package integration

import (
	"context"
	"log/slog"
	"sync"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// licenseCoord is the deduplicated coordinate key used by the license-enricher
// fan-out pipeline. Maven populates ecosystem="maven", namespace=groupId,
// name=artifactId, version=version. ClearlyDefined populates all four fields
// from the analysis PURL.
type licenseCoord struct {
	ecosystem string
	namespace string
	name      string
	version   string
}

// licenseLogEvents carries the slog event names emitted by dispatchLicenseJobs.
// An empty string means "skip that log", which preserves intentional asymmetry:
// the Maven manifest tier has no hit/miss telemetry, while the ClearlyDefined
// tier does.
type licenseLogEvents struct {
	fetchFailed string // logged at WARN on any non-rate-limit fetch error
	rateLimited string // logged at WARN when common.IsRateLimitError is true
	miss        string // logged at DEBUG when fetch returns found=false
	hit         string // logged at DEBUG when applyManifestLicenses writes at least one field
	noChange    string // logged at DEBUG when fetch returned data but nothing was written
}

const maxLicenseFetchConcurrency = 10

// dispatchLicenseJobs is the shared fan-out pipeline for license-enricher
// tiers. It runs up to maxLicenseFetchConcurrency concurrent fetch calls,
// respects context cancellation, and applies applyManifestLicenses to all
// analyses sharing a coordinate.
//
// jobs maps each deduplicated coordinate to the slice of analyses that share
// it. fetch is a closure that wraps the tier-specific client call (Maven POM
// or ClearlyDefined.io). events holds the slog event names; empty string
// fields are silently skipped.
func (s *IntegrationService) dispatchLicenseJobs(
	ctx context.Context,
	jobs map[licenseCoord][]*domain.Analysis,
	fetch func(ctx context.Context, k licenseCoord) ([]domain.ResolvedLicense, bool, error),
	events licenseLogEvents,
) {
	if len(jobs) == 0 {
		return
	}

	sem := make(chan struct{}, maxLicenseFetchConcurrency)
	var wg sync.WaitGroup

dispatchLoop:
	for k, targets := range jobs {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break dispatchLoop
		}

		wg.Add(1)
		go func(k licenseCoord, targets []*domain.Analysis) {
			defer wg.Done()
			defer func() { <-sem }()

			lics, found, err := fetch(ctx, k)
			if err != nil {
				event := events.fetchFailed
				if common.IsRateLimitError(err) {
					event = events.rateLimited
				}
				if event != "" {
					slog.Warn(event,
						"ecosystem", k.ecosystem,
						"namespace", k.namespace,
						"name", k.name,
						"version", k.version,
						"error", err)
				}
				return
			}
			if !found || len(lics) == 0 {
				if events.miss != "" {
					slog.Debug(events.miss,
						"ecosystem", k.ecosystem,
						"namespace", k.namespace,
						"name", k.name,
						"version", k.version)
				}
				return
			}

			var wrote bool
			for _, a := range targets {
				if applyManifestLicenses(a, lics) {
					wrote = true
				}
			}

			if wrote && events.hit != "" {
				slog.Debug(events.hit,
					"ecosystem", k.ecosystem,
					"namespace", k.namespace,
					"name", k.name,
					"version", k.version,
					"licenses_count", len(lics))
			} else if !wrote && events.noChange != "" {
				slog.Debug(events.noChange,
					"ecosystem", k.ecosystem,
					"namespace", k.namespace,
					"name", k.name,
					"version", k.version,
					"licenses_count", len(lics))
			}
		}(k, targets)
	}
	wg.Wait()
}
