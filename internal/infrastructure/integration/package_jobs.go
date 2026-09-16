package integration

import (
	"context"
	"log/slog"
	"strings"
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
// The name is the one written in the PURL, not a lowercased form: it is sent
// verbatim to the source, and api.osv.dev matches crates.io names
// case-sensitively (a query for "Atty" returns nothing where "atty" returns
// three advisories). Folding case here would turn that into a silent empty
// answer, so two casings of one name cost one extra lookup instead.
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
// pick chooses the fetch for an ecosystem and returns nil to skip it, which is
// how each enrichment states both the ecosystems it covers and the clients it
// needs wired. Analyses are also skipped when the PURL does not parse, when it
// carries a namespace (the ecosystems asked here have none, so a namespaced
// PURL would query a different package), or when the name is empty.
func collectPackageJobs[T any](
	analyses map[string]*domain.Analysis,
	pick func(ecosystem string) packageFetch[T],
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
		if strings.TrimSpace(parsed.Namespace()) != "" {
			continue
		}
		name := strings.TrimSpace(parsed.PackageName())
		if name == "" {
			continue
		}
		fetch := pick(parsed.Ecosystem())
		if fetch == nil {
			continue
		}
		key := packageJobKey{ecosystem: parsed.Ecosystem(), name: name}
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
// Best-effort: a failed or empty lookup applies nothing, leaving the target
// field at its zero value, which the lifecycle assessor reads as "not asked"
// rather than as a negative answer. The what argument names the enrichment in
// debug logs.
//
// DDD Layer: Infrastructure (parallel enrichment).
func runPackageJobs[T any](
	ctx context.Context,
	what string,
	jobs map[packageJobKey]*packageJob[T],
	apply func(a *domain.Analysis, value T),
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
		go func(name string, fetch packageFetch[T], targets []*domain.Analysis) {
			defer wg.Done()
			defer func() { <-sem }()
			value, found, err := fetch(ctx, name)
			if err != nil {
				slog.Debug("package_fact_fetch_failed", "what", what, "name", name, "error", err)
				return
			}
			if !found {
				return
			}
			for _, a := range targets {
				apply(a, value)
			}
		}(key.name, job.fetch, job.targets)
	}
	wg.Wait()
}
