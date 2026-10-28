package analysis

// DependentsSource identifies the provider of a dependents observation.
type DependentsSource string

const (
	DependentsSourceDepsDev   DependentsSource = "deps.dev"
	DependentsSourceRubyGems  DependentsSource = "RubyGems"
	DependentsSourcePackagist DependentsSource = "Packagist"
)

// DependentsObservation records the scope and counts returned by one provider.
// A nil count means the provider did not report that measure. See ADR-0032.
type DependentsObservation struct {
	Source  DependentsSource
	Version string
	Direct  *int
	Total   *int
}
