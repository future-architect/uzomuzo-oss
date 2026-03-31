package csv

import (
	"encoding/csv"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"sort"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// getLifecycleAssessmentResult returns a user-friendly lifecycle assessment result for reporting
func getLifecycleAssessmentResult(a *domain.Analysis) (string, string) {
	if a == nil || a.AxisResults == nil {
		return "Unknown", "No lifecycle assessment available"
	}
	if res := a.AxisResults[domain.LifecycleAxis]; res != nil {
		return string(res.Label), res.Reason
	}
	return "Review Needed", "No lifecycle assessment available"
}

// IsSuccessful indicates whether the analysis is usable for reporting
func IsSuccessful(a *domain.Analysis) bool {
	if a == nil {
		return false
	}
	// Successful if no error and we have scores or a lifecycle axis result
	return a.Error == nil && (len(a.Scores) > 0 || a.AxisResults != nil && a.AxisResults[domain.LifecycleAxis] != nil)
}

// getAllCheckNames collects all unique Scorecard check names from domain analyses
func getAllCheckNames(analyses map[string]*domain.Analysis) []string {
	nameSet := make(map[string]struct{})
	for _, analysis := range analyses {
		if analysis == nil || len(analysis.Scores) == 0 {
			continue
		}
		for name := range analysis.Scores {
			nameSet[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ExportScorecard writes the scorecard data to a CSV file
//
// DDD Layer: Infrastructure (CSV export implementation)
// Args: analyses - map of PURL to domain.Analysis, filename - destination CSV path
// Returns: error if any I/O or formatting failure occurs
func ExportScorecard(analyses map[string]*domain.Analysis, filename string) (err error) {
	file, err := os.Create(filename)
	if err != nil {
		return common.NewIOError("failed to create CSV file", err).
			WithContext("filename", filename)
	}
	defer func() {
		if cerr := file.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	writer := csv.NewWriter(file)
	defer func() {
		writer.Flush()
		if werr := writer.Error(); werr != nil && err == nil {
			err = common.NewIOError("failed to flush scorecard CSV writer", werr).
				WithContext("filename", filename)
		}
	}()

	checkNames := getAllCheckNames(analyses)

	// Create CSV headers
	headers := []string{"purl", "repoURL", "scorecard", "api", "release", "star", "Canary", "Latest", "Lastcommit", "archived", "disabled", "Fork", "overallScore", "dependents", "directDeps", "transitiveDeps"}
	headers = append(headers, checkNames...)
	headers = append(headers, "Result", "Reason", "RepoState")

	if err := writer.Write(headers); err != nil {
		return common.NewIOError("failed to write CSV headers", err).
			WithContext("filename", filename).
			WithContext("headers", headers)
	}

	// Create data rows
	var csvCount, skippedNilAnalysis, skippedNilRepo int
	for purl, analysis := range analyses {
		if analysis == nil {
			skippedNilAnalysis++
			continue
		}
		if analysis.Repository == nil {
			skippedNilRepo++
			continue
		}

		csvCount++

		scoreMap := analysis.GetCheckMap()

		scorecardURL := fmt.Sprintf("https://scorecard.dev/viewer/?uri=%s", url.QueryEscape(analysis.Repository.URL))
		apiURL := fmt.Sprintf("https://api.scorecard.dev/projects/%s", analysis.Repository.URL)

		// Get release endpoints - handle missing release data
		releaseEndpoint := ""
		canaryDays := "0"
		latestDays := "0"
		if analysis.ReleaseInfo != nil {
			// Domain ReleaseInfo doesn't have an Endpoint field, use repo URL
			releaseEndpoint = analysis.RepoURL
			if analysis.ReleaseInfo.PreReleaseVersion != nil && !analysis.ReleaseInfo.PreReleaseVersion.PublishedAt.IsZero() {
				canaryDays = fmt.Sprintf("%d", daysSince(analysis.ReleaseInfo.PreReleaseVersion.PublishedAt))
			}
			if analysis.ReleaseInfo.StableVersion != nil && !analysis.ReleaseInfo.StableVersion.PublishedAt.IsZero() {
				latestDays = fmt.Sprintf("%d", daysSince(analysis.ReleaseInfo.StableVersion.PublishedAt))
			}
		}

		// Get repository state information - handle missing repo state data
		starCount := "0"
		lastCommitDays := "0"
		archived := "false"
		disabled := "false"
		forked := "false" // Domain RepoState doesn't track fork status
		repoStateStr := ""
		if analysis.Repository != nil {
			starCount = fmt.Sprintf("%d", analysis.Repository.StarsCount)
		}
		if analysis.RepoState != nil {
			lastCommitDays = fmt.Sprintf("%d", analysis.RepoState.DaysSinceLastCommit)
			archived = fmt.Sprintf("%t", analysis.RepoState.IsArchived)
			disabled = fmt.Sprintf("%t", analysis.RepoState.IsDisabled)
			// Use analysis metadata for repo state string representation
			repoStateStr = fmt.Sprintf("Archived:%t,Disabled:%t", analysis.RepoState.IsArchived, analysis.RepoState.IsDisabled)
		}

		record := []string{
			purl,
			analysis.RepoURL,
			scorecardURL,
			apiURL,
			releaseEndpoint,
			starCount,
			canaryDays,
			latestDays,
			lastCommitDays,
			archived,
			disabled,
			forked,
			fmt.Sprintf("%.2f", analysis.OverallScore),
			fmt.Sprintf("%d", analysis.DependentCount),      // 0 = unknown; CLI omits zero but CSV always emits for machine-readability
			fmt.Sprintf("%d", analysis.DirectDepsCount),     // 0 = unknown or unsupported ecosystem
			fmt.Sprintf("%d", analysis.TransitiveDepsCount), // 0 = unknown or unsupported ecosystem
		}

		// Add check scores
		for _, name := range checkNames {
			if v, ok := scoreMap[name]; ok {
				record = append(record, fmt.Sprintf("%.2f", v))
			} else {
				record = append(record, "") // Empty cell
			}
		}

		// Add lifecycle assessment result
		label, reason := getLifecycleAssessmentResult(analysis)
		record = append(record, label, reason, repoStateStr)

		if err := writer.Write(record); err != nil {
			return common.NewIOError("failed to write CSV record", err).
				WithContext("purl", purl).
				WithContext("filename", filename)
		}
	}

	slog.Debug("csv_export_complete", "written", csvCount, "skipped_nil_analysis", skippedNilAnalysis, "skipped_nil_repo", skippedNilRepo)
	fmt.Printf("Scorecard data written to CSV file: %s\n", filename)
	return nil
}

func daysSince(date time.Time) int {
	now := time.Now()
	duration := now.Sub(date)
	return int(duration.Hours() / 24)
}
