package github

import (
	"strings"
	"time"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// processCommitHistory processes commit history and returns commit statistics
func (c *Client) processCommitHistory(history CommitHistory) *domain.CommitStats {
	if len(history.Nodes) == 0 {
		return &domain.CommitStats{}
	}

	totalCommits := len(history.Nodes)
	userCommits := 0
	botCommits := 0

	for _, commit := range history.Nodes {
		// Check if author and user information exists
		if commit.Author.User != nil && commit.Author.User.Login != "" {
			// Simple heuristic: if login contains "bot", consider it a bot
			if strings.Contains(strings.ToLower(commit.Author.User.Login), "bot") {
				botCommits++
			} else {
				userCommits++
			}
		} else {
			userCommits++ // Unknown author assumed to be human
		}
	}

	return &domain.CommitStats{
		Total:       totalCommits,
		UserCommits: userCommits,
		BotCommits:  botCommits,
		UserRatio:   float64(userCommits) / float64(totalCommits),
		BotRatio:    float64(botCommits) / float64(totalCommits),
	}
}

// getLatestHumanCommit finds the latest commit by a human author
func (c *Client) getLatestHumanCommit(history CommitHistory) *time.Time {
	for _, commit := range history.Nodes {
		// Skip if it's a bot commit - check for nil user first
		if commit.Author.User != nil && commit.Author.User.Login != "" &&
			strings.Contains(strings.ToLower(commit.Author.User.Login), "bot") {
			continue
		}

		// Parse commit date
		if commitTime, err := time.Parse(time.RFC3339, commit.CommittedDate); err == nil {
			return &commitTime
		}
	}
	return nil
}

// getLatestCommit finds the latest commit (including bot commits)
func (c *Client) getLatestCommit(history CommitHistory) *time.Time {
	if len(history.Nodes) == 0 {
		return nil
	}

	// The first commit in the list should be the latest
	if commitTime, err := time.Parse(time.RFC3339, history.Nodes[0].CommittedDate); err == nil {
		return &commitTime
	}
	return nil
}
