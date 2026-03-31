package github

// GraphQLRequest represents the structure for GraphQL requests
type GraphQLRequest struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables"`
}

// GraphQLResponse represents the structure for GraphQL responses
type GraphQLResponse struct {
	Data   ResponseData   `json:"data"`
	Errors []GraphQLError `json:"errors"`
}

// ResponseData represents the data part of GraphQL responses
type ResponseData struct {
	Repository RepositoryInfo `json:"repository"`
	RateLimit  RateLimit      `json:"rateLimit"`
}

// RepositoryInfo represents the structure for repository information
type RepositoryInfo struct {
	IsArchived               bool                     `json:"isArchived"`
	IsDisabled               bool                     `json:"isDisabled"`
	IsFork                   bool                     `json:"isFork"`
	StargazerCount           int                      `json:"stargazerCount"`
	ForkCount                int                      `json:"forkCount"`
	Description              string                   `json:"description"`
	HomepageURL              string                   `json:"homepageUrl"`
	DefaultBranchRef         DefaultBranchRef         `json:"defaultBranchRef"`
	DependencyGraphManifests DependencyGraphManifests `json:"dependencyGraphManifests"`
	LicenseInfo              *LicenseInfo             `json:"licenseInfo"`
	// Source is the ultimate non-fork root repository (GitHub "source" field).
	// Nil when the repository is not a fork, or when the GraphQL "source" field is
	// null/unavailable (for example, if the parent/source repository is private or inaccessible).
	Source *SourceInfo `json:"source,omitempty"`
}

// SourceInfo represents the ultimate source (non-fork root) repository.
type SourceInfo struct {
	NameWithOwner string `json:"nameWithOwner"`
}

// LicenseInfo represents GitHub repository license information
type LicenseInfo struct {
	SpdxID string `json:"spdxId"`
	Name   string `json:"name"`
}

// DefaultBranchRef represents information about the default branch
type DefaultBranchRef struct {
	Name   string `json:"name"`
	Target Target `json:"target"`
}

// Target represents target (commit) information for a branch
type Target struct {
	History CommitHistory `json:"history"`
}

// CommitHistory represents commit history information
type CommitHistory struct {
	Nodes []CommitNode `json:"nodes"`
}

// CommitNode represents individual commit information
type CommitNode struct {
	CommittedDate string       `json:"committedDate"`
	Author        CommitAuthor `json:"author"`
}

// CommitAuthor represents commit author information
type CommitAuthor struct {
	User *User `json:"user"`
}

// User represents GitHub user information
type User struct {
	Login string `json:"login"`
}

// GraphQLError represents the structure for GraphQL errors
type GraphQLError struct {
	Message string `json:"message"`
}

// RateLimit represents GitHub API rate limit information
type RateLimit struct {
	Cost      int    `json:"cost"`
	Remaining int    `json:"remaining"`
	ResetAt   string `json:"resetAt"`
}

// DependencyGraphManifests represents dependency graph manifests information
type DependencyGraphManifests struct {
	Nodes []ManifestNode `json:"nodes"`
}

// ManifestNode represents individual manifest file information
type ManifestNode struct {
	Filename     string       `json:"filename"`
	Dependencies Dependencies `json:"dependencies"`
}

// Dependencies represents dependencies information
type Dependencies struct {
	Nodes []DependencyNode `json:"nodes"`
}

// DependencyNode represents individual dependency information
type DependencyNode struct {
	PackageManager string `json:"packageManager"`
	PackageName    string `json:"packageName"`
}
