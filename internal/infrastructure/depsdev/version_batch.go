package depsdev

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

// FetchTransitiveAdvisoryKeys extracts advisory keys for all non-SELF dependency nodes.
// It fetches each dependency's version info (via the PURL endpoint) in parallel and returns
// a map of "name@version" -> []AdvisoryKey.
//
// DDD Layer: Infrastructure (parallel external API calls)
func (c *DepsDevClient) FetchTransitiveAdvisoryKeys(ctx context.Context, deps *DependenciesResponse) (map[string][]AdvisoryKey, error) {
	if deps == nil || len(deps.Nodes) == 0 {
		return make(map[string][]AdvisoryKey), nil
	}

	// Collect non-SELF nodes and build PURLs for lookup.
	type depInfo struct {
		purl    string
		nodeKey string // "name@version"
	}
	var targets []depInfo
	seen := make(map[string]bool)
	for _, node := range deps.Nodes {
		if node.Relation == "SELF" {
			continue
		}
		vk := node.VersionKey
		if vk.Name == "" || vk.Version == "" || vk.System == "" {
			continue
		}
		nodeKey := fmt.Sprintf("%s@%s", vk.Name, vk.Version)
		if seen[nodeKey] {
			continue
		}
		seen[nodeKey] = true

		purlStr := buildPURLFromVersionKey(vk)
		if purlStr == "" {
			continue
		}
		targets = append(targets, depInfo{purl: purlStr, nodeKey: nodeKey})
	}

	if len(targets) == 0 {
		return make(map[string][]AdvisoryKey), nil
	}

	slog.Debug("fetching_transitive_advisory_keys", "dependency_count", len(targets))

	const maxWorkers = 10
	results := make(map[string][]AdvisoryKey, len(targets))
	var mu sync.Mutex

	jobs := make(chan depInfo)
	var wg sync.WaitGroup

	workerCount := maxWorkers
	if len(targets) < workerCount {
		workerCount = len(targets)
	}

	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				case di, ok := <-jobs:
					if !ok {
						return
					}

					resp, err := c.fetchPackageInfo(ctx, di.purl)
					if err != nil {
						slog.Debug("transitive advisory lookup failed", "purl", di.purl, "error", err)
						continue
					}
					if resp == nil || len(resp.Version.AdvisoryKeys) == 0 {
						continue
					}

					mu.Lock()
					results[di.nodeKey] = resp.Version.AdvisoryKeys
					mu.Unlock()
				}
			}
		}()
	}

dispatch:
	for _, t := range targets {
		select {
		case <-ctx.Done():
			break dispatch
		case jobs <- t:
		}
	}
	close(jobs)
	wg.Wait()

	slog.Debug("transitive_advisory_keys_complete",
		"dependencies_queried", len(targets),
		"with_advisories", len(results),
	)
	return results, nil
}

// buildPURLFromVersionKey converts a DependencyVersionKey to a PURL string.
// Returns empty string for unsupported systems.
func buildPURLFromVersionKey(vk DependencyVersionKey) string {
	system := strings.ToUpper(vk.System)
	switch system {
	case "NPM":
		return fmt.Sprintf("pkg:npm/%s@%s", vk.Name, vk.Version)
	case "CARGO":
		return fmt.Sprintf("pkg:cargo/%s@%s", vk.Name, vk.Version)
	case "PYPI":
		return fmt.Sprintf("pkg:pypi/%s@%s", vk.Name, vk.Version)
	case "MAVEN":
		// Maven names are in "groupId:artifactId" format in deps.dev; PURL uses "groupId/artifactId".
		parts := strings.SplitN(vk.Name, ":", 2)
		if len(parts) == 2 {
			return fmt.Sprintf("pkg:maven/%s/%s@%s", parts[0], parts[1], vk.Version)
		}
		return fmt.Sprintf("pkg:maven/%s@%s", vk.Name, vk.Version)
	default:
		return ""
	}
}
