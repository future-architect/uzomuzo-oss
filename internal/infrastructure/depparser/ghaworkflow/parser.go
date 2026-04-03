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

// ActionRef represents a parsed GitHub Actions uses: reference with full path detail.
type ActionRef struct {
	Owner string // e.g., "actions"
	Repo  string // e.g., "cache"
	Path  string // subdirectory path, empty for root actions (e.g., "save" for actions/cache/save)
	Ref   string // version tag/sha/branch (e.g., "v4")
}

// GitHubURL returns the GitHub repository URL for this action reference.
func (r ActionRef) GitHubURL() string {
	return "https://github.com/" + r.Owner + "/" + r.Repo
}

// ActionYAMLPath returns the path to fetch action.yml from within the repository.
// For root actions this is "action.yml"; for subdirectory actions it is "path/action.yml".
func (r ActionRef) ActionYAMLPath(filename string) string {
	if r.Path == "" {
		return filename
	}
	return r.Path + "/" + filename
}

// ExtractActionRef parses a uses: directive into an ActionRef.
// Returns the zero value and false for local actions (./), docker references, or invalid formats.
func ExtractActionRef(uses string) (ActionRef, bool) {
	uses = strings.TrimSpace(uses)
	if uses == "" {
		return ActionRef{}, false
	}
	if strings.HasPrefix(uses, "./") || strings.HasPrefix(uses, "../") {
		return ActionRef{}, false
	}
	if strings.HasPrefix(uses, "docker://") {
		return ActionRef{}, false
	}

	ref := ""
	if idx := strings.Index(uses, "@"); idx > 0 {
		ref = uses[idx+1:]
		uses = uses[:idx]
	}

	parts := strings.SplitN(uses, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return ActionRef{}, false
	}

	ar := ActionRef{
		Owner: parts[0],
		Repo:  parts[1],
		Ref:   ref,
	}
	if len(parts) == 3 && parts[2] != "" {
		ar.Path = parts[2]
	}
	return ar, true
}

// actionFile is the minimal YAML structure for parsing action.yml files.
type actionFile struct {
	Runs actionRuns `yaml:"runs"`
}

type actionRuns struct {
	Using string `yaml:"using"`
	Steps []step `yaml:"steps"`
}

// ParseCompositeActionURLs parses an action.yml file and extracts GitHub URLs
// from steps[].uses if the action is a composite action (runs.using: composite).
//
// Returns:
//   - refs: ActionRef values for each uses: directive found in composite steps
//   - isComposite: true if runs.using == "composite"
//   - err: non-nil if YAML parsing fails
func ParseCompositeActionURLs(data []byte) (refs []ActionRef, isComposite bool, err error) {
	var af actionFile
	if err := yaml.Unmarshal(data, &af); err != nil {
		return nil, false, fmt.Errorf("failed to parse action.yml: %w", err)
	}

	if !strings.EqualFold(af.Runs.Using, "composite") {
		return nil, false, nil
	}

	seen := make(map[string]struct{})
	for _, s := range af.Runs.Steps {
		ref, ok := ExtractActionRef(s.Uses)
		if !ok {
			continue
		}
		key := ref.GitHubURL()
		if ref.Path != "" {
			key += "/" + ref.Path
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		refs = append(refs, ref)
	}

	return refs, true, nil
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
