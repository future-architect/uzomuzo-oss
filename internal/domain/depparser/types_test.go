package depparser_test

import (
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
)

func TestParsedDependency_Fields(t *testing.T) {
	dep := depparser.ParsedDependency{
		PURL:      "pkg:npm/express@4.18.2",
		Ecosystem: "npm",
		Name:      "express",
		Version:   "4.18.2",
	}
	if dep.PURL != "pkg:npm/express@4.18.2" {
		t.Errorf("PURL = %q, want %q", dep.PURL, "pkg:npm/express@4.18.2")
	}
	if dep.Ecosystem != "npm" {
		t.Errorf("Ecosystem = %q, want %q", dep.Ecosystem, "npm")
	}
}

func TestParsedDependency_ZeroValue(t *testing.T) {
	var dep depparser.ParsedDependency
	if dep.PURL != "" {
		t.Errorf("zero value PURL should be empty, got %q", dep.PURL)
	}
}
