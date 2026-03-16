package depsdev

import (
	"context"
	"log/slog"
	"net/url"
	"strings"

	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/maven"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/npmjs"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/nuget"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/packagist"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/pypi"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/rubygems"
)

// RepoResolver defines a strategy to resolve a repository URL from a deps.dev package response.
// Return an empty string when not applicable or not resolvable; reserve errors for hard failures.
type RepoResolver interface {
	Name() string
	Resolve(ctx context.Context, pkg *PackageResponse) (string, error)
}

// linksResolver resolves from Version.Links using ExtractRepositoryURLFromLinks.
type linksResolver struct{}

func (r *linksResolver) Name() string { return "links" }
func (r *linksResolver) Resolve(ctx context.Context, pkg *PackageResponse) (string, error) {
	if pkg == nil {
		return "", nil
	}
	return ExtractRepositoryURLFromLinks(pkg.Version.Links), nil
}

// relatedProjectsResolver resolves from RelatedProjects using repoURLFromRelatedProjects.
type relatedProjectsResolver struct{}

func (r *relatedProjectsResolver) Name() string { return "related_projects" }
func (r *relatedProjectsResolver) Resolve(ctx context.Context, pkg *PackageResponse) (string, error) {
	if pkg == nil {
		return "", nil
	}
	return repoURLFromRelatedProjects(pkg.Version.RelatedProjects), nil
}

// rubyGemsResolver resolves from RubyGems metadata for gem/rubygems ecosystems.
type rubyGemsResolver struct{ client *rubygems.Client }

func (r *rubyGemsResolver) Name() string { return "rubygems" }
func (r *rubyGemsResolver) Resolve(ctx context.Context, pkg *PackageResponse) (string, error) {
	if pkg == nil || r.client == nil {
		return "", nil
	}
	eco := strings.ToLower(strings.TrimSpace(pkg.Version.VersionKey.System))
	if !isGemSystem(eco) {
		return "", nil
	}
	name := pkg.Version.VersionKey.Name
	ver := pkg.Version.VersionKey.Version
	return r.client.GetRepoURL(ctx, name, ver)
}

// packagistResolver resolves from Packagist metadata for composer/packagist ecosystems.
type packagistResolver struct{ client *packagist.Client }

func (r *packagistResolver) Name() string { return "packagist" }
func (r *packagistResolver) Resolve(ctx context.Context, pkg *PackageResponse) (string, error) {
	if pkg == nil || r.client == nil {
		return "", nil
	}
	eco := strings.ToLower(strings.TrimSpace(pkg.Version.VersionKey.System))
	if eco != "composer" && eco != "packagist" {
		return "", nil
	}
	// Parse PURL to extract vendor/namespace and name
	parser := purl.NewParser()
	parsed, err := parser.Parse(pkg.Version.PURL)
	if err != nil {
		return "", nil
	}
	vendor := strings.TrimSpace(parsed.Namespace())
	name := strings.TrimSpace(parsed.Name())
	if vendor == "" || name == "" {
		return "", nil
	}
	ver := strings.TrimSpace(parsed.Version())
	return r.client.GetRepoURL(ctx, vendor, name, ver)
}

// npmjsResolver resolves from npm registry metadata for npm ecosystem.
type npmjsResolver struct{ client *npmjs.Client }

func (r *npmjsResolver) Name() string { return "npmjs" }
func (r *npmjsResolver) Resolve(ctx context.Context, pkg *PackageResponse) (string, error) {
	if pkg == nil || r.client == nil {
		return "", nil
	}
	eco := strings.ToLower(strings.TrimSpace(pkg.Version.VersionKey.System))
	if eco != "npm" {
		return "", nil
	}
	// Decode PURL to handle encoded scopes like %40types
	raw := strings.TrimSpace(pkg.Version.PURL)
	if u, err := url.PathUnescape(raw); err == nil && u != "" {
		raw = u
	}

	parser := purl.NewParser()
	parsed, err := parser.Parse(raw)
	if err == nil && parsed != nil {
		ns := strings.TrimSpace(parsed.Namespace())
		name := strings.TrimSpace(parsed.Name())
		ver := strings.TrimSpace(parsed.Version())
		slog.Debug("npmjsResolver lookup", "namespace", ns, "name", name, "version", ver)
		return r.client.GetRepoURL(ctx, ns, name, ver)
	}

	// Fallback manual parse for npm PURLs: pkg:npm/[scope/]<name>@<version>
	ns, name, ver := "", "", ""
	body := strings.TrimPrefix(strings.TrimPrefix(raw, "pkg:"), "npm/")
	if body != raw { // only if it looked like an npm purl
		if at := strings.LastIndex(body, "@"); at >= 0 {
			ver = strings.TrimSpace(body[at+1:])
			body = body[:at]
		}
		parts := strings.Split(body, "/")
		if len(parts) == 2 && strings.HasPrefix(parts[0], "@") {
			ns = strings.TrimSpace(parts[0])
			name = strings.TrimSpace(parts[1])
		} else if len(parts) == 1 {
			name = strings.TrimSpace(parts[0])
		}
		if name != "" {
			slog.Debug("npmjsResolver fallback lookup", "namespace", ns, "name", name, "version", ver)
			return r.client.GetRepoURL(ctx, ns, name, ver)
		}
	}
	return "", nil
}

// nugetResolver resolves from NuGet registration metadata for nuget ecosystem.
type nugetResolver struct{ client *nuget.Client }

func (r *nugetResolver) Name() string { return "nuget" }
func (r *nugetResolver) Resolve(ctx context.Context, pkg *PackageResponse) (string, error) {
	if pkg == nil || r.client == nil {
		return "", nil
	}
	eco := strings.ToLower(strings.TrimSpace(pkg.Version.VersionKey.System))
	if eco != "nuget" {
		return "", nil
	}
	name := pkg.Version.VersionKey.Name
	ver := pkg.Version.VersionKey.Version
	return r.client.GetRepoURL(ctx, name, ver)
}

// mavenResolver resolves from Maven POM SCM metadata for maven ecosystem.
type mavenResolver struct{ client *maven.Client }

func (r *mavenResolver) Name() string { return "maven" }
func (r *mavenResolver) Resolve(ctx context.Context, pkg *PackageResponse) (string, error) {
	if pkg == nil || r.client == nil {
		return "", nil
	}
	eco := strings.ToLower(strings.TrimSpace(pkg.Version.VersionKey.System))
	if eco != "maven" {
		return "", nil
	}
	// Parse PURL to extract groupId/artifactId/version
	parser := purl.NewParser()
	parsed, err := parser.Parse(pkg.Version.PURL)
	if err != nil || parsed == nil {
		return "", nil
	}
	groupID := strings.TrimSpace(parsed.Namespace())
	artifactID := strings.TrimSpace(parsed.Name())
	version := strings.TrimSpace(parsed.Version())
	if groupID == "" || artifactID == "" || version == "" {
		return "", nil
	}
	return r.client.GetRepoURL(ctx, groupID, artifactID, version)
}

// pypiResolver resolves from PyPI project metadata for pypi ecosystem.
type pypiResolver struct{ client *pypi.Client }

func (r *pypiResolver) Name() string { return "pypi" }
func (r *pypiResolver) Resolve(ctx context.Context, pkg *PackageResponse) (string, error) {
	if pkg == nil || r.client == nil {
		return "", nil
	}
	eco := strings.ToLower(strings.TrimSpace(pkg.Version.VersionKey.System))
	if eco != "pypi" {
		return "", nil
	}
	name := pkg.Version.VersionKey.Name
	return r.client.GetRepoURL(ctx, name)
}

// buildRepoResolvers assembles a chain of resolvers, ordered by priority.
func (c *DepsDevClient) buildRepoResolvers() []RepoResolver {
	resolvers := []RepoResolver{
		&linksResolver{},
		&relatedProjectsResolver{},
	}
	if c.npm != nil {
		resolvers = append(resolvers, &npmjsResolver{client: c.npm})
	}
	if c.rubygems != nil {
		resolvers = append(resolvers, &rubyGemsResolver{client: c.rubygems})
	}
	if c.packagist != nil {
		resolvers = append(resolvers, &packagistResolver{client: c.packagist})
	}
	if c.nuget != nil {
		resolvers = append(resolvers, &nugetResolver{client: c.nuget})
	}
	if c.maven != nil {
		resolvers = append(resolvers, &mavenResolver{client: c.maven})
	}
	if c.pypi != nil {
		resolvers = append(resolvers, &pypiResolver{client: c.pypi})
	}
	return resolvers
}

// resolveRepoURL iterates resolvers and returns the first non-empty URL.
func (c *DepsDevClient) resolveRepoURL(ctx context.Context, pkg *PackageResponse, purl string) string {
	for _, r := range c.buildRepoResolvers() {
		url, err := r.Resolve(ctx, pkg)
		if err != nil {
			slog.Debug("RepoResolver failed", "resolver", r.Name(), "purl", purl, "error", err)
			continue
		}
		if url != "" {
			slog.Debug("RepoResolver resolved repo URL", "resolver", r.Name(), "purl", purl, "url", url)
			return url
		}
	}
	return ""
}

// hasRegistryResolver returns true if the ecosystem has a registry-specific
// resolver that can resolve a repo URL from a synthetic PackageResponse
// (i.e., without deps.dev package data).
func hasRegistryResolver(eco string) bool {
	switch eco {
	case "npm", "nuget", "maven", "gem", "rubygems", "composer", "packagist", "pypi":
		return true
	default:
		return false
	}
}
