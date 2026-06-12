package depsdev

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	commonpurl "github.com/future-architect/uzomuzo-oss/internal/common/purl"
	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/goproxy"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/httpclient"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/maven"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/npmjs"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/nuget"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/packagist"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/pypi"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/rubygems"
)

// DepsDevClient implements the deps.dev API client.
//
// DDD Layer: Infrastructure
// Responsibility: Call deps.dev v3alpha endpoints to retrieve package/project/release info.
//
// Authoritative docs:
//   - API reference: https://docs.deps.dev/api/v3alpha/
//   - PURL endpoint:  GET /v3alpha/purl/{purl}
//   - Systems/Packages endpoint (versions): GET /v3alpha/systems/{system}/packages/{name}
//   - Project batch endpoint: POST /v3alpha/projectbatch (paginated via nextPageToken)
//
// Concrete examples (public API host: https://api.deps.dev):
//
//   - PURL endpoint (URL-escaped PURL path segment):
//     PURL: pkg:npm/lodash@4.17.21
//     GET  https://api.deps.dev/v3alpha/purl/pkg%3Anpm%2Flodash%404.17.21
//
//   - Systems/Packages endpoint (versions listing):
//     NPM      → GET https://api.deps.dev/v3alpha/systems/NPM/packages/lodash
//     NUGET    → GET https://api.deps.dev/v3alpha/systems/NUGET/packages/newtonsoft.json
//     RUBYGEMS → GET https://api.deps.dev/v3alpha/systems/RUBYGEMS/packages/rails
//
//   - Project batch endpoint (single page request):
//     POST https://api.deps.dev/v3alpha/projectbatch
//     Body:
//     {
//     "requests": [
//     { "projectKey": { "id": "github.com/lodash/lodash" } },
//     { "projectKey": { "id": "github.com/serilog/serilog" } }
//     ]
//     }
//     The response may include nextPageToken; clients should repeat the request with that token
//     until it is empty to retrieve all results for large batches.
//
// Key behaviors implemented:
//   - Release selection: derive Stable, PreRelease, and MaxSemver from versions
//   - Repository URL extraction priority from Package.Project.RelatedProjects and Links:
//     1) SOURCE_REPO link if present
//     2) Any link that normalizes to a valid GitHub URL
//   - Project batch pagination: handle NextPageToken across pages

// errPURLNotFound is a sentinel error returned by fetchPURLRaw when deps.dev
// returns 404 for a PURL lookup. Used by fetchPackageInfo to trigger fallback
// logic via errors.Is rather than fragile string matching.
var errPURLNotFound = errors.New("deps.dev PURL not found")

// DepsDevClient is the deps.dev API client.
type DepsDevClient struct {
	baseURL string
	client  *httpclient.Client
	config  *config.DepsDevConfig
	// optional helpers
	rubygems  *rubygems.Client
	packagist *packagist.Client
	npm       *npmjs.Client
	nuget     *nuget.Client
	maven     *maven.Client
	pypi      *pypi.Client
	goproxy   *goproxy.Client
	// advisoryCache caches advisory details by ID (immutable data, safe to cache for entire run).
	advisoryCache sync.Map
}

// NewDepsDevClient creates a new DepsDevClient configured with the provided settings.
// It sets up an HTTP client with retries and composes the base API URL.
func NewDepsDevClient(cfg *config.DepsDevConfig) *DepsDevClient {
	// HTTP client configuration
	httpClient := &http.Client{
		Timeout: cfg.Timeout,
	}

	// Retry configuration
	retryConfig := httpclient.RetryConfig{
		MaxRetries:        cfg.MaxRetries,
		BaseBackoff:       1 * time.Second,
		MaxBackoff:        30 * time.Second,
		RetryOn5xx:        true,
		RetryOnNetworkErr: true,
	}

	client := httpclient.NewClient(httpClient, retryConfig)

	return &DepsDevClient{
		baseURL: cfg.BaseURL + "/v3alpha",
		client:  client,
		config:  cfg,
		goproxy: goproxy.NewClient(),
	}
}

// WithRubyGems enables a RubyGems client for fallback resolution (used in wiring/tests).
func (c *DepsDevClient) WithRubyGems(g *rubygems.Client) *DepsDevClient {
	c.rubygems = g
	return c
}

// WithPackagist enables a Packagist client for fallback resolution (used in wiring/tests).
func (c *DepsDevClient) WithPackagist(p *packagist.Client) *DepsDevClient {
	c.packagist = p
	return c
}

// WithNPM enables an npmjs client for fallback resolution (used in wiring/tests).
func (c *DepsDevClient) WithNPM(n *npmjs.Client) *DepsDevClient {
	c.npm = n
	return c
}

// WithNuGet enables a NuGet client for fallback resolution (used in wiring/tests).
func (c *DepsDevClient) WithNuGet(n *nuget.Client) *DepsDevClient {
	c.nuget = n
	return c
}

// WithMaven enables a Maven client for fallback resolution (used in wiring/tests).
func (c *DepsDevClient) WithMaven(m *maven.Client) *DepsDevClient {
	c.maven = m
	return c
}

// WithPyPI enables a PyPI client for fallback resolution (used in wiring/tests).
func (c *DepsDevClient) WithPyPI(p *pypi.Client) *DepsDevClient {
	c.pypi = p
	return c
}

// purlpkgToParsed parses a PURL string using the shared parser.
func purlpkgToParsed(s string) (*commonpurl.ParsedPURL, error) {
	parser := commonpurl.NewParser()
	return parser.Parse(s)
}

// ExtractRepositoryURLFromLinks extracts and normalizes the repository URL from deps.dev links.
//
// Priority order:
//  1. SOURCE_REPO when present and valid
//  2. Fallback to any valid GitHub URL from other links
//
// Returns an empty string when nothing usable is found.
func ExtractRepositoryURLFromLinks(links []Link) string {
	// Priority 1: SOURCE_REPO first
	for _, link := range links {
		if link.Label == "SOURCE_REPO" {
			if gh := common.MapApacheHostedToGitHub(link.URL); gh != "" {
				return gh
			}
			if normalized := common.NormalizeRepositoryURL(link.URL); normalized != "" {
				return normalized
			}
		}
	}

	// Priority 2: any GitHub URL as fallback
	for _, link := range links {
		if gh := common.MapApacheHostedToGitHub(link.URL); gh != "" {
			return gh
		}
		if normalized := common.NormalizeRepositoryURL(link.URL); common.IsValidGitHubURL(normalized) {
			return normalized
		}
	}
	return ""
}

// truncateString returns s if it's shorter than or equal to max; otherwise it returns a shortened
// prefix with an ellipsis suffix. Helps keep error logs compact while still useful.
func truncateString(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
