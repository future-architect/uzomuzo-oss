package analysis

// BuildIntegrityLabel represents the build integrity assessment outcome.
type BuildIntegrityLabel string

const (
	// BuildLabelHardened indicates strong build pipeline protection (score >= 7.5).
	BuildLabelHardened BuildIntegrityLabel = "Hardened"
	// BuildLabelModerate indicates moderate build pipeline protection (score 2.5-7.4).
	BuildLabelModerate BuildIntegrityLabel = "Moderate"
	// BuildLabelWeak indicates minimal build pipeline protection (score < 2.5).
	BuildLabelWeak BuildIntegrityLabel = "Weak"
	// BuildLabelUngraded indicates insufficient data to assess build integrity.
	BuildLabelUngraded BuildIntegrityLabel = "Ungraded"
)

// String returns the string representation.
func (b BuildIntegrityLabel) String() string { return string(b) }

// ClassifyBuildIntegrity maps a score (0-10) to a BuildIntegrityLabel.
// Returns BuildLabelUngraded for negative scores (no data).
func ClassifyBuildIntegrity(score float64) BuildIntegrityLabel {
	if score < 0 {
		return BuildLabelUngraded
	}
	switch {
	case score >= 7.5:
		return BuildLabelHardened
	case score >= 2.5:
		return BuildLabelModerate
	default:
		return BuildLabelWeak
	}
}
