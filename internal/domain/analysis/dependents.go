package analysis

// DependentsSource identifies the provider of a dependents observation.
type DependentsSource string

// Known dependents providers.
const (
	// DependentsSourceDepsDev is the deps.dev GetDependents API (version-scoped).
	DependentsSourceDepsDev DependentsSource = "deps.dev"
	// DependentsSourceRubyGems is the RubyGems reverse-dependencies API.
	DependentsSourceRubyGems DependentsSource = "RubyGems"
	// DependentsSourcePackagist is the Packagist dependents API.
	DependentsSourcePackagist DependentsSource = "Packagist"
)

// DependentsObservation records the scope and counts returned by one provider.
// A nil count means the provider did not report that measure. See ADR-0033.
type DependentsObservation struct {
	// Source is the provider that reported the counts.
	Source DependentsSource
	// Version is the exact version queried. Empty when the provider counts
	// across all versions (RubyGems, Packagist).
	Version string
	// Direct is the number of packages declaring a direct dependency.
	// Nil when the provider did not report it.
	Direct *int
	// Total is the provider's count of all dependents, direct or indirect. It is
	// not the sum of direct and indirect counts.
	// Nil when not reported (always nil for RubyGems and Packagist).
	Total *int
}
