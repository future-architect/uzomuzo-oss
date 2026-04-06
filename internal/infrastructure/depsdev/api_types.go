// Package depsdev contains API response types for deps.dev API
package depsdev

import (
	"encoding/json"
	"time"
)

// ============================================================================
// Project Batch API related types and functions
// https://docs.deps.dev/api/v3alpha/#getprojectbatch
// ============================================================================

// ProjectBatchRequest represents the entire request for the Project Batch API.
type ProjectBatchRequest struct {
	Requests  []ProjectRequest `json:"requests"`
	PageToken string           `json:"pageToken,omitempty"`
}

// ProjectBatchResponse is the entire response from the Project Batch API.
type ProjectBatchResponse struct {
	Responses     []ProjectResponse `json:"responses"`
	NextPageToken string            `json:"nextPageToken"`
}

// ProjectResponse is each response item.
type ProjectResponse struct {
	Request ProjectRequest `json:"request"`
	Project *Project       `json:"project,omitempty"`
	// If there are other optional fields such as ossFuzz, they can be added as needed.
}

// ProjectRequest is a request item to the API that indicates a project key.
type ProjectRequest struct {
	ProjectKey ProjectKey `json:"projectKey"`
}

// ProjectKey is the key for a project (such as a GitHub repository URL).
type ProjectKey struct {
	ID string `json:"id"`
}

// Project contains detailed information about a project.
type Project struct {
	ProjectKey      ProjectKey    `json:"projectKey"`
	OpenIssuesCount int           `json:"openIssuesCount"`
	StarsCount      int           `json:"starsCount"`
	ForksCount      int           `json:"forksCount"`
	License         string        `json:"license"`
	Description     string        `json:"description"`
	Homepage        string        `json:"homepage"`
	Scorecard       ScorecardData `json:"scorecard"`
	OssFuzz         OssFuzzData   `json:"ossFuzz"`
}

// ScorecardData represents the analysis results of OpenSSF Scorecard.
type ScorecardData struct {
	Date         time.Time           `json:"date"`
	Repository   ScorecardRepo       `json:"repository"`
	Scorecard    ScorecardScoreSet   `json:"scorecard"`
	OverallScore float64             `json:"overallScore"`
	Checks       []ScorecardCheckSet `json:"checks"`
}

// ScorecardRepo contains repository information analyzed by Scorecard.
type ScorecardRepo struct {
	Name   string `json:"name"`
	Commit string `json:"commit"`
}

// ScorecardScoreSet holds each check item and its score for Scorecard.
type ScorecardScoreSet struct {
	Version string              `json:"version"`
	Commit  string              `json:"commit"`
	Checks  []ScorecardCheckSet `json:"checks"`
}

// ScorecardCheckSet contains details of each Scorecard check item.
type ScorecardCheckSet struct {
	Name          string `json:"name"`
	Documentation struct {
		Short string `json:"short"`
		URL   string `json:"url"`
	} `json:"documentation"`
	Score   int      `json:"score"`
	Reason  string   `json:"reason"`
	Details []string `json:"details"`
}

// OssFuzzData represents information about the OSS-Fuzz project.
type OssFuzzData struct {
	// Add fields related to OSS-Fuzz here
}

// ============================================================================
// PURL Batch API related types and functions
// https://docs.deps.dev/api/v3alpha/#purllookupbatch
// ============================================================================

// RequestPayload is each request item sent to the PURL batch API.
type RequestPayload struct {
	Purl string `json:"purl"`
}

// BatchRequest is the entire request containing multiple RequestPayloads and a page token (if any).
type BatchRequest struct {
	Requests  []RequestPayload `json:"requests"`
	PageToken string           `json:"pageToken,omitempty"`
}

// BatchResponse is the entire response from the PURL batch API.
type BatchResponse struct {
	Responses     []json.RawMessage `json:"responses"`
	NextPageToken string            `json:"nextPageToken"`
}

// The following structs represent the structure within the PURL batch API response

type LicenseDetail struct {
	License string `json:"license"`
	Spdx    string `json:"spdx"`
}

type Link struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type RelatedProject struct {
	ProjectKey struct {
		ID string `json:"id"`
	} `json:"projectKey"`
	RelationType string `json:"relationType"`
}

type UpstreamIdentifier struct {
	PackageName   string `json:"packageName"`
	VersionString string `json:"versionString"`
}

// ============================================================================
// PURL Lookup API response types
// ============================================================================

// Root
type PackageResponse struct {
	Version Version `json:"version"`
}

// package field
type Package struct {
	PackageKey PackageKey `json:"packageKey"`
	PURL       string     `json:"purl"`
	Versions   []Version  `json:"versions"`
}

// package.packageKey
type PackageKey struct {
	System string `json:"system"`
	Name   string `json:"name"`
}

// package.versions[i]
type Version struct {
	VersionKey          VersionKey           `json:"versionKey"`
	PURL                string               `json:"purl"`
	PublishedAt         time.Time            `json:"publishedAt"`
	IsDefault           bool                 `json:"isDefault"`
	IsDeprecated        bool                 `json:"isDeprecated"`
	Licenses            []string             `json:"licenses"`
	LicenseDetails      []LicenseDetail      `json:"licenseDetails"`
	AdvisoryKeys        []AdvisoryKey        `json:"advisoryKeys"`
	Links               []Link               `json:"links"`
	Registries          []string             `json:"registries"`
	RelatedProjects     []RelatedProject     `json:"relatedProjects"`
	UpstreamIdentifiers []UpstreamIdentifier `json:"upstreamIdentifiers"`
	SlsaProvenances     []SLSAProvenance     `json:"slsaProvenances"`
	Attestations        []Attestation        `json:"attestations"`
}

// package.versions[i].versionKey
type VersionKey struct {
	System  string `json:"system"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ============================================================================
// Release Info API response types
// ============================================================================

type ReleaseInfo struct {
	StableVersion     Version
	PreReleaseVersion Version
	MaxSemverVersion  Version
	RequestedVersion  Version

	Endpoint string
	Error    error
}

// BatchResult represents the result of a batch operation
type BatchResult struct {
	PURL        string      `json:"purl"`
	Package     *Package    `json:"package,omitempty"`
	Project     *Project    `json:"project,omitempty"`
	RepoURL     string      `json:"repo_url,omitempty"`
	Error       *string     `json:"error,omitempty"`
	ReleaseInfo ReleaseInfo `json:"release_info,omitempty"`
}

// AdvisoryKey represents each object within advisoryKeys.
type AdvisoryKey struct {
	ID string `json:"id"`
}

// AdvisoryDetail represents the response from GET /v3alpha/advisories/{id}.
// It contains severity metadata not available in the per-version advisoryKeys.
type AdvisoryDetail struct {
	AdvisoryKey AdvisoryKey `json:"advisoryKey"`
	URL         string      `json:"url"`
	Title       string      `json:"title"`
	Aliases     []string    `json:"aliases"`
	CVSS3Score  float64     `json:"cvss3Score"`
	CVSS3Vector string      `json:"cvss3Vector"`
}

// SLSAProvenance represents SLSA provenance information
type SLSAProvenance struct {
	SourceRepository string `json:"sourceRepository"`
	Verified         bool   `json:"verified"`
	Commit           string `json:"commit"`
}

// Attestation represents attestation information
type Attestation struct {
	Verified         bool   `json:"verified,omitempty"`
	SourceRepository string `json:"sourceRepository,omitempty"`
	Commit           string `json:"commit,omitempty"`
}

// ============================================================================
// GetDependencies API response types
// https://docs.deps.dev/api/v3alpha/#getdependencies
// ============================================================================

// DependenciesResponse represents the response from the GetDependencies API.
// Endpoint: GET /v3alpha/systems/{system}/packages/{name}/versions/{version}:dependencies
// Supported systems: npm, cargo, maven, pypi.
type DependenciesResponse struct {
	Nodes []DependencyNode `json:"nodes"`
	Edges []DependencyEdge `json:"edges"`
}

// DependencyNode represents a single node in the dependency graph.
type DependencyNode struct {
	VersionKey DependencyVersionKey `json:"versionKey"`
	Relation   string               `json:"relation"` // SELF, DIRECT, INDIRECT
	Errors     []string             `json:"errors"`
}

// DependencyVersionKey identifies the system/name/version of a dependency node.
type DependencyVersionKey struct {
	System  string `json:"system"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

// DependencyEdge represents an edge in the dependency graph.
type DependencyEdge struct {
	FromNode    int    `json:"fromNode"`
	ToNode      int    `json:"toNode"`
	Requirement string `json:"requirement"`
}

// CountByRelation counts nodes by relation type (DIRECT, INDIRECT), excluding SELF.
func (r *DependenciesResponse) CountByRelation() (direct, transitive int) {
	for _, n := range r.Nodes {
		switch n.Relation {
		case "DIRECT":
			direct++
		case "INDIRECT":
			transitive++
		}
	}
	return
}

// ============================================================================
// GetDependents API response types
// https://docs.deps.dev/api/v3alpha/#getdependents
// ============================================================================

// DependentsResponse represents the response from the GetDependents API.
// Endpoint: GET /v3alpha/systems/{system}/packages/{name}:dependents
// Supported systems: npm, maven, pypi, cargo, go.
type DependentsResponse struct {
	DependentCount int `json:"dependentCount"`
	// DirectDependentCount and IndirectDependentCount are captured from API but only
	// the total (DependentCount) is used for cross-ecosystem consistency
	// (RubyGems/Packagist lack this breakdown).
	DirectDependentCount   int `json:"directDependentCount"`
	IndirectDependentCount int `json:"indirectDependentCount"`
}

// ============================================================================
