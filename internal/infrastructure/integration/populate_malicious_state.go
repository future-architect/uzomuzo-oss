package integration

import (
	"context"

	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/osv"
)

// enrichMaliciousState checks supported packages for OSV malicious advisories.
func (s *IntegrationService) enrichMaliciousState(ctx context.Context, analyses map[string]*domain.Analysis) {
	if len(analyses) == 0 || s.osvClient == nil {
		return
	}
	jobs := collectPackageJobs(analyses, func(parsed *purl.ParsedPURL) (packageJobKey, packageFetch[[]domain.AdvisoryRecord]) {
		eco, name, ok := osv.OSVPackageFor(parsed)
		if !ok {
			return packageJobKey{}, nil
		}
		return packageJobKey{ecosystem: eco, name: name}, func(ctx context.Context, name string) ([]domain.AdvisoryRecord, bool, error) {
			records, err := s.osvClient.QueryPackage(ctx, eco, name)
			return records, true, err
		}
	})
	runPackageJobs(ctx, "malicious_state", jobs, func(a *domain.Analysis, records []domain.AdvisoryRecord) {
		parsed, err := purl.NewParser().Parse(a.Package.PURL)
		if err != nil {
			return
		}
		eco, name, ok := osv.OSVPackageFor(parsed)
		if !ok {
			return
		}
		state := domain.ClassifyMalicious(records, eco, name, parsed.Version())
		a.MaliciousState = &state
	}, func(_ packageJobKey, targets []*domain.Analysis) {
		for _, a := range targets {
			a.MaliciousState = &domain.MaliciousState{Status: domain.MaliciousStatusLookupFailed}
		}
	})
}
