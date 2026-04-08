package eolevaluator

import (
	"context"
	"log/slog"
	"reflect"
	"strings"
	"sync"

	purl "github.com/future-architect/uzomuzo-oss/internal/common/purl"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/maven"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/npmjs"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/nuget"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/packagist"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/pypi"
)

// Evaluator implements domain.EOLEvaluatorPort by querying primary sources.
// --- Minimal client seam interfaces (Infrastructure local) ---
// These interfaces allow injecting fakes in tests without exporting new types.
// They purposefully expose only the evaluator-needed methods to keep surface small.
type packagistAbandonedClient interface {
	GetAbandoned(ctx context.Context, vendor, name string) (bool, string, error)
}
type nugetDeprecationClient interface {
	GetDeprecation(ctx context.Context, packageID string) (*nuget.DeprecationInfo, bool, error)
}
type mavenRelocationClient interface {
	GetRelocation(ctx context.Context, groupID, artifactID, version string) (*maven.RelocationInfo, bool, error)
}
type npmDeprecationClient interface {
	GetDeprecation(ctx context.Context, namespace, name, version string) (*npmjs.DeprecationInfo, bool, error)
}

type Evaluator struct {
	pg   packagistAbandonedClient
	ng   nugetDeprecationClient
	mvn  mavenRelocationClient
	npm  npmDeprecationClient
	pypi *pypi.Client
	// rule chain (rebuilt each EvaluateBatch)
	rules      []eolRuleFunc
	maxWorkers int
}

// eolRuleFunc represents a short-circuiting terminal rule that may decide final EOL status.
// Return true when the rule has conclusively set a terminal state and no further terminal rules
// should run (evidence adders will be skipped if short-circuited).
type eolRuleFunc func(ctx context.Context, key string, a *domain.Analysis, st *domain.EOLStatus) (done bool)

// NewEvaluator creates an EOL evaluator with registry-based rule chain.
func NewEvaluator(pg *packagist.Client) *Evaluator {
	var pgc packagistAbandonedClient = pg
	return &Evaluator{
		pg:         pgc,
		ng:         nuget.NewClient(),
		mvn:        maven.NewClient(),
		npm:        npmjs.NewClient(),
		pypi:       pypi.NewClient(),
		maxWorkers: 12,
	}
}

// SetMaxWorkers bounds parallelism (zero/negative => sequential).
func (e *Evaluator) SetMaxWorkers(n int) { e.maxWorkers = n }

// SetNuGetClient overrides the default NuGet client (useful for tests).
func (e *Evaluator) SetNuGetClient(ng *nuget.Client) { e.ng = ng }

// SetMavenClient overrides the default Maven client (useful for tests).
func (e *Evaluator) SetMavenClient(mv *maven.Client) { e.mvn = mv }

// SetNpmClient overrides the default npmjs client (useful for tests).
func (e *Evaluator) SetNpmClient(npm npmDeprecationClient) { e.npm = npm }

// SetPyPIClient overrides the default PyPI client (useful for tests).
func (e *Evaluator) SetPyPIClient(pc *pypi.Client) { e.pypi = pc }

// (Removed) suppressions support eliminated; heuristic-based evidence gathering removed.

// ensureRuleChain (re)builds the ordered terminal rules and evidence adders.
// Cheap enough to call every EvaluateBatch; keeps behavior in sync with feature flags.
func (e *Evaluator) ensureRuleChain() {
	// Terminal rules in priority order (first match wins / short-circuits)
	e.rules = []eolRuleFunc{
		func(ctx context.Context, key string, a *domain.Analysis, st *domain.EOLStatus) bool {
			return e.applyPackagistAbandoned(ctx, a, st)
		},
		func(ctx context.Context, key string, a *domain.Analysis, st *domain.EOLStatus) bool {
			return e.applyNuGetDeprecation(ctx, a, st)
		},
		func(ctx context.Context, key string, a *domain.Analysis, st *domain.EOLStatus) bool {
			return e.applyNpmStableDeprecation(ctx, a, st)
		},
		func(ctx context.Context, key string, a *domain.Analysis, st *domain.EOLStatus) bool {
			return e.applyNpmPURLDeprecation(ctx, a, st)
		},
		func(ctx context.Context, key string, a *domain.Analysis, st *domain.EOLStatus) bool { // PyPI classifier explicit inactivity
			return e.applyPyPIClassifier(ctx, a, st)
		},
		func(ctx context.Context, key string, a *domain.Analysis, st *domain.EOLStatus) bool {
			return e.applyMavenRelocation(ctx, a, st)
		},
	}
}

// applyPackagistAbandoned detects abandoned Composer/Packagist packages.
func (e *Evaluator) applyPackagistAbandoned(ctx context.Context, a *domain.Analysis, status *domain.EOLStatus) (done bool) {
	if status.State == domain.EOLEndOfLife || a == nil || a.Package == nil || e.pg == nil {
		return false
	}
	pp := purl.NewParser()
	parsed, err := pp.Parse(a.Package.PURL)
	if err != nil {
		return false
	}
	eco := strings.ToLower(parsed.GetEcosystem())
	if eco != "composer" && eco != "packagist" {
		return false
	}
	vendor, name := parseComposerFromPURL(a.Package.PURL)
	if vendor == "" || name == "" {
		return false
	}
	abd, succ, err := e.pg.GetAbandoned(ctx, vendor, name)
	if err != nil {
		slog.Error("eol: packagist abandoned fetch failed", "vendor", vendor, "name", name, "error", err)
		return false
	}
	if !abd {
		return false
	}
	slog.Debug("eol: packagist package abandoned", "vendor", vendor, "name", name, "successor", succ)
	status.State = domain.EOLEndOfLife
	pkgUI := "https://packagist.org/packages/" + vendor + "/" + name
	pkgAPI := pkgUI + ".json"
	status.Evidences = append(status.Evidences, domain.EOLEvidence{
		Source:     "Packagist",
		Summary:    "Package marked abandoned. UI: " + pkgUI,
		Reference:  pkgAPI,
		Confidence: 1.0,
	})
	if succ != "" {
		status.Successor = succ
	}
	return true
}

// applyNuGetDeprecation checks NuGet deprecation metadata.
func (e *Evaluator) applyNuGetDeprecation(ctx context.Context, a *domain.Analysis, status *domain.EOLStatus) (done bool) {
	if status.State == domain.EOLEndOfLife || a == nil || a.Package == nil || e.ng == nil {
		return false
	}
	pp := purl.NewParser()
	parsed, err := pp.Parse(a.Package.PURL)
	if err != nil || strings.ToLower(parsed.GetEcosystem()) != "nuget" {
		return false
	}
	id := parseNuGetIDFromPURL(a.Package.PURL)
	if id == "" {
		return false
	}
	info, found, err := e.ng.GetDeprecation(ctx, id)
	if err != nil {
		slog.Error("eol: nuget deprecation fetch failed", "id", id, "error", err)
		return false
	}
	if !found || info == nil {
		return false
	}
	slog.Debug("eol: nuget deprecation fetched", "id", id, "reasons", info.Reasons, "alt", info.AlternatePackageID)
	newState, succ, evs := decideNuGetEOL(id, info)
	if len(evs) > 0 {
		status.Evidences = append(status.Evidences, evs...)
	}
	if newState == domain.EOLEndOfLife {
		status.State = domain.EOLEndOfLife
		if succ != "" {
			status.Successor = succ
		}
		return true
	}
	return false
}

// applyNpmStableDeprecation checks if the stable requested version is deprecated.

func (e *Evaluator) applyNpmStableDeprecation(ctx context.Context, a *domain.Analysis, status *domain.EOLStatus) (done bool) {
	if status.State == domain.EOLEndOfLife || a == nil || a.EffectivePURL == "" || e.npm == nil || a.ReleaseInfo == nil || a.ReleaseInfo.StableVersion == nil || a.ReleaseInfo.StableVersion.Version == "" {
		return false
	}
	purlParser := purl.NewParser()
	parsed, err := purlParser.Parse(a.EffectivePURL)
	if err != nil || parsed.GetEcosystem() != "npm" {
		return false
	}
	return e.checkNpmDeprecation(ctx, parsed.Namespace(), parsed.Name(), a.ReleaseInfo.StableVersion.Version, "npmjs_stable_version_is_eol", status)
}

// applyNpmPURLDeprecation is a fallback npm deprecation check that uses the version
// from EffectivePURL when ReleaseInfo.StableVersion is unavailable (e.g., deps.dev data lag).
// This addresses issue #218 where packages like vm2 are deprecated on npm but miss
// detection because the stable version data is absent.
func (e *Evaluator) applyNpmPURLDeprecation(ctx context.Context, a *domain.Analysis, status *domain.EOLStatus) (done bool) {
	if status.State == domain.EOLEndOfLife || a == nil || a.EffectivePURL == "" || e.npm == nil {
		return false
	}
	// Skip if applyNpmStableDeprecation already had a chance to run (StableVersion present).
	if a.ReleaseInfo != nil && a.ReleaseInfo.StableVersion != nil && a.ReleaseInfo.StableVersion.Version != "" {
		return false
	}
	purlParser := purl.NewParser()
	parsed, err := purlParser.Parse(a.EffectivePURL)
	if err != nil || parsed.GetEcosystem() != "npm" {
		return false
	}
	ver := parsed.Version()
	if ver == "" {
		return false
	}
	return e.checkNpmDeprecation(ctx, parsed.Namespace(), parsed.Name(), ver, "npmjs_purl_version_is_eol", status)
}

// checkNpmDeprecation is the shared core for npm deprecation detection.
// It queries the npm registry for the given namespace/name/version and populates
// status on confirmed EOL. logEvent identifies the caller in structured log output.
func (e *Evaluator) checkNpmDeprecation(ctx context.Context, ns, name, ver, logEvent string, status *domain.EOLStatus) (done bool) {
	// Guard against typed-nil interface (e.g., var c *npmjs.Client = nil passed to SetNpmClient).
	if isNilInterface(e.npm) {
		return false
	}
	info, found, err := e.npm.GetDeprecation(ctx, ns, name, ver)
	if err != nil || !found || info == nil {
		if err != nil {
			slog.Error("eol: npmjs deprecation check failed", "event", logEvent, "error", err, "namespace", ns, "name", name, "version", ver)
		}
		return false
	}
	pkgID := name
	if ns != "" {
		pkgID = ns + "/" + name
	}
	state, successor, evidences := decideNpmEOL(pkgID, ver, info)
	if state == domain.EOLEndOfLife {
		status.State = state
		status.Successor = successor
		if len(evidences) > 0 {
			status.Evidences = append(status.Evidences, evidences...)
		}
		slog.Debug("eol: npmjs package is eol", "event", logEvent, "pkg", pkgID, "version", ver, "successor", successor)
		return true
	}
	return false
}

// isNilInterface reports whether v is nil or a typed-nil interface (e.g., a nil
// pointer wrapped in an interface). It safely handles non-nilable dynamic types
// (struct values with value receivers) by checking the reflected Kind before
// calling IsNil.
func isNilInterface(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

// applyMavenRelocation detects Maven relocation metadata.
func (e *Evaluator) applyMavenRelocation(ctx context.Context, a *domain.Analysis, status *domain.EOLStatus) (done bool) {
	if status.State != domain.EOLEndOfLife && a != nil && a.Package != nil && e.mvn != nil {
		p := purl.NewParser()
		parsed, err := p.Parse(a.Package.PURL)
		if err != nil || parsed.GetEcosystem() != "maven" {
			return false
		}
		slog.Debug("eol: maven branch entered", "purl", a.Package.PURL)
		g, art, v := parseMavenFromPURL(a.Package.PURL)
		if g == "" || art == "" || v == "" {
			return false
		}
		slog.Debug("eol: maven parsed", "group", g, "artifact", art, "version", v)
		info, found, err := e.mvn.GetRelocation(ctx, g, art, v)
		if err != nil || !found || info == nil {
			return false
		}
		succGA := strings.Trim(info.GroupID+"/"+info.ArtifactID, "/")
		slog.Debug("eol: maven relocation detected", "g", g, "a", art, "v", v, "to", succGA, "message", info.Message)
		status.State = domain.EOLEndOfLife
		if succGA != "" {
			status.Successor = succGA
		}
		ui := "https://search.maven.org/artifact/" + g + "/" + art + "/" + v + "/jar"
		ref := info.POMURL
		if ref == "" {
			ref = ui
		}
		summary := "Artifact relocated"
		if info.Message != "" {
			summary = summary + ": " + info.Message
		}
		summary = summary + " UI: " + ui
		status.Evidences = append(status.Evidences, domain.EOLEvidence{
			Source:     "Maven",
			Summary:    summary,
			Reference:  ref,
			Confidence: 0.95,
		})
		return true
	}
	return false
}

// applyPyPIExplicit checks PyPI project metadata for explicit deprecation/EOL phrases.
// Short-circuits on confirmed EOL. This is a primary-source registry signal.
// applyPyPIClassifier: terminal rule – only classifier-based explicit inactivity.
func (e *Evaluator) applyPyPIClassifier(ctx context.Context, a *domain.Analysis, status *domain.EOLStatus) (done bool) {
	if status.State == domain.EOLEndOfLife || a == nil || a.Package == nil || a.Package.PURL == "" || e.pypi == nil {
		return false
	}
	pp := purl.NewParser()
	parsed, err := pp.Parse(a.Package.PURL)
	if err != nil || parsed.GetEcosystem() != "pypi" {
		return false
	}
	name := strings.ToLower(parsed.Name())
	info, found, err := e.pypi.GetProject(ctx, name)
	if err != nil {
		slog.Error("eol: pypi fetch failed", "name", name, "error", err)
		return false
	}
	if !found || info == nil {
		return false
	}
	if hasPyPIInactiveClassifier(info.Classifiers) { // explicit EOL
		status.State = domain.EOLEndOfLife
		status.Evidences = append(status.Evidences, domain.EOLEvidence{
			Source:     "PyPI",
			Summary:    "Classifier: Development Status :: 7 - Inactive",
			Reference:  "https://pypi.org/project/" + info.Name + "/",
			Confidence: 1.0,
		})
		slog.Debug("eol: pypi inactive classifier explicit EOL", "name", name)
		return true
	}
	return false
}

// EvaluateBatch computes EOL status for the given analyses keyed by input key.
func (e *Evaluator) EvaluateBatch(ctx context.Context, analyses map[string]*domain.Analysis) (map[string]domain.EOLStatus, error) {
	out := make(map[string]domain.EOLStatus, len(analyses))
	type job struct {
		key string
		a   *domain.Analysis
	}
	jobs := make(chan job, len(analyses))
	var mu sync.Mutex
	var wg sync.WaitGroup

	workers := e.maxWorkers
	if workers <= 0 || workers > len(analyses) {
		workers = len(analyses)
	}
	if workers == 0 {
		return out, nil
	}

	// Build rule chain each batch (lightweight) so feature flag toggles are honored.
	e.ensureRuleChain()

	worker := func() {
		defer wg.Done()
		for j := range jobs {
			status := domain.EOLStatus{State: domain.EOLNotEOL}
			for _, rule := range e.rules {
				if rule(ctx, j.key, j.a, &status) {
					break
				}
			}
			mu.Lock()
			out[j.key] = status
			mu.Unlock()
		}
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go worker()
	}
	for k, a := range analyses {
		jobs <- job{key: k, a: a}
	}
	close(jobs)
	wg.Wait()
	return out, nil
}
