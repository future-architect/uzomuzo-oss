package integration

import (
	"context"
	"net/url"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

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
	jobs := collectPackageJobs(analyses, func(ecosystem string) packageFetch[*domain.RegistryState] {
		switch ecosystem {
		case "pypi":
			if s.pypiClient != nil {
				return s.fetchPyPIRegistryState
			}
		case "cargo":
			if s.cratesClient != nil {
				// Fetched for versioned PURLs too: AnalyzeFromGitHubURL synthesises
				// a version from the deps.dev stable release, so gating on
				// "unversioned only" would drop the fact for that entry path alone.
				return s.fetchCratesRegistryState
			}
		}
		return nil
	})
	runPackageJobs(ctx, "registry_state", jobs, func(a *domain.Analysis, state *domain.RegistryState) {
		if state == nil {
			return
		}
		cp := *state
		a.RegistryState = &cp
	})
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
	return domain.SanitizeExternalSummary(raw)
}
