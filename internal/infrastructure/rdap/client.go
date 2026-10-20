package rdap

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"golang.org/x/net/publicsuffix"
)

//go:embed bootstrap.json
var bootstrapFS embed.FS

type bootstrap struct {
	Services [][][]string `json:"services"`
}

type diskEntry struct {
	analysis.DomainRecord
	RequestURL string `json:"request_url"`
	FetchedAt  string `json:"fetched_at"`
}

const (
	rdapRequestTimeout = 10 * time.Second
	rdapRetryDelay     = 30 * time.Second
	rdapResponseLimit  = 1 << 20
	rdapMaxAttempts    = 2
	maxDNSHostLength   = 253
	maxDNSLabelLength  = 63
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
}

// NewClient returns an RDAP client configured from the embedded DNS bootstrap.
func NewClient() *Client {
	data, _ := bootstrapFS.ReadFile("bootstrap.json")
	var b bootstrap
	_ = json.Unmarshal(data, &b)
	base := make(map[string]string)
	for _, service := range b.Services {
		if len(service) < 2 || len(service[1]) == 0 {
			continue
		}
		for _, tld := range service[0] {
			base[strings.ToLower(tld)] = service[1][0]
		}
	}
	return &Client{http: &http.Client{Timeout: rdapRequestTimeout}, base: base, cache: make(map[string]analysis.DomainRecord), last: make(map[string]time.Time), retryDelay: rdapRetryDelay, requestInterval: time.Second}
}

// SetCacheDir sets the directory used for successful lookup records.
func (c *Client) SetCacheDir(dir string) { c.cacheDir = dir }

// SetBaseURL overrides the RDAP endpoint for a TLD.
func (c *Client) SetBaseURL(tld, base string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.base[strings.ToLower(tld)] = base
}

// SetHTTPClient replaces the HTTP client used for RDAP requests.
func (c *Client) SetHTTPClient(client *http.Client) { c.http = client }

// SetRetryDelay sets the pause between retryable requests.
func (c *Client) SetRetryDelay(delay time.Duration) { c.retryDelay = delay }

// SetRequestInterval sets the minimum time between requests to one host.
func (c *Client) SetRequestInterval(delay time.Duration) { c.requestInterval = delay }

// Registrable resolves a mail host to its effective registered domain.
func Registrable(host string) (string, bool) {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	suffix, icann := publicsuffix.PublicSuffix(host)
	domain, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil || suffix == "" {
		return "", false
	}
	return domain, !icann && suffix != host[strings.LastIndex(host, ".")+1:]
}

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
	email := strings.ToLower(strings.TrimSpace(address.Email))
	at := strings.LastIndexByte(email, '@')
	if at < 1 || at == len(email)-1 || address.FirstSeen.IsZero() {
		f.Kind, f.Reason = analysis.MaintainerDomainUnchecked, "not_in_history"
		return f
	}
	host := email[at+1:]
	if !validMailHost(host) {
		f.Kind, f.Reason = analysis.MaintainerDomainUnchecked, "not_in_history"
		return f
	}
	domain, private := Registrable(host)
	f.Domain = domain
	if domain == "" {
		f.Kind, f.Reason = analysis.MaintainerDomainUnchecked, "not_in_history"
		return f
	}
	if _, shared := sharedMail[host]; shared {
		f.Kind, f.Reason = analysis.MaintainerDomainUnchecked, "free_mail"
		return f
	}
	if private {
		f.Kind, f.Reason = analysis.MaintainerDomainUnchecked, "private_suffix"
		return f
	}
	rec, _ := c.Lookup(ctx, domain)
	f.Registered = rec.Registered
	f.Kind = analysis.ClassifyMaintainerDomain(address.FirstSeen, base, rec)
	if rec.Status == analysis.DomainUnsupported {
		f.Reason = "no_rdap"
	}
	return f
}

func validMailHost(host string) bool {
	if len(host) > maxDNSHostLength || !strings.Contains(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > maxDNSLabelLength || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

// Lookup returns the RDAP record for a registrable domain.
func (c *Client) Lookup(ctx context.Context, domain string) (analysis.DomainRecord, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	c.mu.Lock()
	defer c.mu.Unlock()
	if rec, ok := c.cache[domain]; ok {
		return rec, nil
	}
	if c.cacheDir != "" {
		if data, err := os.ReadFile(c.cachePath(domain)); err == nil {
			var entry diskEntry
			if json.Unmarshal(data, &entry) == nil && entry.Status != analysis.DomainLookupFailed {
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
		if err == nil || rec.Status != analysis.DomainLookupFailed {
			break
		}
		// Only transport failures and rate limiting or server errors are retried.
		if _, retry := err.(retryableError); !retry {
			break
		}
	}
	if err != nil {
		rec = analysis.DomainRecord{Status: analysis.DomainLookupFailed}
	}
	c.cache[domain] = rec
	if c.cacheDir != "" && rec.Status != analysis.DomainLookupFailed {
		if e := os.MkdirAll(c.cacheDir, 0o700); e == nil {
			data, _ := json.Marshal(diskEntry{DomainRecord: rec, RequestURL: endpoint, FetchedAt: time.Now().UTC().Format(time.RFC3339)})
			_ = os.WriteFile(c.cachePath(domain), data, 0o600)
		}
	}
	return rec, nil
}

func (c *Client) cachePath(domain string) string {
	return filepath.Join(c.cacheDir, fmt.Sprintf("%x.json", sha256.Sum256([]byte(domain))))
}

type retryableError struct{ error }

func (c *Client) request(ctx context.Context, endpoint string) (analysis.DomainRecord, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return analysis.DomainRecord{Status: analysis.DomainLookupFailed}, err
	}
	req.Header.Set("Accept", "application/rdap+json")
	resp, err := c.http.Do(req)
	if err != nil {
		return analysis.DomainRecord{Status: analysis.DomainLookupFailed}, retryableError{err}
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
			Status []string `json:"status"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, rdapResponseLimit)).Decode(&body); err != nil {
			return analysis.DomainRecord{Status: analysis.DomainLookupFailed}, nil
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
				return analysis.DomainRecord{Status: analysis.DomainLookupFailed}, nil
			}
		}
		if count != 1 {
			return analysis.DomainRecord{Status: analysis.DomainLookupFailed}, nil
		}
		return analysis.DomainRecord{Status: analysis.DomainFound, Registered: registration, Statuses: body.Status}, nil
	default:
		rec := analysis.DomainRecord{Status: analysis.DomainLookupFailed}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
			return rec, retryableError{fmt.Errorf("RDAP HTTP %d", resp.StatusCode)}
		}
		return rec, nil
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
