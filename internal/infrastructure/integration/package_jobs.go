package integration

import (
	"context"
	"log/slog"
	"sync"

	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// maxPackageFactWorkers bounds the concurrent package-level lookups started by
// each enrichment. A 30k-PURL batch would otherwise spawn one goroutine and one
// in-flight request per unique package name.
const maxPackageFactWorkers = 16

// packageJobKey identifies one package-level lookup.
//
// The name is the one sent to the source. Each enrichment applies its own
// normalization before building this key. OSV matches crates.io names
// case-sensitively, so cargo names keep their original spelling.
type packageJobKey struct {
	ecosystem string
	name      string
}

// packageFetch reports one package-level fact. found is false when the source
// has no such package or the lookup failed; err is non-nil only for a failed
// lookup, and callers must read an error as "unknown", never as a negative.
type packageFetch[T any] func(ctx context.Context, name string) (value T, found bool, err error)

// packageJob is one lookup and every analysis waiting on its result.
type packageJob[T any] struct {
	fetch   packageFetch[T]
	targets []*domain.Analysis
}

// collectPackageJobs groups analyses into one lookup per distinct package.
//
// pick chooses the lookup key and fetch for a parsed PURL, returning nil to
// skip it. Each enrichment decides how to handle namespaces and unsupported
// ecosystems. Analyses with unparseable PURLs are skipped.
func collectPackageJobs[T any](
	analyses map[string]*domain.Analysis,
	pick func(parsed *purl.ParsedPURL) (packageJobKey, packageFetch[T]),
) map[packageJobKey]*packageJob[T] {
	jobs := map[packageJobKey]*packageJob[T]{}
	parser := purl.NewParser()
	for _, a := range analyses {
		if a == nil || a.Package == nil {
			continue
		}
		parsed, err := parser.Parse(a.Package.PURL)
		if err != nil {
			continue
		}
		key, fetch := pick(parsed)
		if fetch == nil || key.ecosystem == "" || key.name == "" {
			continue
		}
		job, seen := jobs[key]
		if !seen {
			job = &packageJob[T]{fetch: fetch}
			jobs[key] = job
		}
		job.targets = append(job.targets, a)
	}
	return jobs
}

// runPackageJobs runs each lookup under a bounded worker pool and hands the
// result to apply for every analysis waiting on it.
//
// A failed lookup invokes onFailure when provided; existing callers leave their
// target field untouched. The what argument names the enrichment in debug logs.
//
// DDD Layer: Infrastructure (parallel enrichment).
func runPackageJobs[T any](
	ctx context.Context,
	what string,
	jobs map[packageJobKey]*packageJob[T],
	apply func(a *domain.Analysis, value T),
	onFailure ...func(key packageJobKey, targets []*domain.Analysis),
) {
	if len(jobs) == 0 {
		return
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxPackageFactWorkers)
	for key, job := range jobs {
		// Acquire before launching so a cancelled context stops dispatch instead
		// of parking a goroutine per remaining package.
		// A closed ctx.Done() and a free slot are both ready cases, and select
		// picks at random, so cancellation is checked explicitly rather than
		// relied on to win the race.
		if ctx.Err() != nil {
			wg.Wait()
			return
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return
		}
		if ctx.Err() != nil {
			<-sem
			wg.Wait()
			return
		}
		wg.Add(1)
		go func(key packageJobKey, fetch packageFetch[T], targets []*domain.Analysis) {
			defer wg.Done()
			defer func() { <-sem }()
			value, found, err := fetch(ctx, key.name)
			if err != nil {
				slog.Debug("package_fact_fetch_failed", "what", what, "name", key.name, "error", err)
				if len(onFailure) != 0 && onFailure[0] != nil {
					onFailure[0](key, targets)
				}
				return
			}
			if !found {
				return
			}
			for _, a := range targets {
				apply(a, value)
			}
		}(key, job.fetch, job.targets)
	}
	wg.Wait()
}
