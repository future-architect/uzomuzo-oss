package rdap

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/common/maildomain"
	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

type diskEntry struct {
	analysis.DomainRecord
	RequestURL string `json:"request_url"`
	FetchedAt  string `json:"fetched_at"`
	Schema     int    `json:"schema"`
}

const diskSchema = 2

const (
	rdapRequestTimeout = 10 * time.Second
	rdapRetryDelay     = 30 * time.Second
	rdapResponseLimit  = 1 << 20
	rdapMaxAttempts    = 2
	maxRedirects       = 5
)

// Client looks up registration data through RDAP and caches the results.
type Client struct {
	mu              sync.Mutex
	http            *http.Client
	base            map[string]string
	cache           map[string]analysis.DomainRecord
	last            map[string]time.Time
	cacheDir        string
	retryDelay      time.Duration
	requestInterval time.Duration
	bootstrapURL    string
	bootstrapCache  string
	now             func() time.Time
}

// NewClient returns an RDAP client configured from the embedded DNS bootstrap.
// It panics if the embedded bootstrap cannot be decoded, which is a build defect.
// Setters are meant to be called before the first lookup.
func NewClient() *Client {
	return &Client{
		http:            &http.Client{Timeout: rdapRequestTimeout, CheckRedirect: checkRedirect},
		base:            maps.Clone(embeddedBootstrap().bases),
		cache:           make(map[string]analysis.DomainRecord),
		last:            make(map[string]time.Time),
		retryDelay:      rdapRetryDelay,
		requestInterval: time.Second,
		bootstrapURL:    ianaBootstrapURL,
		now:             time.Now,
	}
}

// SetCacheDir sets the directory used for found and not-found lookup records.
// Entries do not expire: a later run reuses them as they were fetched.
func (c *Client) SetCacheDir(dir string) { c.cacheDir = dir }

// SetBaseURL overrides the RDAP endpoint for a TLD.
func (c *Client) SetBaseURL(tld, base string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.base[strings.ToLower(tld)] = base
}

// SetHTTPClient uses a copy of client for RDAP requests; a nil client is
// ignored. The copy gets the RDAP redirect policy when client has none, so the
// caller's client is left as it was.
func (c *Client) SetHTTPClient(client *http.Client) {
	if client == nil {
		return
	}
	cp := *client
	if cp.CheckRedirect == nil {
		cp.CheckRedirect = checkRedirect
	}
	c.http = &cp
}

// SetRetryDelay sets the pause between retryable requests.
func (c *Client) SetRetryDelay(delay time.Duration) { c.retryDelay = delay }

// SetRequestInterval sets the minimum time between requests to one host.
func (c *Client) SetRequestInterval(delay time.Duration) { c.requestInterval = delay }

var sharedMail = map[string]struct{}{
	"gmail.com": {}, "googlemail.com": {}, "outlook.com": {}, "hotmail.com": {}, "live.com": {}, "msn.com": {},
	"yahoo.com": {}, "yahoo.co.jp": {}, "icloud.com": {}, "me.com": {}, "mac.com": {}, "protonmail.com": {},
	"proton.me": {}, "pm.me": {}, "gmx.com": {}, "gmx.de": {}, "gmx.net": {}, "web.de": {}, "mail.ru": {},
	"yandex.ru": {}, "yandex.com": {}, "qq.com": {}, "163.com": {}, "126.com": {}, "foxmail.com": {},
	"aol.com": {}, "fastmail.com": {}, "fastmail.fm": {}, "hey.com": {}, "zoho.com": {},
	"tutanota.com": {}, "users.noreply.github.com": {}, "naver.com": {},
}

// Check classifies the domain in a maintainer address at the given time.
func (c *Client) Check(ctx context.Context, address analysis.MaintainerAddress, base time.Time) analysis.MaintainerDomainFinding {
	f := analysis.MaintainerDomainFinding{Account: address.Account, FirstSeen: address.FirstSeen}
	unchecked := func(reason analysis.MaintainerDomainReason) analysis.MaintainerDomainFinding {
		f.Kind, f.Reason = analysis.MaintainerDomainUnchecked, reason
		return f
	}
	host := maildomain.Host(address.Email)
	if host == "" {
		return unchecked(analysis.MaintainerDomainReasonInvalidAddress)
	}
	domain, private := maildomain.Registrable(host)
	f.Domain = domain
	if domain == "" {
		return unchecked(analysis.MaintainerDomainReasonInvalidAddress)
	}
	if address.FirstSeen.IsZero() {
		return unchecked(analysis.MaintainerDomainReasonNotInHistory)
	}
	if isSharedMail(host, domain) {
		return unchecked(analysis.MaintainerDomainReasonSharedMail)
	}
	if private {
		return unchecked(analysis.MaintainerDomainReasonPrivateSuffix)
	}
	rec, err := c.Lookup(ctx, domain)
	if err != nil {
		slog.Debug("rdap_lookup_failed", "domain", domain, "error", err)
	}
	f.Registered = rec.Registered
	f.Registrar = rec.Registrar
	f.DropCatch = analysis.IsDropCatchRegistrar(rec.Registrar)
	f.Kind = analysis.ClassifyMaintainerDomain(address.FirstSeen, base, rec)
	if rec.Status == analysis.DomainUnsupported {
		f.Reason = analysis.MaintainerDomainReasonNoRDAP
	}
	return f
}

// isSharedMail reports whether the mail host or its registrable domain is a
// shared provider; regional hosts such as mail.yahoo.co.jp count too.
func isSharedMail(host, domain string) bool {
	_, sharedHost := sharedMail[host]
	_, sharedDomain := sharedMail[domain]
	return sharedHost || sharedDomain
}

// Lookup returns the RDAP record for a registrable domain. The error is
// non-nil only when the record's status is DomainLookupFailed, and says why.
func (c *Client) Lookup(ctx context.Context, domain string) (analysis.DomainRecord, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	c.mu.Lock()
	defer c.mu.Unlock()
	if rec, ok := c.cache[domain]; ok {
		if rec.Status == analysis.DomainLookupFailed {
			return rec, fmt.Errorf("rdap lookup %q: an earlier attempt in this run failed", domain)
		}
		return rec, nil
	}
	if c.cacheDir != "" {
		if data, err := os.ReadFile(c.cachePath(domain)); err == nil {
			var entry diskEntry
			if json.Unmarshal(data, &entry) == nil && entry.Schema == diskSchema && entry.Status != analysis.DomainLookupFailed {
				rec := entry.DomainRecord
				c.cache[domain] = rec
				return rec, nil
			}
		}
	}
	parts := strings.Split(domain, ".")
	base := c.base[parts[len(parts)-1]]
	if base == "" {
		return analysis.DomainRecord{Status: analysis.DomainUnsupported}, nil
	}
	host := base
	if parsed, err := url.Parse(base); err == nil {
		host = parsed.Host
	}
	var rec analysis.DomainRecord
	var err error
	endpoint := strings.TrimRight(base, "/") + "/domain/" + url.PathEscape(domain)
	for attempt := 0; attempt < rdapMaxAttempts; attempt++ {
		if attempt > 0 {
			if err = wait(ctx, c.retryDelay); err != nil {
				break
			}
		}
		if err = wait(ctx, time.Until(c.last[host].Add(c.requestInterval))); err != nil {
			break
		}
		c.last[host] = time.Now()
		rec, err = c.request(ctx, endpoint)
		// Only transport failures, cut-off bodies and HTTP 408, 429 and 5xx are retried.
		var retry retryableError
		if err == nil || !errors.As(err, &retry) {
			break
		}
	}
	if err != nil {
		rec = analysis.DomainRecord{Status: analysis.DomainLookupFailed}
		err = fmt.Errorf("rdap lookup %q: %w", domain, err)
		// A lookup cut short by the caller is not remembered, so a later call
		// with a live context asks again.
		if ctx.Err() != nil {
			return rec, err
		}
	}
	c.cache[domain] = rec
	if c.cacheDir != "" && rec.Status != analysis.DomainLookupFailed {
		c.writeDiskEntry(domain, endpoint, rec)
	}
	return rec, err
}

// writeDiskEntry stores a record on a best-effort basis: the lookup result is
// already in memory, so a failed write only costs a repeat request next run.
func (c *Client) writeDiskEntry(domain, endpoint string, rec analysis.DomainRecord) {
	if err := os.MkdirAll(c.cacheDir, 0o700); err != nil {
		slog.Debug("rdap_cache_write_failed", "domain", domain, "error", err)
		return
	}
	data, err := json.Marshal(diskEntry{DomainRecord: rec, RequestURL: endpoint, FetchedAt: time.Now().UTC().Format(time.RFC3339), Schema: diskSchema})
	if err != nil {
		slog.Debug("rdap_cache_write_failed", "domain", domain, "error", err)
		return
	}
	if err := os.WriteFile(c.cachePath(domain), data, 0o600); err != nil {
		slog.Debug("rdap_cache_write_failed", "domain", domain, "error", err)
	}
}

func (c *Client) cachePath(domain string) string {
	return filepath.Join(c.cacheDir, fmt.Sprintf("%x.json", sha256.Sum256([]byte(domain))))
}

// retryableError marks a failure worth one more attempt.
type retryableError struct{ error }

// Unwrap returns the underlying failure.
func (e retryableError) Unwrap() error { return e.error }

// errRedirectRefused marks a redirect that checkRedirect refused; it is not
// retried.
var errRedirectRefused = errors.New("rdap: redirect refused")

// checkRedirect bounds redirect hops and refuses a downgrade from https.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > maxRedirects {
		return fmt.Errorf("%w: more than %d redirects", errRedirectRefused, maxRedirects)
	}
	if req.URL.User != nil {
		return fmt.Errorf("%w: userinfo in redirect", errRedirectRefused)
	}
	if len(via) > 0 && strings.EqualFold(via[0].URL.Scheme, "https") && !strings.EqualFold(req.URL.Scheme, "https") {
		return fmt.Errorf("%w: from https to %q", errRedirectRefused, req.URL.Scheme)
	}
	return nil
}

func (c *Client) request(ctx context.Context, endpoint string) (analysis.DomainRecord, error) {
	failed := analysis.DomainRecord{Status: analysis.DomainLookupFailed}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return failed, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/rdap+json")
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, errRedirectRefused) {
			return failed, err
		}
		return failed, retryableError{err}
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusNotFound:
		return analysis.DomainRecord{Status: analysis.DomainNotFound}, nil
	case http.StatusOK:
		var body struct {
			Events []struct {
				Action string `json:"eventAction"`
				Date   string `json:"eventDate"`
			} `json:"events"`
			Status   []string `json:"status"`
			Entities []struct {
				Roles      []string        `json:"roles"`
				VCardArray json.RawMessage `json:"vcardArray"`
			} `json:"entities"`
		}
		// A body cut off by the connection is retried; malformed JSON is not.
		data, err := io.ReadAll(io.LimitReader(resp.Body, rdapResponseLimit))
		if err != nil {
			return failed, retryableError{fmt.Errorf("read response: %w", err)}
		}
		if err := json.Unmarshal(data, &body); err != nil {
			return failed, fmt.Errorf("decode response: %w", err)
		}
		var registration time.Time
		count := 0
		for _, event := range body.Events {
			if !strings.EqualFold(event.Action, "registration") {
				continue
			}
			count++
			registration, err = time.Parse(time.RFC3339, event.Date)
			if err != nil {
				return failed, fmt.Errorf("parse registration date: %w", err)
			}
		}
		if count > 1 {
			return failed, fmt.Errorf("%d registration events", count)
		}
		if count == 0 && !analysis.HasDeletionStatus(body.Status) {
			return failed, errors.New("no registration event")
		}
		if count != 1 {
			registration = time.Time{}
		}
		// Stored in UTC so JSON and the notice show the same date.
		rec := analysis.DomainRecord{Status: analysis.DomainFound, Registered: registration.UTC(), Statuses: body.Status}
		for _, entity := range body.Entities {
			if !slices.ContainsFunc(entity.Roles, func(role string) bool { return strings.EqualFold(role, "registrar") }) {
				continue
			}
			var card []json.RawMessage
			if json.Unmarshal(entity.VCardArray, &card) != nil || len(card) != 2 {
				continue
			}
			var fields [][]json.RawMessage
			if json.Unmarshal(card[1], &fields) != nil {
				continue
			}
			for _, field := range fields {
				if len(field) < 4 {
					continue
				}
				var key, value string
				if json.Unmarshal(field[0], &key) == nil && strings.EqualFold(key, "fn") && json.Unmarshal(field[3], &value) == nil {
					// Shown in the terminal notice, so control characters are dropped.
					rec.Registrar = analysis.SanitizeExternalSummary(value)
					return rec, nil
				}
			}
		}
		return rec, nil
	default:
		err := fmt.Errorf("RDAP HTTP %d", resp.StatusCode)
		if resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
			return failed, retryableError{err}
		}
		return failed, err
	}
}

func wait(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
