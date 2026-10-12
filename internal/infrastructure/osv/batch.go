package osv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// maxBatchQueries is OSV.dev's limit on queries in one querybatch request.
const maxBatchQueries = 1000

// batchTimeout bounds one querybatch request, retries included (there are
// none). OSV.dev's stated latency for querybatch is 4 s at P90 and 6 s at P95
// (https://github.com/google/osv.dev/blob/master/docs/faq.md); past this the
// per-package queries take over.
var batchTimeout = 10 * time.Second

// PackageRef names one package in OSV's spelling, as OSVPackageFor returns it.
type PackageRef struct {
	Ecosystem string
	Name      string
}

// PrimeNoAdvisories asks OSV.dev, through /v1/querybatch, which of pkgs have no
// advisory at all, and records that answer for each of them. A later
// QueryPackage for such a package is then served from the cache instead of
// issuing its own request; a package with any advisory is left to
// QueryPackage, which fetches the full records.
//
// Most packages have no advisory: on the npm CLI lockfile, 741 of 839. One
// batch request replaces those 741 queries.
//
// The prime is an optimisation only. A failed or malformed batch is logged and
// leaves the cache untouched, so every package falls back to its own query;
// nothing is ever cached as clean unless OSV.dev said so for that package.
// With caching disabled (SetCacheTTL <= 0) the answers are dropped and every
// package is queried on its own, as before.
func (c *Client) PrimeNoAdvisories(ctx context.Context, pkgs []PackageRef) {
	var pending []PackageRef
	seen := map[string]bool{}
	for _, p := range pkgs {
		eco, n := strings.TrimSpace(p.Ecosystem), strings.TrimSpace(p.Name)
		if eco == "" || n == "" {
			continue
		}
		key := eco + "/" + n
		if seen[key] {
			continue
		}
		seen[key] = true
		if _, ok := c.cache.Get(key); ok {
			continue
		}
		if _, ok := c.noAdvisory.Get(key); ok {
			continue
		}
		pending = append(pending, PackageRef{Ecosystem: eco, Name: n})
	}
	for start := 0; start < len(pending); start += maxBatchQueries {
		chunk := pending[start:min(start+maxBatchQueries, len(pending))]
		if err := c.primeChunk(ctx, chunk); err != nil {
			// The remaining chunks would most likely fail the same way; leave
			// them to their own queries rather than spend another timeout.
			slog.Debug("osv_querybatch_failed", "packages", len(chunk), "error", err)
			return
		}
	}
}

func (c *Client) primeChunk(ctx context.Context, chunk []PackageRef) error {
	ctx, cancel := context.WithTimeout(ctx, batchTimeout)
	defer cancel()
	req := batchRequest{Queries: make([]queryRequest, len(chunk))}
	for i, p := range chunk {
		req.Queries[i] = queryRequest{Package: queryPackageRef{Name: p.Name, Ecosystem: p.Ecosystem}}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("osv querybatch body encode failed: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.resolvedBaseURL()+"/v1/querybatch", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("osv querybatch request build failed: %w", err)
	}
	httpReq.Header.Set("User-Agent", osvUserAgent)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	// Answers are stamped with the time they were asked, not received. Records
	// a concurrent query stores describe a later moment, so they always outlive
	// a no-advisory answer for the same package, whatever the TTL.
	asked := time.Now()
	resp, err := c.batchHTTP.Do(ctx, httpReq)
	if err != nil {
		return fmt.Errorf("osv querybatch http failed: %w", err)
	}
	// best-effort cleanup; the body is fully read below.
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("osv querybatch http status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxJSONResponseSize+1))
	if err != nil {
		return fmt.Errorf("osv querybatch read failed: %w", err)
	}
	if len(raw) > maxJSONResponseSize {
		return fmt.Errorf("osv querybatch response exceeded %d bytes", maxJSONResponseSize)
	}
	results, err := decodeBatchResults(raw)
	if err != nil {
		return err
	}
	// Results are positional. A count that does not match the queries cannot
	// be attributed to packages, so none of it is used.
	if len(results) != len(chunk) {
		return fmt.Errorf("osv querybatch returned %d results for %d queries", len(results), len(chunk))
	}
	empty := 0
	for i, r := range results {
		if !r.clean {
			continue
		}
		key := chunk[i].Ecosystem + "/" + chunk[i].Name
		c.storeMu.Lock()
		// Records stored by a concurrent query are newer than this answer.
		if _, has := c.cache.Get(key); !has && !asked.Before(c.ttlChangedAt) {
			c.noAdvisory.SetAt(key, true, asked)
			empty++
		}
		c.storeMu.Unlock()
	}
	slog.Debug("osv_querybatch_primed", "packages", len(chunk), "no_advisories", empty)
	return nil
}

// batchOutcome is one decoded querybatch result: clean only when OSV.dev
// answered with an object that names no advisory and has no further page.
type batchOutcome struct{ clean bool }

// decodeBatchResults applies the same strictness as queryPage: only a JSON
// object is an answer, and a result whose vulns field is an explicit null, or
// that is not an object at all, is not read as "no advisories".
func decodeBatchResults(raw []byte) ([]batchOutcome, error) {
	if trimmed := bytes.TrimLeft(raw, " \t\r\n"); len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, fmt.Errorf("osv querybatch response was not a JSON object")
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("osv querybatch decode failed: %w", err)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(top["results"], &items); err != nil {
		return nil, fmt.Errorf("osv querybatch results decode failed: %w", err)
	}
	out := make([]batchOutcome, len(items))
	for i, item := range items {
		out[i] = batchOutcome{clean: cleanBatchResult(item)}
	}
	return out, nil
}

// cleanBatchResult reports whether one result is an unambiguous "no advisory":
// an object whose vulns, if present, is an empty array and whose
// next_page_token, if present, is an empty string or null.
//
// Keys are read one by one instead of through a struct because encoding/json
// matches field names case-insensitively and lets a repeated key overwrite an
// earlier one: {"VULNS":null} or {"vulns":null,"vulns":[]} would otherwise
// decode as an empty list and read as clean.
func cleanBatchResult(item json.RawMessage) bool {
	var fields map[string]json.RawMessage
	if t := bytes.TrimLeft(item, " \t\r\n"); len(t) == 0 || t[0] != '{' {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(item))
	if _, err := dec.Token(); err != nil { // opening brace
		return false
	}
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return false
		}
		key, ok := tok.(string)
		if !ok {
			return false
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return false
		}
		folded := strings.ToLower(key)
		if folded != "vulns" && folded != "next_page_token" {
			continue
		}
		if seen[folded] {
			return false
		}
		seen[folded] = true
		if fields == nil {
			fields = map[string]json.RawMessage{}
		}
		fields[folded] = v
	}
	if v, ok := fields["vulns"]; ok {
		var vulns []json.RawMessage
		if string(bytes.TrimSpace(v)) == "null" || json.Unmarshal(v, &vulns) != nil || len(vulns) != 0 {
			return false
		}
	}
	if v, ok := fields["next_page_token"]; ok && string(bytes.TrimSpace(v)) != "null" {
		var token string
		if json.Unmarshal(v, &token) != nil || strings.TrimSpace(token) != "" {
			return false
		}
	}
	return true
}
