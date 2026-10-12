package integration

import (
	"context"

	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/osv"
)

// primeOSVNoAdvisories asks OSV.dev in one batch which of the analysed packages
// have no advisory, so that enrichAdvisoryDBState and enrichMaliciousState
// query only the packages that have one. Without it each distinct package costs
// its own request: about 30 seconds for the 839 packages of the npm CLI
// lockfile, against under 2 seconds for the batch.
func (s *IntegrationService) primeOSVNoAdvisories(ctx context.Context, analyses map[string]*domain.Analysis) {
	if len(analyses) == 0 || s.osvClient == nil {
		return
	}
	parser := purl.NewParser()
	pkgs := make([]osv.PackageRef, 0, len(analyses))
	for _, a := range analyses {
		if a == nil || a.Package == nil {
			continue
		}
		parsed, err := parser.Parse(a.Package.PURL)
		if err != nil {
			continue
		}
		if eco, name, ok := osv.OSVPackageFor(parsed); ok {
			pkgs = append(pkgs, osv.PackageRef{Ecosystem: eco, Name: name})
		}
	}
	s.osvClient.PrimeNoAdvisories(ctx, pkgs)
}
