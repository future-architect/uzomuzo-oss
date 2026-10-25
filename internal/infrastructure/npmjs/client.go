package npmjs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common"
	"github.com/future-architect/uzomuzo-oss/internal/common/links"
	"github.com/future-architect/uzomuzo-oss/internal/common/maildomain"
	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/httpclient"
)

// Client is a minimal npm registry client used to resolve repository URLs.
//
// DDD Layer: Infrastructure
// Responsibility: External HTTP call to registry.npmjs.org to fetch package metadata.
//
// Authoritative docs and behavior references:
//   - npm package metadata endpoint (GET): https://registry.npmjs.org/<package>
//     Shape: top-level "versions" object keyed by version, "dist-tags" (e.g., latest), and metadata fields
//     Reference (metadata schema examples):
//     https://github.com/npm/registry/blob/master/docs/responses/package-metadata.md
//   - package.json fields used for repository inference:
//     repository: https://docs.npmjs.com/cli/v10/configuring-npm/package-json#repository
//     homepage:   https://docs.npmjs.com/cli/v10/configuring-npm/package-json#homepage
//     bugs:       https://docs.npmjs.com/cli/v10/configuring-npm/package-json#bugs
//
// Concrete examples:
//   - Unscoped package (lodash):
//     Endpoint: https://registry.npmjs.org/lodash
//     Possible repository field in versions[x].repository:
//     { "type": "git", "url": "git+https://github.com/lodash/lodash.git" }
//     Normalization keeps: https://github.com/lodash/lodash
//   - Scoped package (@types/node):
//     Endpoint (URL-encoded): https://registry.npmjs.org/%40types%2Fnode
//     Often publishes repository/homepage/bugs at top-level metadata (not only per-version).
//     This client first checks top-level repository/homepage/bugs, then version-specific data.
//
// Resolution policy implemented here (in order):
//  1. Top-level metadata: repository.url -> homepage -> bugs.url (some packages publish repo only at top-level)
//  2. Requested version's metadata (if a version is provided): repository -> homepage -> bugs.url
//  3. dist-tags.latest version's metadata (repository -> homepage -> bugs.url)
//  4. Any version's metadata (first hit) as a last resort
//     - Only GitHub URLs are retained after normalization via common.NormalizeRepositoryURL.
type Client struct {
	baseURL string
	http    *httpclient.Client
	// lookupTimeout, when positive, is one deadline for a GetPublishHistory
	// call's HTTP work: every attempt, the waits between retries, and reading
	// the body. Decoding a body already read is not interrupted.
	lookupTimeout time.Duration
}

// NewClient creates a new npmjs Client with sane defaults.
func NewClient() *Client {
	return &Client{
		baseURL: "https://registry.npmjs.org",
		http:    httpclient.NewClient(&http.Client{Timeout: 3 * time.Second}, httpclient.RegistryRetryConfig()),
	}
}

// packumentTimeout bounds a full-packument lookup's HTTP work, retries
// included. NewClient's 3-second limit covers the whole body, and the
// packument of a package with thousands of versions runs to megabytes.
const packumentTimeout = 20 * time.Second

// maxPackumentBytes caps a packument read. The largest seen, react-native and
// typescript, are about 16 MB uncompressed; a body cut at the cap fails to
// decode and the package is treated as not asked.
const maxPackumentBytes = 64 << 20

// NewPackumentClient creates a Client for GetPublishHistory, whose full-packument
// responses need a longer timeout than NewClient's metadata lookups.
func NewPackumentClient() *Client {
	c := NewClient()
	c.http = httpclient.NewClient(&http.Client{Timeout: packumentTimeout}, httpclient.RegistryRetryConfig())
	// The per-attempt limit alone lets two retries stretch one lookup to about
	// a minute, and the scan waits for it.
	c.lookupTimeout = packumentTimeout
	return c
}

// SetBaseURL overrides the registry base URL (useful for tests).
func (c *Client) SetBaseURL(u string) { c.baseURL = strings.TrimRight(u, "/") }

// SetHTTPClient overrides the underlying HTTP client (useful for tests).
// Uses RegistryRetryConfig so injected test clients exercise the same retry
// policy as production (DefaultRetryConfig was a prod/test parity gap).
func (c *Client) SetHTTPClient(h *http.Client) {
	c.http = httpclient.NewClient(h, httpclient.RegistryRetryConfig())
}

// GetRepoURL attempts to resolve a GitHub repository URL from npm registry metadata.
// It prefers version-specific repository/homepage/bugs fields when a version is provided,
// then falls back to top-level metadata.
// Returns an empty string when nothing usable is found or the URL is not a GitHub repo.
func (c *Client) GetRepoURL(ctx context.Context, namespace, name, version string) (string, error) {
	pkg := strings.TrimSpace(name)
	ns := strings.TrimSpace(namespace)
	if pkg == "" {
		return "", nil
	}
	full := links.JoinNpmName(ns, pkg)
	endpoint := fmt.Sprintf("%s/%s", c.baseURL, url.PathEscape(full))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("npm request build failed: %w", err)
	}
	slog.Debug("npmjs request", "endpoint", endpoint, "namespace", ns, "name", pkg, "version", version)
	req.Header.Set("User-Agent", "uzomuzo-npmjs-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return "", fmt.Errorf("npm http failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("npm HTTP %d", resp.StatusCode)
	}

	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return "", fmt.Errorf("npm decode failed: %w", err)
	}

	// Try top-level fields first (common for meta packages like @types/*)
	if u := extractRepoHomeBugs(doc); u != "" {
		slog.Debug("npmjs resolved repo (top-level)", "url", u)
		return u, nil
	}
	// Fallback to version-specific fields: preferred version, then latest tag, then any version
	if u := repoFromVersion(doc, strings.TrimSpace(version)); u != "" {
		slog.Debug("npmjs resolved repo (version-specific: requested)", "url", u)
		return u, nil
	}
	if u := repoFromLatestTag(doc); u != "" {
		slog.Debug("npmjs resolved repo (version-specific: dist-tags.latest)", "url", u)
		return u, nil
	}
	if u := repoFromAnyVersion(doc); u != "" {
		slog.Debug("npmjs resolved repo (version-specific: any)", "url", u)
		return u, nil
	}
	return "", nil
}

func repoFromVersion(doc map[string]any, ver string) string {
	if ver == "" {
		return ""
	}
	versions, _ := doc["versions"].(map[string]any)
	if versions == nil {
		return ""
	}
	vdoc, _ := versions[ver].(map[string]any)
	if vdoc == nil {
		return ""
	}
	// repository
	if u := extractRepoURL(vdoc["repository"]); u != "" {
		return u
	}
	// homepage / bugs.url
	if u := extractHomeBugs(vdoc); u != "" {
		return u
	}
	return ""
}

func repoFromLatestTag(doc map[string]any) string {
	distTags, _ := doc["dist-tags"].(map[string]any)
	if distTags == nil {
		return ""
	}
	latest, _ := distTags["latest"].(string)
	if latest == "" {
		return ""
	}
	return repoFromVersion(doc, latest)
}

func repoFromAnyVersion(doc map[string]any) string {
	versions, _ := doc["versions"].(map[string]any)
	if versions == nil {
		return ""
	}
	for _, v := range versions {
		vdoc, _ := v.(map[string]any)
		if vdoc == nil {
			continue
		}
		if u := extractRepoURL(vdoc["repository"]); u != "" {
			return u
		}
		if u := extractHomeBugs(vdoc); u != "" {
			return u
		}
	}
	return ""
}

func extractRepoHomeBugs(doc map[string]any) string {
	if u := extractRepoURL(doc["repository"]); u != "" {
		return u
	}
	if u := extractHomeBugs(doc); u != "" {
		return u
	}
	return ""
}

func extractRepoURL(v any) string {
	switch t := v.(type) {
	case string:
		return normalizeGitHub(t)
	case map[string]any:
		if u, _ := t["url"].(string); u != "" {
			return normalizeGitHub(u)
		}
	}
	return ""
}

func extractHomeBugs(doc map[string]any) string {
	if hp, _ := doc["homepage"].(string); hp != "" {
		if u := normalizeGitHub(hp); u != "" {
			return u
		}
	}
	if bugs, _ := doc["bugs"].(map[string]any); bugs != nil {
		if u, _ := bugs["url"].(string); u != "" {
			if n := normalizeGitHub(u); n != "" {
				return n
			}
		}
	}
	return ""
}

func normalizeGitHub(raw string) string {
	norm := common.NormalizeRepositoryURL(raw)
	if norm == "" || !common.IsValidGitHubURL(norm) {
		return ""
	}
	return norm
}

// DeprecationInfo holds npm registry deprecation/unpublished signals for a package version.
type DeprecationInfo struct {
	Deprecated  bool   // true if the version is marked deprecated
	Message     string // deprecated message (if any)
	Unpublished bool   // true if the version is unpublished
	Successor   string // extracted successor package name (if any)
}

// GetDeprecation queries npm registry for the given package and version.
// Returns info, found=true if package exists, else found=false.
func (c *Client) GetDeprecation(ctx context.Context, namespace, name, version string) (*DeprecationInfo, bool, error) {
	pkg := strings.TrimSpace(name)
	ns := strings.TrimSpace(namespace)
	if pkg == "" {
		return nil, false, nil
	}
	full := links.JoinNpmName(ns, pkg)
	endpoint := fmt.Sprintf("%s/%s", c.baseURL, url.PathEscape(full))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false, fmt.Errorf("npm request build failed: %w", err)
	}
	req.Header.Set("User-Agent", "uzomuzo-npmjs-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, false, fmt.Errorf("npm http failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("npm HTTP %d", resp.StatusCode)
	}
	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, false, fmt.Errorf("npm decode failed: %w", err)
	}
	info := &DeprecationInfo{}
	// Check for unpublished
	if t, ok := doc["time"].(map[string]any); ok {
		if _, ok := t["unpublished"]; ok {
			info.Unpublished = true
		}
	}
	// Check for deprecated on the version
	if vmap, ok := doc["versions"].(map[string]any); ok {
		if v, ok := vmap[version].(map[string]any); ok {
			if msg, ok := v["deprecated"].(string); ok && msg != "" {
				info.Deprecated = true
				info.Message = msg
				if succ := extractNpmSuccessor(msg); succ != "" {
					// Self-reference suppression: compare normalized tokens ignoring case.
					if !strings.EqualFold(succ, pkg) && !strings.EqualFold(succ, full) {
						info.Successor = succ
					}
				}
			}
		}
	}
	return info, true, nil
}

// PublishHistory is the publish time of every version npm has recorded for a
// package, the subset it still serves, and the maintainer identities the
// packument names (see ADR-0030).
type PublishHistory struct {
	// PublishedAt maps version to publish time. npm keeps an unpublished
	// version's entry in the packument's "time" object, so this can list
	// versions that are no longer installable.
	PublishedAt map[string]time.Time
	// Installable holds the versions present in the packument's "versions".
	// It is nil when "versions" is missing or null, meaning unknown.
	Installable map[string]struct{}
	// Maintainers is the packument's top-level "maintainers" list. It is nil
	// when the field is missing or malformed.
	Maintainers []Person
	// FirstSeen maps each normalized e-mail (analysis.NormalizeEmail) to the
	// earliest publish time of a version whose "maintainers" or "_npmUser"
	// names it. Versions without a publish time are skipped.
	FirstSeen map[string]time.Time
	// VersionMaintainers maps version to its "maintainers" list. It is nil
	// unless the history was built by ParsePublishHistoryWithVersions.
	VersionMaintainers map[string][]Person
	// VersionPublisher maps version to its "_npmUser". It is nil unless the
	// history was built by ParsePublishHistoryWithVersions.
	VersionPublisher map[string]Person
	// Publishers maps visible versions to sanitized named publisher accounts.
	Publishers map[string]string
	// PublisherHistory summarizes each publisher account's latest dated
	// publishes and the publish times of versions whose publisher is not
	// visible.
	PublisherHistory analysis.PublisherPackageHistory
	// unattributed holds versions whose manifest names no publisher account.
	unattributed map[string]struct{}
}

// Person holds a maintainer or publisher identity from an npm packument.
type Person struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// FirstSeenBefore returns, for each normalized e-mail, the first publish time
// strictly before cutoff of a version that names it. It reads
// VersionMaintainers and VersionPublisher, so it returns an empty map for a
// history not built by ParsePublishHistoryWithVersions.
func (h *PublishHistory) FirstSeenBefore(cutoff time.Time) map[string]time.Time {
	out := make(map[string]time.Time)
	if h == nil {
		return out
	}
	for version, people := range h.VersionMaintainers {
		at := h.PublishedAt[version]
		if at.IsZero() || !at.Before(cutoff) {
			continue
		}
		recordFirstSeen(out, at, people...)
		recordFirstSeen(out, at, h.VersionPublisher[version])
	}
	return out
}

// ParsePublishHistoryWithVersions decodes a full npm packument and also keeps
// each version's maintainers and publisher, which the measurement command
// needs and the scan does not.
func ParsePublishHistoryWithVersions(r io.Reader) (*PublishHistory, error) {
	return decodePublishHistory(r, true)
}

// GetPublishHistory fetches the full packument of the named package ("name" or
// "@scope/name") and returns its per-version publish times. The abbreviated
// install document has no per-version times, so the full one is needed.
// Returns found=false when the package does not exist.
//
// Example: https://registry.npmjs.org/node-ipc lists 12.0.1 under "time"
// (2026-05-14T14:25:30Z) but not under "versions", because it was removed.
func (c *Client) GetPublishHistory(ctx context.Context, fullName string) (*PublishHistory, bool, error) {
	fullName = strings.TrimSpace(fullName)
	if fullName == "" {
		return nil, false, nil
	}
	if c.lookupTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.lookupTimeout)
		defer cancel()
	}
	endpoint := fmt.Sprintf("%s/%s", c.baseURL, url.PathEscape(fullName))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false, fmt.Errorf("npm request build failed: %w", err)
	}
	req.Header.Set("User-Agent", "uzomuzo-npmjs-client/1.0 (+https://github.com/future-architect/uzomuzo-oss)")
	resp, err := c.http.Do(ctx, req)
	if err != nil {
		return nil, false, fmt.Errorf("npm http failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("npm HTTP %d", resp.StatusCode)
	}
	h, err := parsePublishHistory(io.LimitReader(resp.Body, maxPackumentBytes))
	if err != nil {
		return nil, false, fmt.Errorf("npm packument %q: %w", fullName, err)
	}
	// Each slice shares its backing array with the map value, so the names
	// are filled in place.
	for _, publishes := range h.PublisherHistory.Latest {
		for i := range publishes {
			if publishes[i].Package == "" {
				publishes[i].Package = fullName
			}
		}
	}
	return h, true, nil
}

// extractNpmSuccessor tries to extract a successor package name from a deprecated message.
//
// Intentional limitations:
//   - We intentionally match only high-confidence phrases ("use ", "moved to ", "replaced by ")
//     to avoid false positives.
//   - Broader phrases like "migrate to" are NOT matched on purpose because they often point to
//     non-package targets (e.g., "migrate to ESM", "migrate to Node 18", "migrate to v3").
//     Allowing them would increase noise and degrade result quality.
//
// If we expand supported phrases in the future, we should validate the extracted token against
// npm package naming rules (optionally scoped "@scope/name") and add unit tests to lock in the
// intended behavior. See successor_test.go for negative cases that document this policy.
var npmPkgNamePattern = regexp.MustCompile(`^(?:@[-a-z0-9_.]+/)?[A-Za-z0-9][A-Za-z0-9._-]*$`)

func extractNpmSuccessor(msg string) string {
	raw := msg
	lower := strings.ToLower(msg)
	phrases := []string{"use ", "moved to ", "replaced by "}
	for _, p := range phrases {
		idx := strings.Index(lower, p)
		if idx >= 0 {
			rest := raw[idx+len(p):]
			fields := strings.Fields(rest)
			if len(fields) == 0 {
				continue
			}
			candidate := strings.Trim(fields[0], ".,;:!?()[]{}")
			if candidate == "" || !npmPkgNamePattern.MatchString(candidate) {
				continue
			}
			return candidate
		}
	}
	return ""
}

// parsePublishHistory decodes the "name", "time", "versions" and "maintainers"
// keys of a packument. Of each version manifest only "maintainers" and
// "_npmUser" are kept, as raw JSON: a packument with thousands of versions runs
// to megabytes, and the rest of the manifest is not needed here. Per-version
// identities are folded into FirstSeen, and each "_npmUser" into
// PublisherHistory. Only sanitized publisher names are retained per version
// in scan mode; full identities are not retained.
func parsePublishHistory(r io.Reader) (*PublishHistory, error) {
	return decodePublishHistory(r, false)
}

func decodePublishHistory(r io.Reader, retainVersions bool) (*PublishHistory, error) {
	type manifestIdentity struct {
		Maintainers json.RawMessage `json:"maintainers"`
		NPMUser     json.RawMessage `json:"_npmUser"`
	}
	var doc struct {
		Name        string                      `json:"name"`
		Time        map[string]json.RawMessage  `json:"time"`
		Versions    map[string]manifestIdentity `json:"versions"`
		Maintainers json.RawMessage             `json:"maintainers"`
	}
	if err := json.NewDecoder(r).Decode(&doc); err != nil {
		return nil, fmt.Errorf("npm decode failed: %w", err)
	}
	h := &PublishHistory{PublishedAt: make(map[string]time.Time, len(doc.Time)), FirstSeen: make(map[string]time.Time)}
	if retainVersions {
		h.VersionMaintainers = make(map[string][]Person, len(doc.Versions))
		h.VersionPublisher = make(map[string]Person, len(doc.Versions))
	}
	if doc.Versions != nil {
		h.Publishers = make(map[string]string)
	}
	h.Maintainers = decodePeople(doc.Maintainers)
	// A missing or null "versions" leaves Installable nil (unknown), so a
	// burst is not reported as removed without evidence; "{}" stays known-empty.
	if doc.Versions != nil {
		h.Installable = make(map[string]struct{}, len(doc.Versions))
	}
	for v, raw := range doc.Time {
		// "created" and "modified" are package-level; "unpublished" is an object
		// written when the whole package is unpublished.
		if v == "created" || v == "modified" || v == "unpublished" {
			continue
		}
		// An entry that is not an RFC 3339 string carries no publish time; it is
		// skipped rather than failing the whole package.
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			continue
		}
		t, err := time.Parse(time.RFC3339, s)
		if err != nil || t.IsZero() {
			continue
		}
		h.PublishedAt[v] = t
	}
	for v := range doc.Versions {
		h.Installable[v] = struct{}{}
		manifest := doc.Versions[v]
		people := decodePeople(manifest.Maintainers)
		if retainVersions {
			h.VersionMaintainers[v] = people
		}
		publisher := decodePerson(manifest.NPMUser)
		account := analysis.StripControlRunes(publisher.Name)
		if account != "" {
			h.Publishers[v] = account
		}
		if retainVersions {
			h.VersionPublisher[v] = publisher
		}
		at := h.PublishedAt[v]
		if at.IsZero() {
			continue
		}
		if account != "" {
			h.PublisherHistory.AddPublisherPublish(account, publisherPublish(doc.Name, v, at, publisher.Email))
		} else {
			if h.unattributed == nil {
				h.unattributed = make(map[string]struct{})
			}
			h.unattributed[v] = struct{}{}
		}
		recordFirstSeen(h.FirstSeen, at, people...)
		recordFirstSeen(h.FirstSeen, at, publisher)
	}
	h.PublisherHistory.HiddenPublishedAt = h.hiddenPublishTimes(time.Time{})
	return h, nil
}

// hiddenPublishTimes returns the publish times of versions whose publisher is
// not visible, before cutoff when it is non-zero: versions missing from
// "versions", and versions whose manifest names no publisher account. It
// returns nil when "versions" was missing or null, since removal is then
// unknown.
func (h *PublishHistory) hiddenPublishTimes(cutoff time.Time) []time.Time {
	if h.Installable == nil {
		return nil
	}
	var out []time.Time
	for v, at := range h.PublishedAt {
		if !cutoff.IsZero() && !at.Before(cutoff) {
			continue
		}
		_, visible := h.Installable[v]
		_, unattributed := h.unattributed[v]
		if !visible || unattributed {
			out = append(out, at)
		}
	}
	return out
}

// recordFirstSeen keeps the earliest time, in UTC, each person's normalized
// e-mail appears.
func recordFirstSeen(m map[string]time.Time, at time.Time, people ...Person) {
	for _, p := range people {
		email := analysis.NormalizeEmail(p.Email)
		if email != "" && (m[email].IsZero() || at.Before(m[email])) {
			m[email] = at.UTC()
		}
	}
}

// publisherPublish leaves both domains empty for an address that the RDAP
// check would reject as invalid: no valid host, or no registrable domain.
func publisherPublish(packageName, version string, at time.Time, email string) analysis.PublisherPublish {
	p := analysis.PublisherPublish{Package: packageName, Version: version, PublishedAt: at}
	host := maildomain.Host(email)
	if host == "" {
		return p
	}
	if registrable, _ := maildomain.Registrable(host); registrable != "" {
		p.EmailDomain, p.RegistrableDomain = host, registrable
	}
	return p
}

// PublisherHistoryBefore builds a summary using only publishes before cutoff.
// It needs a history from ParsePublishHistoryWithVersions; any other history
// has no per-version publishers, so the summary lists no visible publish.
func (h *PublishHistory) PublisherHistoryBefore(packageName string, cutoff time.Time) analysis.PublisherPackageHistory {
	var out analysis.PublisherPackageHistory
	if h == nil {
		return out
	}
	for version, publisher := range h.VersionPublisher {
		at := h.PublishedAt[version]
		if at.IsZero() || !at.Before(cutoff) {
			continue
		}
		out.AddPublisherPublish(analysis.StripControlRunes(publisher.Name), publisherPublish(packageName, version, at, publisher.Email))
	}
	out.HiddenPublishedAt = h.hiddenPublishTimes(cutoff)
	return out
}

func decodePeople(raw json.RawMessage) []Person {
	var people []Person
	if json.Unmarshal(raw, &people) != nil {
		return nil
	}
	return people
}

func decodePerson(raw json.RawMessage) Person {
	var p Person
	// A missing or legacy string-shaped "_npmUser" leaves p empty, which
	// recordFirstSeen skips; one bad version must not fail the package.
	_ = json.Unmarshal(raw, &p)
	return p
}
