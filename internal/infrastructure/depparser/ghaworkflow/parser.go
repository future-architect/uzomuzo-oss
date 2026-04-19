// Package ghaworkflow extracts GitHub repository URLs from GitHub Actions workflow YAML files.
//
// DDD Layer: Infrastructure (external format parsing)
package ghaworkflow

import (
	"fmt"
	"path"
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

// ParseWorkflowAll reads a GitHub Actions workflow YAML file and returns both
// the unique GitHub repository URLs and step-level local action paths from a
// single unmarshal. Local action paths are extracted only from step `uses:`
// values (matching ParseLocalActionPaths); job-level `uses:` values are not
// included in localPaths.
// This avoids double-parsing for callers that need both results.
// Jobs are iterated in sorted key order for deterministic output.
func ParseWorkflowAll(data []byte) (urls []string, localPaths []string, err error) {
	refs, localPaths, err := ParseWorkflowAllWithRefs(data)
	if err != nil {
		return nil, nil, err
	}
	seen := make(map[string]struct{}, len(refs))
	urls = make([]string, 0, len(refs))
	for _, r := range refs {
		u := r.GitHubURL()
		if _, exists := seen[u]; exists {
			continue
		}
		seen[u] = struct{}{}
		urls = append(urls, u)
	}
	return urls, localPaths, nil
}

// ParseWorkflowAllWithRefs reads a GitHub Actions workflow YAML file and returns
// both the parsed ActionRef values (preserving owner/repo/path/ref) and the
// step-level local action paths.
//
// Unlike ParseWorkflowAll, this function does NOT deduplicate by GitHub URL:
// the same owner/repo pinned to different versions (e.g., actions/checkout@v2
// and actions/checkout@v4 in different jobs) yields multiple ActionRef entries,
// one per distinct (URL, Ref) pair. Callers that need URL-level deduplication
// should aggregate the returned refs themselves.
//
// Local action paths are extracted only from step `uses:` values (matching
// ParseLocalActionPaths); job-level `uses:` values are not included in localPaths.
// Jobs are iterated in sorted key order for deterministic output.
func ParseWorkflowAllWithRefs(data []byte) (refs []ActionRef, localPaths []string, err error) {
	var wf workflowFile
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil, nil, fmt.Errorf("failed to parse GitHub Actions workflow YAML: %w", err)
	}

	jobNames := make([]string, 0, len(wf.Jobs))
	for name := range wf.Jobs {
		jobNames = append(jobNames, name)
	}
	sort.Strings(jobNames)

	refSeen := make(map[string]struct{})
	localSeen := make(map[string]struct{})

	addRef := func(uses string) {
		ref, ok := ExtractActionRef(uses)
		if !ok {
			return
		}
		key := ref.GitHubURL()
		if ref.Path != "" {
			key += "/" + ref.Path
		}
		key += "@" + ref.Ref
		if _, exists := refSeen[key]; exists {
			return
		}
		refSeen[key] = struct{}{}
		refs = append(refs, ref)
	}

	for _, name := range jobNames {
		j := wf.Jobs[name]
		// Reusable workflow reference at job level (GitHub URLs only).
		addRef(j.Uses)
		for _, s := range j.Steps {
			addRef(s.Uses)
			if p := ExtractLocalActionPath(s.Uses); p != "" {
				if _, exists := localSeen[p]; !exists {
					localSeen[p] = struct{}{}
					localPaths = append(localPaths, p)
				}
			}
		}
	}

	return refs, localPaths, nil
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

// ParseCompositeActionURLs parses an action.yml file and extracts ActionRef values
// from steps[].uses if the action is a composite action (runs.using: composite).
//
// Returns:
//   - refs: parsed ActionRef values (owner/repo/path/ref) for each uses: directive found in composite steps
//   - isComposite: true if runs.using == "composite"
//   - err: non-nil if YAML parsing fails
func ParseCompositeActionURLs(data []byte) (refs []ActionRef, isComposite bool, err error) {
	var af actionFile
	if err := yaml.Unmarshal(data, &af); err != nil {
		return nil, false, fmt.Errorf("failed to parse action manifest: %w", err)
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

// ExtractLocalActionPath returns the cleaned local path from a uses: directive
// that starts with "./" (e.g., "./.github/actions/foo" → ".github/actions/foo").
// Returns "" for non-local references, docker://, empty strings, or paths that
// attempt directory traversal (e.g., "./../secret" or "./foo/../../../etc").
func ExtractLocalActionPath(uses string) string {
	uses = strings.TrimSpace(uses)
	if uses == "" {
		return ""
	}
	if !strings.HasPrefix(uses, "./") {
		return ""
	}
	// Reject backslashes (non-POSIX path separators).
	if strings.ContainsRune(uses, '\\') {
		return ""
	}
	// Strip "./" prefix and any trailing slashes.
	p := strings.TrimPrefix(uses, "./")
	p = strings.TrimRight(p, "/")
	if p == "" {
		return ""
	}
	// Normalize and reject path traversal attempts and absolute paths.
	cleaned := path.Clean(p)
	if cleaned == "." || strings.HasPrefix(cleaned, "/") || strings.HasPrefix(cleaned, "..") {
		return ""
	}
	return cleaned
}

// ParseLocalActionPaths reads a GitHub Actions workflow YAML file and returns
// the unique local action paths referenced by step-level uses: directives
// (those starting with "./").
// Paths are cleaned (leading "./" and trailing "/" removed).
// Jobs are iterated in sorted key order for deterministic output.
func ParseLocalActionPaths(data []byte) ([]string, error) {
	var wf workflowFile
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return nil, fmt.Errorf("failed to parse GitHub Actions workflow YAML: %w", err)
	}

	jobNames := make([]string, 0, len(wf.Jobs))
	for name := range wf.Jobs {
		jobNames = append(jobNames, name)
	}
	sort.Strings(jobNames)

	seen := make(map[string]struct{})
	var paths []string

	for _, name := range jobNames {
		j := wf.Jobs[name]
		for _, s := range j.Steps {
			if p := ExtractLocalActionPath(s.Uses); p != "" {
				if _, exists := seen[p]; !exists {
					seen[p] = struct{}{}
					paths = append(paths, p)
				}
			}
		}
	}

	return paths, nil
}

// ParseCompositeAll parses an action.yml and returns both external ActionRef values
// and nested local action paths from a single YAML unmarshal. This avoids double-parsing
// for callers that need both results (e.g., BFS over local composite actions).
// Returns isComposite=false if runs.using != "composite".
func ParseCompositeAll(data []byte) (refs []ActionRef, localPaths []string, isComposite bool, err error) {
	var af actionFile
	if err := yaml.Unmarshal(data, &af); err != nil {
		return nil, nil, false, fmt.Errorf("failed to parse action manifest: %w", err)
	}

	if !strings.EqualFold(af.Runs.Using, "composite") {
		return nil, nil, false, nil
	}

	refSeen := make(map[string]struct{})
	localSeen := make(map[string]struct{})

	for _, s := range af.Runs.Steps {
		if ref, ok := ExtractActionRef(s.Uses); ok {
			key := ref.GitHubURL()
			if ref.Path != "" {
				key += "/" + ref.Path
			}
			if _, exists := refSeen[key]; !exists {
				refSeen[key] = struct{}{}
				refs = append(refs, ref)
			}
		}
		if p := ExtractLocalActionPath(s.Uses); p != "" {
			if _, exists := localSeen[p]; !exists {
				localSeen[p] = struct{}{}
				localPaths = append(localPaths, p)
			}
		}
	}

	return refs, localPaths, true, nil
}

// ParseCompositeLocalActionPaths parses an action.yml and extracts local action paths
// from composite steps (uses: ./ references). Returns nil if not a composite action.
func ParseCompositeLocalActionPaths(data []byte) (paths []string, isComposite bool, err error) {
	var af actionFile
	if err := yaml.Unmarshal(data, &af); err != nil {
		return nil, false, fmt.Errorf("failed to parse action manifest: %w", err)
	}

	if !strings.EqualFold(af.Runs.Using, "composite") {
		return nil, false, nil
	}

	seen := make(map[string]struct{})
	for _, s := range af.Runs.Steps {
		if p := ExtractLocalActionPath(s.Uses); p != "" {
			if _, exists := seen[p]; !exists {
				seen[p] = struct{}{}
				paths = append(paths, p)
			}
		}
	}

	return paths, true, nil
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
