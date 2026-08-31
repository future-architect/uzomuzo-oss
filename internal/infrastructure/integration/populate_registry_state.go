package integration

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// Registry names recorded in Analysis.RegistryState.Registry.
const (
	registryNamePyPI   = "PyPI"
	registryNameCrates = "crates.io"
)

// enrichRegistryState populates Analysis.RegistryState for pypi and cargo
// analyses with the registry's package-level withdrawal fact ("every published
// release is yanked"). No-op when the corresponding client is unwired.
//
// Best-effort: a fetch failure leaves RegistryState nil, which the lifecycle
// assessor reads as "not fetched" rather than "nothing yanked". A successful
// fetch always writes RegistryState, including the AllReleasesYanked=false case.
//
// Unlike enrichPyPISummary this does not require a populated Repository: the
// fact is asserted by the registry and is independent of the source repository.
//
// DDD Layer: Infrastructure (best-effort parallel enrichment, mirroring the
// name-deduplicated WaitGroup fan-out of enrichPyPISummary).
func (s *IntegrationService) enrichRegistryState(ctx context.Context, analyses map[string]*domain.Analysis) {
	if len(analyses) == 0 {
		return
	}
	pypiJobs := map[string][]*domain.Analysis{}
	cargoJobs := map[string][]*domain.Analysis{}
	parser := purl.NewParser()
	for _, a := range analyses {
		if a == nil || a.Package == nil {
			continue
		}
		parsed, err := parser.Parse(a.Package.PURL)
		if err != nil {
			continue
		}
		// pypi and cargo PURLs carry no namespace. A namespaced PURL would make
		// PackageName() drop a segment and attribute another package's fact here.
		if strings.TrimSpace(parsed.Namespace()) != "" {
			continue
		}
		name := strings.TrimSpace(parsed.PackageName())
		if name == "" {
			continue
		}
		switch parsed.Ecosystem() {
		case "pypi":
			if s.pypiClient != nil {
				key := strings.ToLower(name)
				pypiJobs[key] = append(pypiJobs[key], a)
			}
		case "cargo":
			if s.cratesClient != nil {
				// Fetched for versioned PURLs too: AnalyzeFromGitHubURL synthesises
				// a version from the deps.dev stable release, so gating on
				// "unversioned only" would drop the fact for that entry path alone.
				cargoJobs[name] = append(cargoJobs[name], a)
			}
		}
	}
	if len(pypiJobs) == 0 && len(cargoJobs) == 0 {
		return
	}

	var wg sync.WaitGroup
	for name, targets := range pypiJobs {
		wg.Add(1)
		go func(name string, targets []*domain.Analysis) {
			defer wg.Done()
			info, found, err := s.pypiClient.GetProject(ctx, name)
			if err != nil {
				slog.Debug("registry_state_fetch_failed", "registry", registryNamePyPI, "name", name, "error", err)
				return
			}
			if !found || info == nil {
				return
			}
			state := &domain.RegistryState{
				AllReleasesYanked: info.Yanked,
				Registry:          registryNamePyPI,
				Reason:            strings.TrimSpace(info.YankedReason),
				Reference:         "https://pypi.org/project/" + name + "/",
			}
			assignRegistryState(targets, state)
		}(name, targets)
	}
	for name, targets := range cargoJobs {
		wg.Add(1)
		go func(name string, targets []*domain.Analysis) {
			defer wg.Done()
			info, found, err := s.cratesClient.GetCrate(ctx, name)
			if err != nil {
				slog.Debug("registry_state_fetch_failed", "registry", registryNameCrates, "name", name, "error", err)
				return
			}
			if !found || info == nil {
				return
			}
			// crates.io yanks carry no reason text.
			state := &domain.RegistryState{
				AllReleasesYanked: info.Yanked,
				Registry:          registryNameCrates,
				Reference:         "https://crates.io/crates/" + name,
			}
			assignRegistryState(targets, state)
		}(name, targets)
	}
	wg.Wait()
}

// assignRegistryState gives each analysis its own copy so callers cannot mutate
// a shared value through one analysis.
func assignRegistryState(targets []*domain.Analysis, state *domain.RegistryState) {
	for _, a := range targets {
		cp := *state
		a.RegistryState = &cp
	}
}
