package integration

import (
	"context"
	"log/slog"
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
	jobs := map[string][]*domain.Analysis{}
	fetches := map[string]registryFetch{}
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
		key := parsed.Ecosystem() + ":" + strings.ToLower(name)
		if _, seen := jobs[key]; !seen {
			fetches[key] = fetch
		}
		jobs[key] = append(jobs[key], a)
	}
	if len(jobs) == 0 {
		return
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxRegistryStateWorkers)
	for key, targets := range jobs {
		// Acquire before launching so a cancelled context stops dispatch instead
		// of parking a goroutine per remaining package.
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return
		}
		name := strings.SplitN(key, ":", 2)[1]
		fetch := fetches[key]
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
		}(name, fetch, targets)
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
		Reference:         "https://pypi.org/project/" + name + "/",
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
		Reference:         "https://crates.io/crates/" + name,
	}, true, nil
}

// sanitizeRegistryReason makes a package maintainer's free-text yank reason safe
// to print: control characters (which include ANSI escape introducers) are
// dropped so the reason cannot repaint or spoof the terminal, and the remainder
// is collapsed to a single capped line.
func sanitizeRegistryReason(raw string) string {
	stripped := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			return -1
		}
		return r
	}, raw)
	return domain.NormalizeSummary(stripped)
}
