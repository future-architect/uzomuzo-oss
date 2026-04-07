package integration

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// AnalyzeFromGitHubURLs processes multiple GitHub URLs concurrently.
// Extracted from service.go to isolate GitHub URL workflow.
func (s *IntegrationService) AnalyzeFromGitHubURLs(ctx context.Context, githubURLs []string) (map[string]*domain.Analysis, error) {
	if len(githubURLs) == 0 {
		return make(map[string]*domain.Analysis), nil
	}

	slog.Debug("starting_github_url_batch", "url_count", len(githubURLs))
	analyses := make(map[string]*domain.Analysis)

	type urlResult struct {
		githubURL string
		analysis  *domain.Analysis
	}
	resultChan := make(chan urlResult, len(githubURLs))
	urlChan := make(chan string, len(githubURLs))
	for _, u := range githubURLs {
		urlChan <- u
	}
	close(urlChan)

	maxWorkers := 20
	if s.config != nil && s.config.GitHub.MaxConcurrency > 0 {
		maxWorkers = s.config.GitHub.MaxConcurrency
	}
	if maxWorkers > 30 {
		maxWorkers = 30
	}
	if maxWorkers > len(githubURLs) {
		maxWorkers = len(githubURLs)
	}

	var wg sync.WaitGroup
	for i := 0; i < maxWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for gh := range urlChan {
				analysis, err := s.AnalyzeFromGitHubURL(ctx, gh)
				if err != nil {
					// On failure prior to deriving a PURL, we store the GitHub URL in OriginalPURL only.
					analysis = &domain.Analysis{OriginalPURL: gh, EffectivePURL: "", AnalyzedAt: time.Now(), Error: fmt.Errorf("failed to process GitHub URL '%s': %w", gh, err)}
					analysis.EnsureCanonical() // Will not set CanonicalKey (no pkg: prefix) which is acceptable.
				}
				resultChan <- urlResult{githubURL: gh, analysis: analysis}
			}
		}()
	}

	go func() { wg.Wait(); close(resultChan) }()
	for r := range resultChan {
		analyses[r.githubURL] = r.analysis
	}

	// Scorecard enrichment (best-effort, scorecard.dev returns all 18 checks).
	s.enrichScorecardFromAPI(ctx, analyses)

	// Advisory severity enrichment (best-effort, after all analyses are populated).
	s.enrichAdvisorySeverity(ctx, analyses)

	slog.Debug("completed_github_url_batch", "total", len(analyses))
	return analyses, nil
}
