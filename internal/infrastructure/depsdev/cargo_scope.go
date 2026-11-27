package depsdev

import "strings"

// cargoName normalizes a crate name so requirement names (unicode_ident) match
// graph node names (unicode-ident).
func cargoName(name string) string {
	return strings.ReplaceAll(strings.ToLower(name), "_", "-")
}

// isCargoDevDependency reports whether dep is declared under [dev-dependencies].
func isCargoDevDependency(dep CargoRequirementDependency) bool {
	return strings.EqualFold(strings.TrimSpace(dep.Kind), "dev")
}

// enabledCargoDependencies returns the optional dependencies that the crate's
// default feature turns on, following feature implications transitively.
func enabledCargoDependencies(cargo *CargoRequirementsData) map[string]bool {
	optional := make(map[string]bool)
	for _, dep := range cargo.Dependencies {
		if isCargoDevDependency(dep) || !dep.Optional {
			continue
		}
		localName := dep.Name
		if dep.PackageAlias != "" {
			localName = dep.PackageAlias
		}
		optional[localName] = true
	}
	features := make(map[string][]string)
	explicitDependencies := make(map[string]bool)
	for _, feature := range cargo.Features {
		features[feature.Name] = feature.Implies
		for _, token := range feature.Implies {
			if strings.HasPrefix(token, "dep:") {
				explicitDependencies[strings.TrimPrefix(token, "dep:")] = true
			}
		}
	}
	for name := range optional {
		if _, exists := features[name]; !exists && !explicitDependencies[name] {
			features[name] = []string{"dep:" + name}
		}
	}
	enabled := make(map[string]bool)
	visited := make(map[string]bool)
	var visit func(string)
	visit = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true
		for _, token := range features[name] {
			switch {
			case strings.HasPrefix(token, "dep:"):
				key := strings.TrimPrefix(token, "dep:")
				if optional[key] {
					enabled[key] = true
				}
			case strings.Contains(token, "?/"):
				continue
			case strings.Contains(token, "/"):
				key := strings.SplitN(token, "/", 2)[0]
				if optional[key] {
					enabled[key] = true
				}
			default:
				if _, exists := features[token]; exists {
					visit(token)
				}
			}
		}
	}
	if _, ok := features["default"]; ok {
		visit("default")
	}
	return enabled
}

// restrictCargoGraph keeps only the part of graph that a consumer building the
// crate with default features pulls in. It returns graph unchanged when the
// requirements cannot be matched to it, so an uncertain reduction never hides
// advisories. See ADR-0038.
func restrictCargoGraph(requirements *CargoRequirements, graph *DependenciesResponse) *DependenciesResponse {
	if requirements == nil || requirements.Cargo == nil || graph == nil || len(requirements.Cargo.Dependencies) == 0 {
		return graph
	}
	self := -1
	for i, node := range graph.Nodes {
		if node.Relation == "SELF" {
			self = i
			break
		}
	}
	if self < 0 {
		return graph
	}

	enabled := enabledCargoDependencies(requirements.Cargo)
	kept := make(map[string]map[string]bool)
	declared := make(map[string]bool)
	var keptDeclarations []CargoRequirementDependency
	for _, dep := range requirements.Cargo.Dependencies {
		declared[cargoName(dep.Name)] = true
		if dep.PackageAlias != "" {
			declared[cargoName(dep.PackageAlias)] = true
		}
		if isCargoDevDependency(dep) {
			continue
		}
		localName := dep.Name
		if dep.PackageAlias != "" {
			localName = dep.PackageAlias
		}
		if dep.Optional && !enabled[localName] {
			continue
		}
		keptDeclarations = append(keptDeclarations, dep)
		requirement := strings.TrimSpace(dep.Requirement)
		name := cargoName(dep.Name)
		if kept[name] == nil {
			kept[name] = make(map[string]bool)
		}
		kept[name][requirement] = true
		if dep.PackageAlias != "" {
			name = cargoName(dep.PackageAlias)
			if kept[name] == nil {
				kept[name] = make(map[string]bool)
			}
			kept[name][requirement] = true
		}
	}
	adjacent := make([][]int, len(graph.Nodes))
	matchedRequirements := make(map[string]map[string]bool)
	directNames := make(map[string]bool)
	for _, edge := range graph.Edges {
		if edge.FromNode < 0 || edge.FromNode >= len(graph.Nodes) || edge.ToNode < 0 || edge.ToNode >= len(graph.Nodes) {
			continue
		}
		adjacent[edge.FromNode] = append(adjacent[edge.FromNode], edge.ToNode)
		if edge.FromNode == self {
			name := cargoName(graph.Nodes[edge.ToNode].VersionKey.Name)
			if !declared[name] {
				return graph
			}
			directNames[name] = true
			requirement := strings.TrimSpace(edge.Requirement)
			if kept[name][requirement] {
				if matchedRequirements[name] == nil {
					matchedRequirements[name] = make(map[string]bool)
				}
				matchedRequirements[name][requirement] = true
			}
		}
	}
	for _, dep := range keptDeclarations {
		if !directNames[cargoName(dep.Name)] && (dep.PackageAlias == "" || !directNames[cargoName(dep.PackageAlias)]) {
			return graph
		}
	}
	if len(kept) == 0 {
		return &DependenciesResponse{Nodes: []DependencyNode{graph.Nodes[self]}}
	}
	reached := make([]bool, len(graph.Nodes))
	direct := make([]bool, len(graph.Nodes))
	accepted := make([]bool, len(graph.Edges))
	reached[self] = true
	queue := make([]int, 0, len(graph.Nodes))
	for edgeIndex, edge := range graph.Edges {
		if edge.FromNode != self || edge.ToNode < 0 || edge.ToNode >= len(graph.Nodes) {
			continue
		}
		target := edge.ToNode
		name := cargoName(graph.Nodes[target].VersionKey.Name)
		if kept[name] == nil || len(matchedRequirements[name]) == len(kept[name]) && !kept[name][strings.TrimSpace(edge.Requirement)] {
			continue
		}
		accepted[edgeIndex] = true
		direct[target] = true
		if !reached[target] {
			reached[target] = true
			queue = append(queue, target)
		}
	}
	if len(queue) == 0 {
		return graph
	}
	for head := 0; head < len(queue); head++ {
		for _, target := range adjacent[queue[head]] {
			if !reached[target] {
				reached[target] = true
				queue = append(queue, target)
			}
		}
	}

	result := &DependenciesResponse{Error: graph.Error}
	index := make([]int, len(graph.Nodes))
	for i, node := range graph.Nodes {
		if !reached[i] {
			continue
		}
		index[i] = len(result.Nodes)
		if i != self {
			if direct[i] {
				node.Relation = "DIRECT"
			} else {
				node.Relation = "INDIRECT"
			}
		}
		result.Nodes = append(result.Nodes, node)
	}
	for edgeIndex, edge := range graph.Edges {
		if edge.FromNode < 0 || edge.FromNode >= len(reached) || edge.ToNode < 0 || edge.ToNode >= len(reached) || !reached[edge.FromNode] || !reached[edge.ToNode] {
			continue
		}
		if edge.FromNode == self && !accepted[edgeIndex] {
			continue
		}
		edge.FromNode = index[edge.FromNode]
		edge.ToNode = index[edge.ToNode]
		result.Edges = append(result.Edges, edge)
	}
	return result
}
