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

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
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
// The prime is an optimisation only, sent in chunks of 1,000. A failed or
// malformed chunk is logged and stores nothing, and the chunks after it are not
// sent; their packages, and every package of the failed chunk, fall back to
// their own query. Answers from chunks that succeeded before stay cached.
// Nothing is cached as clean unless OSV.dev said so for that package.
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
		if c.storeAnswer(chunk[i].Ecosystem+"/"+chunk[i].Name, []domain.AdvisoryRecord{}, asked, true) {
			empty++
		}
	}
	slog.Debug("osv_querybatch_primed", "packages", len(chunk), "no_advisories", empty)
	return nil
}

// batchOutcome is one decoded querybatch result: clean only when OSV.dev
// answered with an object that names no advisory and has no further page.
type batchOutcome struct{ clean bool }

// decodeBatchResults reads a querybatch body. Only an unambiguous body is an
// answer (see strictFields), and a result that is not an unambiguous "no
// advisory" is reported as not clean, never as clean.
func decodeBatchResults(raw []byte) ([]batchOutcome, error) {
	top, err := strictFields(raw, "results")
	if err != nil {
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
func cleanBatchResult(item json.RawMessage) bool {
	fields, err := strictFields(item, "vulns", "next_page_token")
	if err != nil {
		return false
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

// strictFields reads a JSON object and returns the values of the named keys,
// spelled exactly; other keys are ignored. It fails on anything but a single
// object, on a named key that appears twice, and on a key that differs from a
// name but that encoding/json would match to it, since that matching folds
// case, the Kelvin sign and the long s included.
//
// A struct decode is not used because of that folding and because a repeated
// key silently replaces the first: {"VULNS":null}, {"vulnſ":[...]} and
// {"vulns":null,"vulns":[]} would each decode as an empty advisory list.
func strictFields(raw []byte, names ...string) (map[string]json.RawMessage, error) {
	if t := bytes.TrimLeft(raw, " \t\r\n"); len(t) == 0 || t[0] != '{' {
		return nil, fmt.Errorf("not a JSON object")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil { // opening brace
		return nil, err
	}
	out := map[string]json.RawMessage{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("object key is not a string")
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
		for _, name := range names {
			if !strings.EqualFold(key, name) {
				continue
			}
			if key != name {
				return nil, fmt.Errorf("key %q is a variant spelling of %q", key, name)
			}
			if _, dup := out[name]; dup {
				return nil, fmt.Errorf("key %q appears twice", name)
			}
			out[name] = v
		}
	}
	if _, err := dec.Token(); err != nil { // closing brace
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("data after the JSON object")
	}
	return out, nil
}
