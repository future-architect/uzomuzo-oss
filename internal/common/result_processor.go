package common

import (
	"strings"

	domain "github.com/future-architect/uzomuzo/internal/domain/analysis"
)

// ResultProcessor provides common result processing operations
type ResultProcessor struct{}

// NewResultProcessor creates a new result processor
func NewResultProcessor() *ResultProcessor {
	return &ResultProcessor{}
}

// Package types supported by deps.dev API
var AllowedPackageTypes = map[string]bool{
	"cargo":    true,
	"golang":   true,
	"maven":    true,
	"npm":      true,
	"nuget":    true,
	"pypi":     true,
	"gem":      true,
	"composer": true,
	"github":   true, // temporarily added for debugging
}

// FilterPackageTypes filters PURLs by allowed package types
func (rp *ResultProcessor) FilterPackageTypes(purls []string) (allowed []string, notAllowed []string) {
	for _, s := range purls {
		// Extract package type from PURL (first part after "pkg:")
		packageType := strings.SplitN(strings.TrimPrefix(s, "pkg:"), "/", 2)[0]

		if AllowedPackageTypes[packageType] {
			allowed = append(allowed, s)
		} else {
			notAllowed = append(notAllowed, s)
		}
	}
	return allowed, notAllowed
}

// ColorizeResult adds visual indicators to lifecycle assessment results based on official labels
func ColorizeResult(result string) string {
	switch result {
	case string(domain.LabelActive):
		return "🟢 " + result
	case string(domain.LabelStalled):
		return "⚪ " + result
	case string(domain.LabelLegacySafe):
		return "🔵 " + result
	case string(domain.LabelEOLConfirmed):
		return "🔴 " + result
	case string(domain.LabelEOLEffective):
		return "🛑 " + result
	case string(domain.LabelEOLScheduled):
		return "🟠 " + result
	case string(domain.LabelReviewNeeded):
		return "⚠️ " + result
	default:
		return "⚪ " + result
	}
}
