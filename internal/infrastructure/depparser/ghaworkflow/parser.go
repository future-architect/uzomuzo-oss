// Package ghaworkflow extracts GitHub repository URLs from GitHub Actions workflow YAML files.
//
// DDD Layer: Infrastructure (external format parsing)
package ghaworkflow

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// workflowFile is the minimal YAML structure needed to extract `uses:` references.
type workflowFile struct {
	Jobs map[string]job `yaml:"jobs"`
}

type job struct {
	// Uses is set for reusable workflows (e.g., "owner/repo/.github/workflows/ci.yml@main").
	Uses  string `yaml:"uses"`
	Steps []step `yaml:"steps"`
}

type step struct {
	Uses string `yaml:"uses"`
}

// ParseGitHubURLs reads a GitHub Actions workflow YAML file and returns
// the unique GitHub repository URLs referenced in `uses:` directives.
// Local actions (./path) and Docker references (docker://image) are skipped.
// Jobs are iterated in sorted key order for deterministic output.
func ParseGitHubURLs(data []byte) ([]string, error) {
	var wf workflowFile
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil, fmt.Errorf("failed to parse GitHub Actions workflow YAML: %w", err)
	}

	// Sort job keys for deterministic output ordering.
	jobNames := make([]string, 0, len(wf.Jobs))
	for name := range wf.Jobs {
		jobNames = append(jobNames, name)
	}
	sort.Strings(jobNames)

	seen := make(map[string]struct{})
	var urls []string

	for _, name := range jobNames {
		j := wf.Jobs[name]
		// Reusable workflow reference at job level.
		if u := extractGitHubURL(j.Uses); u != "" {
			if _, exists := seen[u]; !exists {
				seen[u] = struct{}{}
				urls = append(urls, u)
			}
		}
		for _, s := range j.Steps {
			if u := extractGitHubURL(s.Uses); u != "" {
				if _, exists := seen[u]; !exists {
					seen[u] = struct{}{}
					urls = append(urls, u)
				}
			}
		}
	}

	return urls, nil
}

// extractGitHubURL converts a `uses:` value to a GitHub repository URL.
// Returns "" for empty strings, local actions (./path), and Docker references (docker://).
func extractGitHubURL(uses string) string {
	uses = strings.TrimSpace(uses)
	if uses == "" {
		return ""
	}

	// Skip local actions.
	if strings.HasPrefix(uses, "./") || strings.HasPrefix(uses, "../") {
		return ""
	}

	// Skip Docker container references.
	if strings.HasPrefix(uses, "docker://") {
		return ""
	}

	// Strip version/ref suffix: "owner/repo@ref" or "owner/repo/path@ref".
	ref := uses
	if idx := strings.Index(ref, "@"); idx > 0 {
		ref = ref[:idx]
	}

	// Extract owner/repo from "owner/repo" or "owner/repo/subpath".
	parts := strings.SplitN(ref, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return ""
	}

	return "https://github.com/" + parts[0] + "/" + parts[1]
}

// IsWorkflowYAMLByPath reports whether filePath is inside a .github/workflows/ directory
// and has a YAML extension. This is the fast, I/O-free path check.
func IsWorkflowYAMLByPath(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext != ".yml" && ext != ".yaml" {
		return false
	}
	// Use "/.github/workflows/" with leading slash to avoid false positives from
	// paths like "/tmp/not.github/workflows/foo.yml".
	normalized := filepath.ToSlash(filePath)
	return strings.Contains(normalized, "/.github/workflows/") || strings.HasPrefix(normalized, ".github/workflows/")
}

// IsWorkflowYAML reports whether the file at filePath looks like a GitHub Actions workflow.
// It checks the file extension and either the path or a content prefix for workflow markers.
func IsWorkflowYAML(filePath string, prefix []byte) bool {
	if IsWorkflowYAMLByPath(filePath) {
		return true
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	if ext != ".yml" && ext != ".yaml" {
		return false
	}

	// Content-based fallback: look for top-level "on:" and "jobs:" keys.
	return hasWorkflowMarkers(prefix)
}

// hasWorkflowMarkers checks whether the byte prefix contains top-level YAML keys
// that indicate a GitHub Actions workflow file: "on:" (or quoted "on":) and "jobs:".
func hasWorkflowMarkers(data []byte) bool {
	s := string(data)
	hasOn := strings.Contains(s, "\non:") || strings.HasPrefix(s, "on:") ||
		strings.Contains(s, "\n\"on\":") || strings.HasPrefix(s, "\"on\":")
	hasJobs := strings.Contains(s, "\njobs:") || strings.HasPrefix(s, "jobs:")
	return hasOn && hasJobs
}
