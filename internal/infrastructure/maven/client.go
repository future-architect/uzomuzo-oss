package maven

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/future-architect/uzomuzo/internal/common"
	"github.com/future-architect/uzomuzo/internal/infrastructure/httpclient"
)

// Client fetches Maven POM files (typically from Maven Central) to extract SCM information.
//
// DDD Layer: Infrastructure
// Responsibility: External HTTP to Maven repositories to retrieve POM SCM URLs.
type Client struct {
	baseURL string
	http    *httpclient.Client
	// Maximum number of parent POMs to follow when looking for URLs.
	maxParentDepth int
	// searchBaseURL is the Maven Central Search API base (overridable for tests).
	searchBaseURL string
	searchHTTP    *httpclient.Client
}

// NewClient returns a Maven client with sane defaults targeting Maven Central.
func NewClient() *Client {
	return &Client{
		baseURL: "https://repo1.maven.org/maven2",
		http: httpclient.NewClient(
			&http.Client{Timeout: 10 * time.Second},
			httpclient.RetryConfig{MaxRetries: 2, BaseBackoff: 500 * time.Millisecond, MaxBackoff: 3 * time.Second, RetryOn5xx: true, RetryOnNetworkErr: true},
		),
		maxParentDepth: 3,
		searchBaseURL:  "https://search.maven.org",
		searchHTTP: httpclient.NewClient(
			&http.Client{Timeout: 10 * time.Second},
			httpclient.RetryConfig{MaxRetries: 1, BaseBackoff: 500 * time.Millisecond, MaxBackoff: 2 * time.Second, RetryOn5xx: true, RetryOnNetworkErr: true},
		),
	}
}

// SetBaseURL overrides the Maven repository base URL (useful for tests or mirrors).
func (c *Client) SetBaseURL(u string) { c.baseURL = strings.TrimRight(strings.TrimSpace(u), "/") }

// SetSearchBaseURL overrides the Maven Central Search API base URL (useful for tests).
func (c *Client) SetSearchBaseURL(u string) {
	c.searchBaseURL = strings.TrimRight(strings.TrimSpace(u), "/")
}

// SetHTTPClient allows injecting a custom HTTP client (for tests).
func (c *Client) SetHTTPClient(h *http.Client) {
	c.http = httpclient.NewClient(h, httpclient.DefaultRetryConfig())
}

// GetRepoURL attempts to fetch the POM for groupId:artifactId:version and extract SCM URL.
// It returns a normalized URL (preferably GitHub) or an empty string when not determinable.
func (c *Client) GetRepoURL(ctx context.Context, groupID, artifactID, version string) (string, error) {
	g := strings.TrimSpace(groupID)
	a := strings.TrimSpace(artifactID)
	v := strings.TrimSpace(version)
	if g == "" || a == "" || v == "" {
		return "", fmt.Errorf("groupId, artifactId and version are required")
	}
	return c.getRepoURLRecursive(ctx, g, a, v, 0, nil)
}

// pomModel captures a tiny subset of Maven POM necessary for SCM extraction.
type pomModel struct {
	XMLName xml.Name `xml:"project"`
	Parent  struct {
		GroupID    string `xml:"groupId"`
		ArtifactID string `xml:"artifactId"`
		Version    string `xml:"version"`
	} `xml:"parent"`
	// Description may contain deprecation / relocation notes we want to surface as heuristic evidence.
	Description string `xml:"description"`
	URL         string `xml:"url"`
	SCM         struct {
		URL                 string `xml:"url"`
		Connection          string `xml:"connection"`
		DeveloperConnection string `xml:"developerConnection"`
	} `xml:"scm"`
	IssueManagement struct {
		URL string `xml:"url"`
	} `xml:"issueManagement"`
	CIManagement struct {
		URL string `xml:"url"`
	} `xml:"ciManagement"`
	DistributionManagement struct {
		Site struct {
			URL string `xml:"url"`
		} `xml:"site"`
		Relocation struct {
			GroupID    string `xml:"groupId"`
			ArtifactID string `xml:"artifactId"`
			Version    string `xml:"version"`
			Message    string `xml:"message"`
		} `xml:"relocation"`
	} `xml:"distributionManagement"`
	Properties pomProperties `xml:"properties"`
}

// pomProperties is a dynamic map backed by <properties> arbitrary children.
type pomProperties map[string]string

func (p *pomProperties) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	*p = make(map[string]string)
	for {
		tok, err := d.Token()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		switch se := tok.(type) {
		case xml.StartElement:
			var v string
			if err := d.DecodeElement(&v, &se); err != nil {
				return err
			}
			(*p)[se.Name.Local] = v
		}
	}
}

// internal recursive resolver with parent traversal and property expansion
func (c *Client) getRepoURLRecursive(ctx context.Context, groupID, artifactID, version string, depth int, inherited *pomModel) (string, error) {
	if depth > c.maxParentDepth {
		return "", nil
	}
	pom, found, err := c.fetchPOM(ctx, groupID, artifactID, version)
	if err != nil {
		return "", err
	}
	if !found {
		return "", nil
	}

	// Merge properties (inherited first)
	props := c.mergeProps(pom, inherited, groupID, artifactID, version)

	// 1) Try SCM Trio after expansion
	for _, raw := range []string{
		expand(props, pom.SCM.URL),
		expand(props, pom.SCM.Connection),
		expand(props, pom.SCM.DeveloperConnection),
	} {
		if u := normalizeToGitHub(raw); u != "" {
			return u, nil
		}
	}
	// 2) Try other URL candidates
	otherCandidates := []string{
		expand(props, pom.URL),
		expand(props, pom.IssueManagement.URL),
		expand(props, pom.CIManagement.URL),
		expand(props, pom.DistributionManagement.Site.URL),
	}
	for _, raw := range otherCandidates {
		if u := normalizeToGitHub(raw); u != "" {
			return u, nil
		}
	}

	// 2.5) Last resort: scrape HTML of non-GitHub pages to find a GitHub link
	for _, page := range otherCandidates {
		p := strings.TrimSpace(page)
		if p == "" {
			continue
		}
		// Skip if already GitHub (would have matched above)
		if strings.Contains(strings.ToLower(p), "github.com") {
			continue
		}
		if gh := c.scrapeFirstGitHubFromHTML(ctx, p); gh != "" {
			slog.Debug("maven: html scrape found github", "page", p, "repo", gh)
			return gh, nil
		}
	}

	// 3) Try parent
	if pom.Parent.GroupID != "" && pom.Parent.ArtifactID != "" && pom.Parent.Version != "" {
		return c.getRepoURLRecursive(ctx, strings.TrimSpace(pom.Parent.GroupID), strings.TrimSpace(pom.Parent.ArtifactID), strings.TrimSpace(pom.Parent.Version), depth+1, pom)
	}
	return "", nil
}

// buildPOMURL constructs canonical POM URL.
func (c *Client) buildPOMURL(groupID, artifactID, version string) (string, error) {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base url: %w", err)
	}
	groupPath := strings.ReplaceAll(groupID, ".", "/")
	pomName := fmt.Sprintf("%s-%s.pom", artifactID, version)
	base.Path = path.Join(base.Path, groupPath, artifactID, version, pomName)
	return base.String(), nil
}

// fetchPOM performs a single HTTP GET + XML decode for a POM.
// Returns (model,true,nil) on success, (nil,false,nil) on 404, (nil,false,error) otherwise.
func (c *Client) fetchPOM(ctx context.Context, groupID, artifactID, version string) (*pomModel, bool, error) {
	g := strings.TrimSpace(groupID)
	a := strings.TrimSpace(artifactID)
	v := strings.TrimSpace(version)
	if g == "" || a == "" || v == "" {
		return nil, false, fmt.Errorf("groupId, artifactId and version are required")
	}
	pomURL, err := c.buildPOMURL(g, a, v)
	if err != nil {
		return nil, false, fmt.Errorf("maven build url: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pomURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("maven build request: %w", err)
	}
	req.Header.Set("User-Agent", "uzomuzo-maven-client/1.0 (+https://github.com/future-architect/uzomuzo)")
	slog.Debug("maven: http get", "phase", "fetch_pom", "url", pomURL, "group", g, "artifact", a, "version", v)
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, false, fmt.Errorf("maven http failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.CopyN(io.Discard, resp.Body, 1024)
		if resp.StatusCode == http.StatusNotFound {
			slog.Debug("maven: http not found", "phase", "fetch_pom", "status", resp.StatusCode, "url", pomURL)
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("maven http status %d", resp.StatusCode)
	}
	var pom pomModel
	if err := xml.NewDecoder(resp.Body).Decode(&pom); err != nil {
		return nil, false, fmt.Errorf("maven decode pom: %w", err)
	}
	return &pom, true, nil
}

// merge properties from parent and current, and inject project.* defaults
func (c *Client) mergeProps(cur, parent *pomModel, g, a, v string) map[string]string {
	out := map[string]string{
		"project.groupId":    g,
		"project.artifactId": a,
		"project.version":    v,
		"pom.groupId":        g,
		"pom.artifactId":     a,
		"pom.version":        v,
	}
	if parent != nil {
		for k, val := range parent.Properties {
			out[k] = val
		}
	}
	if cur != nil {
		for k, val := range cur.Properties {
			out[k] = val
		}
	}
	return out
}

var propRE = regexp.MustCompile(`\$\{([^}]+)\}`)

// expand resolves ${...} placeholders with provided properties (best-effort, max iterations)
func expand(props map[string]string, s string) string {
	if s == "" {
		return ""
	}
	const maxIter = 5
	out := s
	for i := 0; i < maxIter; i++ {
		changed := false
		out = propRE.ReplaceAllStringFunc(out, func(m string) string {
			key := strings.TrimSuffix(strings.TrimPrefix(m, "${"), "}")
			if val, ok := props[key]; ok {
				changed = true
				return val
			}
			return m
		})
		if !changed {
			break
		}
	}
	return out
}

// normalizeToGitHub normalizes arbitrary URL-ish strings to base GitHub repo URL if possible.
func normalizeToGitHub(raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" {
		return ""
	}
	// Handle scm:* prefixes early
	if strings.HasPrefix(strings.ToLower(u), "scm:") {
		u = strings.TrimPrefix(u, "scm:git:")
		u = strings.TrimPrefix(u, "scm:hg:")
		u = strings.TrimPrefix(u, "scm:svn:")
		u = strings.TrimPrefix(u, "scm:")
	}

	// Special-case: Apache GitBox / legacy git-wip-us mapping to GitHub
	if gh := common.MapApacheHostedToGitHub(u); gh != "" {
		return gh
	}
	norm := common.NormalizeRepositoryURL(u)
	if norm == "" {
		return ""
	}
	// Accept only GitHub; derive base if deep url
	if base := deriveGitHubBase(norm); base != "" {
		return base
	}
	if common.IsValidGitHubURL(norm) {
		return norm
	}
	return ""
}

// deriveGitHubBase reduces GitHub subpaths (issues/wiki/tree/blob/etc.) to https://github.com/owner/repo
func deriveGitHubBase(norm string) string {
	s := strings.TrimSpace(norm)
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	if !strings.HasPrefix(lower, "https://github.com/") && !strings.HasPrefix(lower, "http://github.com/") && !strings.HasPrefix(lower, "github.com/") {
		return ""
	}
	// Ensure https scheme for final
	if strings.HasPrefix(lower, "github.com/") {
		s = "https://" + s
	}
	// Trim after /owner/repo
	u, err := url.Parse(s)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return ""
	}
	base := "https://github.com/" + parts[0] + "/" + parts[1]
	return base
}

// deriveGitHubFromApacheHosted maps Apache Git hosting URLs to the canonical GitHub repository URL.
//
// Supported patterns (non-exhaustive):
//   - https://gitbox.apache.org/repos/asf?p=<repo>.git;...
//   - https://gitbox.apache.org/repos/asf/<repo>.git
//   - https://git-wip-us.apache.org/repos/asf/<repo>.git
// Returns: https://github.com/apache/<repo> when derivable, otherwise "".
// deriveGitHubFromApacheHosted moved to internal/common as MapApacheHostedToGitHub and reused across layers.

// scrapeFirstGitHubFromHTML fetches the page and returns the first GitHub repository URL found in the HTML.
func (c *Client) scrapeFirstGitHubFromHTML(ctx context.Context, pageURL string) string {
	// Only attempt HTTP(S) pages; skip unsupported schemes and unresolved placeholders
	if !isHTTPURL(pageURL) {
		slog.Debug("maven: skip non-http page for scrape", "url", pageURL)
		return ""
	}
	if containsUnresolvedPlaceholder(pageURL) {
		slog.Debug("maven: skip page with unresolved placeholder for scrape", "url", pageURL)
		return ""
	}

	// Use a short timeout specifically for scraping to avoid long retries on dead hosts
	scrapeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(scrapeCtx, http.MethodGet, pageURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "uzomuzo-maven-client/1.0 (+https://github.com/future-architect/uzomuzo)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	// Avoid known-dead legacy hosts that often hang (e.g., dev.java.net)
	if u, parseErr := url.Parse(pageURL); parseErr == nil {
		host := strings.ToLower(u.Host)
		if strings.HasSuffix(host, "dev.java.net") || host == "java.net" {
			slog.Debug("maven: skip legacy host for scrape", "host", host, "url", pageURL)
			return ""
		}
	}
	// IMPORTANT: use the scrapeCtx (short timeout) for HTTP calls to avoid long hangs
	resp, err := c.http.Do(scrapeCtx, req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return ""
	}
	const maxRead = 512 * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxRead))
	if err != nil && !errors.Is(err, io.EOF) {
		return ""
	}
	// Simple regex to find a GitHub URL
	re := regexp.MustCompile(`https?://github\.com/[^"'\s<>]+`)
	if m := re.Find(body); len(m) > 0 {
		candidate := string(m)
		normalized := common.NormalizeRepositoryURL(candidate)
		if normalized != "" && common.IsValidGitHubURL(normalized) {
			if base := deriveGitHubBase(normalized); base != "" {
				return base
			}
			return normalized
		}
		if common.IsValidGitHubURL(candidate) {
			if base := deriveGitHubBase(candidate); base != "" {
				return base
			}
			return candidate
		}
	}
	return ""
}

// isHTTPURL returns true if the URL uses http or https scheme
func isHTTPURL(raw string) bool {
	s := strings.TrimSpace(strings.ToLower(raw))
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// containsUnresolvedPlaceholder checks for common ${...} placeholders or their URL-encoded forms
func containsUnresolvedPlaceholder(raw string) bool {
	if strings.Contains(raw, "${") || strings.Contains(raw, "}") {
		return true
	}
	lower := strings.ToLower(raw)
	// %7B == '{', %7D == '}'
	return strings.Contains(lower, "%7b") || strings.Contains(lower, "%7d")
}

// RelocationInfo models Maven POM <distributionManagement><relocation> details.
type RelocationInfo struct {
	GroupID    string
	ArtifactID string
	Version    string
	Message    string
	// POMURL is the URL of the POM where relocation was found (helpful for evidence reference)
	POMURL string
}

// It reuses internal POM resolution (parent traversal + property expansion) already
// implemented in GetRepoURL / GetRelocation paths. We intentionally do NOT perform
// any additional HTTP calls beyond the POM fetch chain.
// GetRelocation fetches the POM for the given coordinates and returns relocation information if present.
//
// DDD Layer: Infrastructure
// Responsibility: Read Maven repository metadata (POM) for first-party EOL-like signals.
// Args: groupID, artifactID, version must be non-empty.
// Returns: (info, found, error)
func (c *Client) GetRelocation(ctx context.Context, groupID, artifactID, version string) (*RelocationInfo, bool, error) {
	pom, found, err := c.fetchPOM(ctx, groupID, artifactID, version)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}
	rel := pom.DistributionManagement.Relocation
	slog.Debug("maven: relocation parsed",
		"groupId", strings.TrimSpace(rel.GroupID),
		"artifactId", strings.TrimSpace(rel.ArtifactID),
		"version", strings.TrimSpace(rel.Version),
		"message", strings.TrimSpace(rel.Message),
	)
	if strings.TrimSpace(rel.GroupID) == "" && strings.TrimSpace(rel.ArtifactID) == "" && strings.TrimSpace(rel.Version) == "" && strings.TrimSpace(rel.Message) == "" {
		slog.Debug("maven: no relocation element in POM")
		return nil, false, nil
	}
	pomURL, _ := c.buildPOMURL(groupID, artifactID, version)
	info := &RelocationInfo{
		GroupID:    strings.TrimSpace(rel.GroupID),
		ArtifactID: strings.TrimSpace(rel.ArtifactID),
		Version:    strings.TrimSpace(rel.Version),
		Message:    strings.TrimSpace(rel.Message),
		POMURL:     pomURL,
	}
	return info, true, nil
}

// POMInfo aggregates description + relocation evidence from a single POM.
//
// DDD Layer: Infrastructure
// Responsibility: Provide normalized textual metadata for heuristic lifecycle evidence.
type POMInfo struct {
	Description string
	Relocation  *RelocationInfo
	POMURL      string
}

// FetchPOMInfo fetches and parses the POM for given coordinates, returning description and relocation info.
// It performs a single POM request (no parent traversal); this is intentional to limit HTTP cost for
// heuristic evidence. Parent traversal for description could be added later if needed.
// Returns: (info, true, nil) on success with data; (nil, false, nil) when not found; (nil,false,error) on errors.
func (c *Client) FetchPOMInfo(ctx context.Context, groupID, artifactID, version string) (*POMInfo, bool, error) {
	pom, found, err := c.fetchPOM(ctx, groupID, artifactID, version)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}
	rel := pom.DistributionManagement.Relocation
	var reloc *RelocationInfo
	if strings.TrimSpace(rel.ArtifactID) != "" || strings.TrimSpace(rel.GroupID) != "" || strings.TrimSpace(rel.Version) != "" || strings.TrimSpace(rel.Message) != "" {
		pomURL, _ := c.buildPOMURL(groupID, artifactID, version)
		reloc = &RelocationInfo{
			GroupID:    strings.TrimSpace(rel.GroupID),
			ArtifactID: strings.TrimSpace(rel.ArtifactID),
			Version:    strings.TrimSpace(rel.Version),
			Message:    strings.TrimSpace(rel.Message),
			POMURL:     pomURL,
		}
	}
	pomURL, _ := c.buildPOMURL(groupID, artifactID, version)
	return &POMInfo{
		Description: strings.TrimSpace(pom.Description),
		Relocation:  reloc,
		POMURL:      pomURL,
	}, true, nil
}

// searchResponse models the Maven Central Search (Solr) JSON response.
type searchResponse struct {
	Response struct {
		NumFound int `json:"numFound"`
		Docs     []struct {
			GroupID    string `json:"g"`
			ArtifactID string `json:"a"`
		} `json:"docs"`
	} `json:"response"`
}

// SearchByArtifactID queries the Maven Central Search API to find the groupId
// for a given artifactId. It returns the groupId only when exactly one result
// is found (unambiguous match). When zero or multiple results are found, it
// returns ("", false, nil). This is used as a fallback to resolve Maven PURLs
// that are missing the namespace (groupId).
//
// DDD Layer: Infrastructure
// Responsibility: Query Maven Central Search Solr API for artifact coordinate resolution.
func (c *Client) SearchByArtifactID(ctx context.Context, artifactID string) (groupID string, found bool, err error) {
	a := strings.TrimSpace(artifactID)
	if a == "" {
		return "", false, nil
	}

	searchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// rows=2: we only care whether there is exactly 1 result or more
	apiURL := fmt.Sprintf("%s/solrsearch/select?q=a:%s&rows=2&wt=json",
		c.searchBaseURL, url.QueryEscape(a))

	req, err := http.NewRequestWithContext(searchCtx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", false, fmt.Errorf("maven search build request: %w", err)
	}
	req.Header.Set("User-Agent", "uzomuzo-maven-client/1.0 (+https://github.com/future-architect/uzomuzo)")

	resp, err := c.searchHTTP.Do(searchCtx, req)
	if err != nil {
		return "", false, fmt.Errorf("maven search http failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.CopyN(io.Discard, resp.Body, 1024)
		return "", false, fmt.Errorf("maven search http status %d", resp.StatusCode)
	}

	var result searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", false, fmt.Errorf("maven search decode: %w", err)
	}

	if result.Response.NumFound != 1 || len(result.Response.Docs) != 1 {
		slog.Debug("maven search: ambiguous or no results",
			"artifact_id", a,
			"num_found", result.Response.NumFound,
		)
		return "", false, nil
	}

	g := strings.TrimSpace(result.Response.Docs[0].GroupID)
	if g == "" {
		return "", false, nil
	}

	slog.Info("maven search: resolved groupId",
		"artifact_id", a,
		"group_id", g,
	)
	return g, true, nil
}
