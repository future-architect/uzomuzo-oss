package integration

import (
	"context"
	"log/slog"
	"sort"
	"strings"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// enrichScorecardFromAPI replaces deps.dev scorecard data (14 checks) with
// scorecard.dev data (all 18 checks) for analyses that have a GitHub repo URL.
//
// DDD Layer: Infrastructure (best-effort enrichment)
// This follows the same pattern as enrichAdvisorySeverity and enrichDependentCounts:
// collect unique keys, batch fetch, merge results back into analyses.
func (s *IntegrationService) enrichScorecardFromAPI(ctx context.Context, analyses map[string]*domain.Analysis) {
	if s.scorecardClient == nil {
		return
	}

	// Collect unique repo keys (github.com/owner/repo) from analyses.
	repoKeySet := make(map[string]struct{})
	for _, a := range analyses {
		if a == nil {
			continue
		}
		rk := repoKeyFromURL(a.RepoURL)
		if rk == "" {
			continue
		}
		repoKeySet[rk] = struct{}{}
	}
	if len(repoKeySet) == 0 {
		return
	}

	repoKeys := make([]string, 0, len(repoKeySet))
	for rk := range repoKeySet {
		repoKeys = append(repoKeys, rk)
	}
	sort.Strings(repoKeys)

	slog.Debug("scorecard_api_enrichment_start", "repo_count", len(repoKeys))
	results := s.scorecardClient.FetchScorecardBatch(ctx, repoKeys)
	slog.Debug("scorecard_api_enrichment_done", "fetched", len(results), "total", len(repoKeys))

	// Overwrite deps.dev scorecard data with the scorecard.dev result (strict superset).
	for _, a := range analyses {
		if a == nil {
			continue
		}
		rk := repoKeyFromURL(a.RepoURL)
		if rk == "" {
			continue
		}
		result, ok := results[rk]
		if !ok || result == nil {
			continue
		}
		// Only overwrite if scorecard.dev returned at least as many checks as deps.dev.
		// This guards against stale or partial scorecard.dev responses.
		if len(result.Scores) < len(a.Scores) {
			slog.Debug("scorecard_api_fewer_checks_skipped",
				"repo_key", rk,
				"api_checks", len(result.Scores),
				"existing_checks", len(a.Scores),
			)
			continue
		}
		slog.Debug("scorecard_api_overwrite",
			"repo_key", rk,
			"old_checks", len(a.Scores),
			"new_checks", len(result.Scores),
		)
		a.OverallScore = result.OverallScore
		a.Scores = result.Scores

		// Re-run archived detection from the "Maintained" check reason.
		// scorecard.dev may have a different or updated reason string.
		detectArchivedFromScorecard(a)
	}
}

// repoKeyFromURL converts a GitHub URL (https://github.com/owner/repo) to a
// scorecard.dev repo key (github.com/owner/repo). Returns empty for non-GitHub URLs.
func repoKeyFromURL(repoURL string) string {
	if repoURL == "" {
		return ""
	}
	owner, repo, err := common.ExtractGitHubOwnerRepo(repoURL)
	if err != nil || owner == "" || repo == "" {
		return ""
	}
	return "github.com/" + owner + "/" + repo
}

// detectArchivedFromScorecard checks the "Maintained" scorecard check reason
// for archived-project signals and sets RepoState.IsArchived accordingly.
// Note: This is a one-directional latch — it only sets IsArchived=true, never false.
// This is intentional: GitHub API may have already set IsArchived before scorecard
// enrichment runs, and we must not clear a previously confirmed archive status.
func detectArchivedFromScorecard(a *domain.Analysis) {
	if a == nil || a.Scores == nil {
		return
	}
	maintained, ok := a.Scores["Maintained"]
	if !ok || maintained == nil {
		return
	}
	if strings.Contains(strings.ToLower(maintained.Reason()), "project is archived") {
		if a.RepoState == nil {
			a.RepoState = &domain.RepoState{}
		}
		a.RepoState.IsArchived = true
	}
}
