package ghaworkflow

import (
	"fmt"
	"sort"

	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
	"gopkg.in/yaml.v3"
)

// ParseGitHubURLsWithLocations extracts action URLs and every corresponding uses line.
func ParseGitHubURLsWithLocations(data []byte) ([]string, map[string][]depparser.SourceLocation, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, nil, fmt.Errorf("failed to parse GitHub Actions workflow YAML: %w", err)
	}
	var wf workflowFile
	if err := root.Decode(&wf); err != nil {
		return nil, nil, fmt.Errorf("failed to decode GitHub Actions workflow YAML: %w", err)
	}
	var positions struct {
		Jobs map[string]struct {
			Uses  yaml.Node `yaml:"uses"`
			Steps []struct {
				Uses yaml.Node `yaml:"uses"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := root.Decode(&positions); err != nil {
		return nil, nil, fmt.Errorf("failed to decode workflow locations: %w", err)
	}
	locs := make(map[string][]depparser.SourceLocation)
	for name, j := range wf.Jobs {
		pos := positions.Jobs[name]
		addWorkflowLocation(locs, j.Uses, pos.Uses.Line)
		for i, s := range j.Steps {
			addWorkflowLocation(locs, s.Uses, pos.Steps[i].Uses.Line)
		}
	}
	for u := range locs {
		sort.Slice(locs[u], func(i, j int) bool { return locs[u][i].Line < locs[u][j].Line })
	}
	return workflowURLs(wf), locs, nil
}

func addWorkflowLocation(locs map[string][]depparser.SourceLocation, uses string, line int) {
	u := extractGitHubURL(uses)
	if u == "" || line < 1 {
		return
	}
	for _, loc := range locs[u] {
		if loc.Line == line {
			return
		}
	}
	locs[u] = append(locs[u], depparser.SourceLocation{Line: line})
}
