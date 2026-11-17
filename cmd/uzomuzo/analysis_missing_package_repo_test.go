package main

import (
	"context"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/application"
	"github.com/future-architect/uzomuzo-oss/internal/common"
	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depsdev"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/github"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/integration"
)

type missingPackageRepoDepsDevClient struct{ depsdev.Client }

func (missingPackageRepoDepsDevClient) GetDetailsForPURLs(_ context.Context, purls []string) (map[string]*depsdev.BatchResult, error) {
	result := make(map[string]*depsdev.BatchResult, len(purls))
	for _, p := range purls {
		result[p] = &depsdev.BatchResult{
			PURL:    p,
			RepoURL: "https://gitlab.example/org/example",
			ReleaseInfo: depsdev.ReleaseInfo{StableVersion: depsdev.Version{
				VersionKey: depsdev.VersionKey{Version: "1.5.7"}, PURL: p + "@1.5.7",
			}},
		}
	}
	return result, nil
}

func (missingPackageRepoDepsDevClient) GetPackageVersionLicenses(context.Context, string) ([]string, error) {
	return nil, nil
}

func (missingPackageRepoDepsDevClient) FetchDependentCountBatch(context.Context, []string) map[string]*depsdev.DependentsResponse {
	return nil
}

func (missingPackageRepoDepsDevClient) FetchDependenciesBatch(context.Context, []string) map[string]*depsdev.DependenciesResponse {
	return nil
}

func (missingPackageRepoDepsDevClient) FetchAdvisoriesBatch(context.Context, []string) map[string]*depsdev.AdvisoryDetail {
	return nil
}

func (missingPackageRepoDepsDevClient) FetchTransitiveAdvisoryKeys(context.Context, *depsdev.DependenciesResponse) (map[string][]depsdev.AdvisoryKey, error) {
	return nil, nil
}

func TestProcessBatchPURLs_MissingPackageRepositoryClearsError(t *testing.T) {
	const p = "pkg:maven/org.apache.directory.server/apacheds-core"
	source := integration.NewIntegrationService(github.NewClient(&config.Config{}), missingPackageRepoDepsDevClient{})
	integrationResult, err := source.AnalyzeFromPURLs(context.Background(), []string{p})
	if err != nil {
		t.Fatalf("AnalyzeFromPURLs: %v", err)
	}
	if got := integrationResult[p]; got == nil || !common.IsResourceNotFoundError(got.Error) || got.Repository == nil {
		t.Fatalf("integration result = %+v, want not-found error and resolved repository", got)
	}
	svc := application.NewAnalysisService(source)
	results, err := svc.ProcessBatchPURLs(context.Background(), []string{p})
	if err != nil {
		t.Fatalf("ProcessBatchPURLs: %v", err)
	}
	got := results[p]
	if got == nil {
		t.Fatal("missing analysis")
	}
	if got.Error != nil {
		t.Errorf("Error = %v, want nil after repository resolution", got.Error)
	}
	if got.RepoURL != "https://gitlab.example/org/example" || got.Repository == nil || got.Repository.URL != got.RepoURL {
		t.Errorf("repository = %+v, RepoURL = %q, want GitLab repository", got.Repository, got.RepoURL)
	}
	if got.ReleaseInfo == nil || got.ReleaseInfo.StableVersion == nil || got.ReleaseInfo.StableVersion.Version != "1.5.7" {
		t.Errorf("ReleaseInfo = %+v, want Stable 1.5.7", got.ReleaseInfo)
	}
}
