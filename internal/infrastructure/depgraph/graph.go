// Package depgraph analyzes CycloneDX SBOM dependency graphs to compute
// exclusive transitive dependency counts for each direct dependency.
package depgraph

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	domaindiet "github.com/future-architect/uzomuzo-oss/internal/domain/diet"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/sbomgraph"
)

// Analyzer performs dependency graph analysis on CycloneDX SBOMs.
type Analyzer struct{}

// NewAnalyzer creates a new graph analyzer.
func NewAnalyzer() *Analyzer {
	return &Analyzer{}
}

// AnalyzeGraph parses the SBOM and computes graph metrics for each direct dependency.
func (a *Analyzer) AnalyzeGraph(_ context.Context, sbomData []byte) (*domaindiet.GraphResult, error) {
	var bom sbomgraph.BOMEnvelope
	if err := json.Unmarshal(sbomData, &bom); err != nil {
		return nil, fmt.Errorf("failed to parse CycloneDX JSON: %w", err)
	}

	refMap := sbomgraph.BuildRefMap(bom.Components)
	directPURLs := sbomgraph.ResolveDirectPURLs(&bom, refMap)
	if directPURLs == nil {
		return nil, fmt.Errorf("SBOM has no dependency graph (missing metadata.component or dependencies section)")
	}

	// Build adjacency list: normalizedPURL → []normalizedPURL
	adj := sbomgraph.BuildAdjacencyList(bom.Dependencies, refMap)

	// Collect all unique normalized PURLs
	allPURLs := make(map[string]struct{})
	for _, normalized := range refMap {
		allPURLs[normalized] = struct{}{}
	}

	// Compute reachable sets per direct dependency
	directList := make([]string, 0, len(directPURLs))
	reachable := make(map[string]map[string]struct{})
	for dp := range directPURLs {
		directList = append(directList, dp)
		reachable[dp] = bfs(dp, adj)
	}
	sort.Strings(directList)

	// Count how many direct deps can reach each transitive dep
	providerCount := make(map[string]int)
	for _, dp := range directList {
		for tp := range reachable[dp] {
			if _, isDirect := directPURLs[tp]; !isDirect {
				providerCount[tp]++
			}
		}
	}

	// Compute metrics per direct dep
	metrics := make(map[string]*domaindiet.GraphMetrics, len(directList))
	for _, dp := range directList {
		m := &domaindiet.GraphMetrics{}
		for tp := range reachable[dp] {
			if _, isDirect := directPURLs[tp]; isDirect {
				continue
			}
			m.TotalTransitiveCount++
			if providerCount[tp] == 1 {
				m.ExclusiveTransitiveCount++
			} else {
				m.SharedTransitiveCount++
			}
		}
		metrics[dp] = m
	}

	// Collect all dependency PURLs
	allDepList := make([]string, 0, len(allPURLs))
	for p := range allPURLs {
		allDepList = append(allDepList, p)
	}
	sort.Strings(allDepList)

	// Total transitive = all deps minus direct deps minus root component
	rootPURL := ""
	if bom.Metadata != nil && bom.Metadata.Component != nil && bom.Metadata.Component.PURL != "" {
		rootPURL = sbomgraph.NormalizePURL(bom.Metadata.Component.PURL)
	}
	totalTransitive := 0
	for p := range allPURLs {
		if p == rootPURL {
			continue
		}
		if _, isDirect := directPURLs[p]; !isDirect {
			totalTransitive++
		}
	}

	return &domaindiet.GraphResult{
		DirectDeps:      directList,
		AllDeps:         allDepList,
		Metrics:         metrics,
		TotalTransitive: totalTransitive,
	}, nil
}

// bfs returns all PURLs reachable from start (excluding start itself).
func bfs(start string, adj map[string][]string) map[string]struct{} {
	visited := make(map[string]struct{})
	queue := []string{start}
	visited[start] = struct{}{}
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		for _, next := range adj[curr] {
			if _, seen := visited[next]; !seen {
				visited[next] = struct{}{}
				queue = append(queue, next)
			}
		}
	}
	delete(visited, start) // exclude start itself
	return visited
}
