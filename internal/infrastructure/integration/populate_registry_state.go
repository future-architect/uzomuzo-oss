package integration

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"unicode"

	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// maxRegistryStateWorkers bounds the concurrent registry lookups started by
// enrichRegistryState. A 30k-PURL batch would otherwise spawn one goroutine and
// one in-flight request per unique package name.
const maxRegistryStateWorkers = 16

// registryFetch reports the registry's package-level withdrawal fact for one
// package. found is false when the registry has no such package or the lookup
// failed; err is non-nil only for a failed lookup.
type registryFetch func(ctx context.Context, name string) (state *domain.RegistryState, found bool, err error)

// registryJobKey identifies one registry lookup. The name is lowercased so
// case-variant PURLs for the same package share a single lookup.
type registryJobKey struct {
	ecosystem string
	name      string
}

// registryJob is one lookup and every analysis waiting on its result.
type registryJob struct {
	fetch   registryFetch
	targets []*domain.Analysis
}

// enrichRegistryState populates Analysis.RegistryState for pypi and cargo
// analyses with the registry's package-level withdrawal fact ("every published
// release is yanked"). No-op when the corresponding client is unwired.
//
// Best-effort: a fetch failure leaves RegistryState nil, which the lifecycle
// assessor reads as "not asked" rather than "nothing yanked". A successful fetch
// always writes RegistryState, including the AllReleasesYanked=false case.
//
// Analyses are skipped — leaving RegistryState nil — when the PURL does not
// parse, when it carries a namespace (pypi and cargo PURLs have none, so a
// namespaced one would query a different package), or when the ecosystem is
// neither pypi nor cargo.
//
// Unlike enrichPyPISummary this does not require a populated Repository: the
// fact is asserted by the registry and is independent of the source repository.
//
// DDD Layer: Infrastructure (best-effort parallel enrichment).
func (s *IntegrationService) enrichRegistryState(ctx context.Context, analyses map[string]*domain.Analysis) {
	if len(analyses) == 0 {
		return
	}
	// Deduplicate by lowercased package name: one lookup serves every analysis
	// that shares it, including case-variant PURLs.
	jobs := map[registryJobKey]*registryJob{}
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
		var fetch registryFetch
		switch parsed.Ecosystem() {
		case "pypi":
			if s.pypiClient != nil {
				fetch = s.fetchPyPIRegistryState
			}
		case "cargo":
			if s.cratesClient != nil {
				// Fetched for versioned PURLs too: AnalyzeFromGitHubURL synthesises
				// a version from the deps.dev stable release, so gating on
				// "unversioned only" would drop the fact for that entry path alone.
				fetch = s.fetchCratesRegistryState
			}
		}
		if fetch == nil {
			continue
		}
		key := registryJobKey{ecosystem: parsed.Ecosystem(), name: strings.ToLower(name)}
		job, seen := jobs[key]
		if !seen {
			job = &registryJob{fetch: fetch}
			jobs[key] = job
		}
		job.targets = append(job.targets, a)
	}
	if len(jobs) == 0 {
		return
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxRegistryStateWorkers)
	for key, job := range jobs {
		// Acquire before launching so a cancelled context stops dispatch instead
		// of parking a goroutine per remaining package.
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return
		}
		wg.Add(1)
		go func(name string, fetch registryFetch, targets []*domain.Analysis) {
			defer wg.Done()
			defer func() { <-sem }()
			state, found, err := fetch(ctx, name)
			if err != nil {
				slog.Debug("registry_state_fetch_failed", "name", name, "error", err)
				return
			}
			if !found || state == nil {
				return
			}
			for _, a := range targets {
				cp := *state
				a.RegistryState = &cp
			}
		}(key.name, job.fetch, job.targets)
	}
	wg.Wait()
}

// fetchPyPIRegistryState reports whether PyPI has yanked every release.
func (s *IntegrationService) fetchPyPIRegistryState(ctx context.Context, name string) (*domain.RegistryState, bool, error) {
	info, found, err := s.pypiClient.GetProject(ctx, name)
	if err != nil || !found || info == nil {
		return nil, found, err
	}
	return &domain.RegistryState{
		AllReleasesYanked: info.Yanked,
		Registry:          domain.RegistryPyPI,
		Reason:            sanitizeRegistryReason(info.YankedReason),
		Reference:         "https://pypi.org/project/" + url.PathEscape(name) + "/",
	}, true, nil
}

// fetchCratesRegistryState reports whether crates.io has yanked every version.
// crates.io yanks carry no reason text, so Reason stays empty.
func (s *IntegrationService) fetchCratesRegistryState(ctx context.Context, name string) (*domain.RegistryState, bool, error) {
	info, found, err := s.cratesClient.GetCrate(ctx, name)
	if err != nil || !found || info == nil {
		return nil, found, err
	}
	return &domain.RegistryState{
		AllReleasesYanked: info.Yanked,
		Registry:          domain.RegistryCrates,
		Reference:         "https://crates.io/crates/" + url.PathEscape(name),
	}, true, nil
}

// sanitizeRegistryReason makes a package maintainer's free-text yank reason safe
// to print, then collapses it to a single capped line.
//
// Both control characters (Cc, which carry the ANSI escape introducer and the
// carriage return) and format characters (Cf, which carry the bidirectional
// overrides and zero-width characters) are dropped: the CLI prints the reason
// verbatim, and either class can repaint or visually reorder the surrounding
// output. Whitespace is exempt so ordinary line breaks survive to be collapsed.
func sanitizeRegistryReason(raw string) string {
	stripped := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return r
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, raw)
	return domain.NormalizeSummary(stripped)
}
