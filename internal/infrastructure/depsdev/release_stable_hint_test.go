package depsdev

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/httpclient"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/pypi"
)

// This file exercises the ADR-0022 contract end to end: fetchLatestRelease
// combines deps.dev's versions listing with PyPI's own current-release field
// to bound Stable selection. All servers are httptest-local; nothing here
// touches the network.
//
// Fixture mirrors pkg:pypi/pydantic-extra-types as observed 2026-08-31 (see
// ADR-0022): deps.dev lists 2.11.0/2.11.1/2.11.2 with 2.11.2 isDefault=true,
// while PyPI's info.version is 2.11.1 (2.11.2 having been yanked in the real
// case that motivated the fix).
const pydanticExtraTypesVersionsJSON = `{
  "versions": [
    {"versionKey": {"version": "2.11.0"}, "publishedAt": "2026-01-01T00:00:00Z", "isDefault": false, "isDeprecated": false},
    {"versionKey": {"version": "2.11.1"}, "publishedAt": "2026-02-01T00:00:00Z", "isDefault": false, "isDeprecated": false},
    {"versionKey": {"version": "2.11.2"}, "publishedAt": "2026-03-01T00:00:00Z", "isDefault": true,  "isDeprecated": false}
  ]
}`

const leftPadVersionsJSON = `{
  "versions": [
    {"versionKey": {"version": "1.3.0"}, "publishedAt": "2026-01-01T00:00:00Z", "isDefault": true, "isDeprecated": false}
  ]
}`

// newDepsDevTestServer serves a fixed versions payload at
// /v3alpha/systems/{system}/packages/{name} and 404s everything else.
func newDepsDevTestServer(t *testing.T, path, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newDepsDevClient builds a DepsDevClient pointed at depsDevSrv, with no
// retries so httptest-server errors surface immediately in test failures
// instead of being retried and slowing the suite down.
func newDepsDevClient(depsDevSrv *httptest.Server) *DepsDevClient {
	return NewDepsDevClient(&config.DepsDevConfig{
		BaseURL:    depsDevSrv.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 0,
		BatchSize:  100,
	})
}

// pydanticExtraTypesPyPIPath is the exact PyPI request path the client must
// hit for the pydantic-extra-types fixture: /pypi/{name}/json with the
// PURL-normalized (lowercase, hyphenated) package name. Asserting the exact
// path — not just "some request happened" — catches a wrong package name or
// a "_"/"-" normalization mismatch that a permissive handler would miss.
const pydanticExtraTypesPyPIPath = "/pypi/pydantic-extra-types/json"

// pypiCountingServer wraps a PyPI response body with a request counter, so
// tests can assert exactly how many times PyPI was actually contacted. Any
// request whose method or path does not match exactly gets 404'd instead of
// being served the fixture — a permissive handler that accepts any
// method/path would not catch a package-name normalization bug.
func pypiCountingServer(t *testing.T, expectedPath string, status int, body string) (*httptest.Server, *int32) {
	t.Helper()
	var count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != expectedPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		atomic.AddInt32(&count, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body != "" {
			_, _ = fmt.Fprint(w, body)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &count
}

const pydanticExtraTypesPyPIBody = `{"info":{"name":"pydantic-extra-types","version":"2.11.1","yanked":false}}`

func TestFetchLatestRelease_PyPIHint_Applied(t *testing.T) {
	depsDevSrv := newDepsDevTestServer(t, "/v3alpha/systems/pypi/packages/pydantic-extra-types", pydanticExtraTypesVersionsJSON)
	pypiSrv, hits := pypiCountingServer(t, pydanticExtraTypesPyPIPath, http.StatusOK, pydanticExtraTypesPyPIBody)

	pypiClient := pypi.NewClient()
	pypiClient.SetBaseURL(pypiSrv.URL)
	pypiClient.SetCacheTTL(0)

	client := newDepsDevClient(depsDevSrv).WithPyPI(pypiClient)

	info, err := client.fetchLatestRelease(context.Background(), "pkg:pypi/pydantic-extra-types@2.11.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.StableVersion.VersionKey.Version != "2.11.1" {
		t.Fatalf("StableVersion=%s, want 2.11.1 (PyPI hint applied)", info.StableVersion.VersionKey.Version)
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("expected exactly 1 PyPI request, got %d", got)
	}
}

func TestFetchLatestRelease_PyPI404_FallsBackToDepsDevDefault(t *testing.T) {
	depsDevSrv := newDepsDevTestServer(t, "/v3alpha/systems/pypi/packages/pydantic-extra-types", pydanticExtraTypesVersionsJSON)
	pypiSrv, hits := pypiCountingServer(t, pydanticExtraTypesPyPIPath, http.StatusNotFound, "")

	pypiClient := pypi.NewClient()
	pypiClient.SetBaseURL(pypiSrv.URL)
	pypiClient.SetCacheTTL(0)

	client := newDepsDevClient(depsDevSrv).WithPyPI(pypiClient)

	info, err := client.fetchLatestRelease(context.Background(), "pkg:pypi/pydantic-extra-types@2.11.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.StableVersion.VersionKey.Version != "2.11.2" {
		t.Fatalf("StableVersion=%s, want 2.11.2 (documented fallback on PyPI 404)", info.StableVersion.VersionKey.Version)
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("expected exactly 1 PyPI request, got %d", got)
	}
}

func TestFetchLatestRelease_PyPI500_FallsBackWithoutError(t *testing.T) {
	depsDevSrv := newDepsDevTestServer(t, "/v3alpha/systems/pypi/packages/pydantic-extra-types", pydanticExtraTypesVersionsJSON)
	pypiSrv, hits := pypiCountingServer(t, pydanticExtraTypesPyPIPath, http.StatusInternalServerError, "")

	pypiClient := pypi.NewClient()
	pypiClient.SetBaseURL(pypiSrv.URL)
	pypiClient.SetCacheTTL(0)
	pypiClient.SetRetryConfig(noRetryConfig())

	client := newDepsDevClient(depsDevSrv).WithPyPI(pypiClient)

	info, err := client.fetchLatestRelease(context.Background(), "pkg:pypi/pydantic-extra-types@2.11.0")
	if err != nil {
		t.Fatalf("fetchLatestRelease must not return an error on a suppressed PyPI failure, got: %v", err)
	}
	if info.StableVersion.VersionKey.Version != "2.11.2" {
		t.Fatalf("StableVersion=%s, want 2.11.2 (fallback on PyPI 500)", info.StableVersion.VersionKey.Version)
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("expected exactly 1 PyPI request, got %d", got)
	}
}

func TestFetchLatestRelease_PyPIYanked_HintRejected(t *testing.T) {
	depsDevSrv := newDepsDevTestServer(t, "/v3alpha/systems/pypi/packages/pydantic-extra-types", pydanticExtraTypesVersionsJSON)
	pypiSrv, hits := pypiCountingServer(t, pydanticExtraTypesPyPIPath, http.StatusOK, `{"info":{"name":"pydantic-extra-types","version":"2.11.1","yanked":true}}`)

	pypiClient := pypi.NewClient()
	pypiClient.SetBaseURL(pypiSrv.URL)
	pypiClient.SetCacheTTL(0)

	client := newDepsDevClient(depsDevSrv).WithPyPI(pypiClient)

	info, err := client.fetchLatestRelease(context.Background(), "pkg:pypi/pydantic-extra-types@2.11.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.StableVersion.VersionKey.Version != "2.11.2" {
		t.Fatalf("StableVersion=%s, want 2.11.2 (a yanked info.version cannot bound anything)", info.StableVersion.VersionKey.Version)
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("expected exactly 1 PyPI request, got %d", got)
	}
}

func TestFetchLatestRelease_NoPyPIClientWired_SkipsPyPIEntirely(t *testing.T) {
	depsDevSrv := newDepsDevTestServer(t, "/v3alpha/systems/pypi/packages/pydantic-extra-types", pydanticExtraTypesVersionsJSON)

	var contacted int32
	pypiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&contacted, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, pydanticExtraTypesPyPIBody)
	}))
	defer pypiSrv.Close()
	// Intentionally never wired via WithPyPI — c.pypi stays nil.

	client := newDepsDevClient(depsDevSrv)

	info, err := client.fetchLatestRelease(context.Background(), "pkg:pypi/pydantic-extra-types@2.11.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.StableVersion.VersionKey.Version != "2.11.2" {
		t.Fatalf("StableVersion=%s, want 2.11.2 (no PyPI client wired -> deps.dev rules)", info.StableVersion.VersionKey.Version)
	}
	if got := atomic.LoadInt32(&contacted); got != 0 {
		t.Fatalf("PyPI server must never be contacted when c.pypi is nil, got %d requests", got)
	}
}

func TestFetchLatestRelease_NonPyPIPURL_PyPINeverContacted(t *testing.T) {
	depsDevSrv := newDepsDevTestServer(t, "/v3alpha/systems/npm/packages/left-pad", leftPadVersionsJSON)

	var contacted int32
	pypiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&contacted, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, pydanticExtraTypesPyPIBody)
	}))
	defer pypiSrv.Close()

	pypiClient := pypi.NewClient()
	pypiClient.SetBaseURL(pypiSrv.URL)
	pypiClient.SetCacheTTL(0)

	client := newDepsDevClient(depsDevSrv).WithPyPI(pypiClient)

	info, err := client.fetchLatestRelease(context.Background(), "pkg:npm/left-pad@1.3.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.StableVersion.VersionKey.Version != "1.3.0" {
		t.Fatalf("StableVersion=%s, want 1.3.0", info.StableVersion.VersionKey.Version)
	}
	if got := atomic.LoadInt32(&contacted); got != 0 {
		t.Fatalf("PyPI must receive zero requests for a non-pypi PURL, got %d requests", got)
	}
}

// TestRegistryStableVersion_ContextCancelled_PropagatesCanceled and
// TestRegistryStableVersion_ContextDeadlineExceeded_PropagatesDeadlineExceeded
// unit-test registryStableVersion directly (same package) to isolate the
// context-propagation contract from deps.dev's own request timing.
func TestRegistryStableVersion_ContextCancelled_PropagatesCanceled(t *testing.T) {
	pypiSrv, hits := pypiCountingServer(t, pydanticExtraTypesPyPIPath, http.StatusOK, pydanticExtraTypesPyPIBody)
	pypiClient := pypi.NewClient()
	pypiClient.SetBaseURL(pypiSrv.URL)
	pypiClient.SetCacheTTL(0)

	client := (&DepsDevClient{}).WithPyPI(pypiClient)

	parsed, err := purlpkgToParsed("pkg:pypi/pydantic-extra-types@2.11.0")
	if err != nil {
		t.Fatalf("failed to parse test PURL: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	hint, err := client.registryStableVersion(ctx, parsed)
	if err == nil {
		t.Fatalf("expected an error propagated from a cancelled context, got hint=%q", hint)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errors.Is(err, context.Canceled) = false; err = %v", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err must not also match context.DeadlineExceeded: %v", err)
	}
	if got := atomic.LoadInt32(hits); got != 0 {
		t.Fatalf("expected zero PyPI requests when the context is already cancelled, got %d", got)
	}
}

func TestRegistryStableVersion_ContextDeadlineExceeded_PropagatesDeadlineExceeded(t *testing.T) {
	pypiSrv, hits := pypiCountingServer(t, pydanticExtraTypesPyPIPath, http.StatusOK, pydanticExtraTypesPyPIBody)
	pypiClient := pypi.NewClient()
	pypiClient.SetBaseURL(pypiSrv.URL)
	pypiClient.SetCacheTTL(0)

	client := (&DepsDevClient{}).WithPyPI(pypiClient)

	parsed, err := purlpkgToParsed("pkg:pypi/pydantic-extra-types@2.11.0")
	if err != nil {
		t.Fatalf("failed to parse test PURL: %v", err)
	}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Hour))
	defer cancel()

	hint, err := client.registryStableVersion(ctx, parsed)
	if err == nil {
		t.Fatalf("expected an error propagated from an already-expired deadline, got hint=%q", hint)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("errors.Is(err, context.DeadlineExceeded) = false; err = %v", err)
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("err must not also match context.Canceled: %v", err)
	}
	if got := atomic.LoadInt32(hits); got != 0 {
		t.Fatalf("expected zero PyPI requests when the deadline has already passed, got %d", got)
	}
}

// TestFetchLatestRelease_ContextCancelled_PropagatesError confirms the
// end-to-end fetch path (deps.dev call + PyPI hint call) does not silently
// fall back to a partial result when the caller's context is already dead —
// it returns an error the caller can distinguish from "no hint available".
func TestFetchLatestRelease_ContextCancelled_PropagatesError(t *testing.T) {
	depsDevSrv := newDepsDevTestServer(t, "/v3alpha/systems/pypi/packages/pydantic-extra-types", pydanticExtraTypesVersionsJSON)
	pypiSrv, _ := pypiCountingServer(t, pydanticExtraTypesPyPIPath, http.StatusOK, pydanticExtraTypesPyPIBody)

	pypiClient := pypi.NewClient()
	pypiClient.SetBaseURL(pypiSrv.URL)
	pypiClient.SetCacheTTL(0)

	client := newDepsDevClient(depsDevSrv).WithPyPI(pypiClient)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	info, err := client.fetchLatestRelease(ctx, "pkg:pypi/pydantic-extra-types@2.11.0")
	if err == nil {
		t.Fatalf("expected an error from fetchLatestRelease with an already-cancelled context, got info=%+v", info)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errors.Is(err, context.Canceled) = false; err = %v", err)
	}
}

// TestFetchReleaseInfoBatch_DuplicateAndCaseVariantPyPIPURLs_ConcurrentMiss
// runs several PURLs for the SAME PyPI package (an exact duplicate string
// and a differently-cased spelling that packageurl-go normalizes to the same
// lowercase name) through the batch entry point, WITHOUT pre-warming the
// PyPI project cache first.
//
// This deliberately exercises the concurrent cache-MISS path:
// fetchReleaseInfoBatch fans the (deduplicated-by-map-key) PURLs out to
// concurrent goroutines via collectBounded, and every goroutine calls
// pypiClient.GetProject("pydantic-extra-types") at roughly the same time.
// ttlcache has no singleflight/coalescing, so more than one goroutine can
// observe a cache miss and issue its own HTTP request before any of them
// populates the cache — the exact PyPI request count is therefore NOT
// asserted as a fixed number (asserting "1" here would be aspirational, not
// a property the implementation guarantees). What IS guaranteed, and is
// asserted below, is that every request that does land hits the exact
// expected path/method (a wrong package name would 404 and surface as a
// missing/empty StableVersion) and that every result — keyed by the two
// distinct PURL strings the input reduces to — carries the correct,
// PyPI-bounded Stable version.
func TestFetchReleaseInfoBatch_DuplicateAndCaseVariantPyPIPURLs_ConcurrentMiss(t *testing.T) {
	depsDevSrv := newDepsDevTestServer(t, "/v3alpha/systems/pypi/packages/pydantic-extra-types", pydanticExtraTypesVersionsJSON)
	pypiSrv, hits := pypiCountingServer(t, pydanticExtraTypesPyPIPath, http.StatusOK, pydanticExtraTypesPyPIBody)

	pypiClient := pypi.NewClient()
	pypiClient.SetBaseURL(pypiSrv.URL)
	pypiClient.SetCacheTTL(10 * time.Minute)

	client := newDepsDevClient(depsDevSrv).WithPyPI(pypiClient)

	const dupPURL = "pkg:pypi/pydantic-extra-types@2.11.0"
	const caseVariantPURL = "pkg:pypi/Pydantic-Extra-Types@2.11.1"
	purls := []string{
		dupPURL,
		dupPURL,         // exact duplicate: collapses to the same result map key
		caseVariantPURL, // case-variant: distinct map key, same normalized pypi name
	}

	results, err := client.fetchReleaseInfoBatch(context.Background(), purls)
	if err != nil {
		t.Fatalf("fetchReleaseInfoBatch failed: %v", err)
	}

	// Two distinct input PURL strings -> two result map keys, regardless of
	// how many times the duplicate was repeated in the input slice.
	if len(results) != 2 {
		t.Fatalf("len(results)=%d, want 2 (dupPURL and caseVariantPURL each keyed once), got keys: %v", len(results), resultKeys(results))
	}

	for _, want := range []string{dupPURL, caseVariantPURL} {
		info, ok := results[want]
		if !ok {
			t.Fatalf("missing result for purl=%s (got keys: %v)", want, resultKeys(results))
		}
		if info.StableVersion.VersionKey.Version != "2.11.1" {
			t.Errorf("purl=%s StableVersion=%s, want 2.11.1", want, info.StableVersion.VersionKey.Version)
		}
	}

	// At least one PyPI request must have landed at the correct path; more
	// than one is an accepted (not asserted-against) consequence of the
	// cache having no singleflight coalescing.
	if got := atomic.LoadInt32(hits); got < 1 {
		t.Fatalf("expected at least 1 PyPI request, got %d", got)
	}
}

// resultKeys is a small test helper for producing a stable-enough failure
// message listing which PURLs ended up in a fetchReleaseInfoBatch result map.
func resultKeys(results map[string]ReleaseInfo) []string {
	keys := make([]string, 0, len(results))
	for k := range results {
		keys = append(keys, k)
	}
	return keys
}

// TestFetchLatestRelease_PyPIMalformedJSON_FallsBackWithoutError covers the
// PyPI response body being malformed JSON: GetProject's decode fails with a
// plain (non-context) error, which registryStableVersion suppresses per
// ADR-0023, falling back to the deps.dev isDefault rule.
func TestFetchLatestRelease_PyPIMalformedJSON_FallsBackWithoutError(t *testing.T) {
	depsDevSrv := newDepsDevTestServer(t, "/v3alpha/systems/pypi/packages/pydantic-extra-types", pydanticExtraTypesVersionsJSON)
	pypiSrv, hits := pypiCountingServer(t, pydanticExtraTypesPyPIPath, http.StatusOK, `{"info": not valid json`)

	pypiClient := pypi.NewClient()
	pypiClient.SetBaseURL(pypiSrv.URL)
	pypiClient.SetCacheTTL(0)

	client := newDepsDevClient(depsDevSrv).WithPyPI(pypiClient)

	info, err := client.fetchLatestRelease(context.Background(), "pkg:pypi/pydantic-extra-types@2.11.0")
	if err != nil {
		t.Fatalf("fetchLatestRelease must not return an error on a suppressed PyPI decode failure, got: %v", err)
	}
	if info.StableVersion.VersionKey.Version != "2.11.2" {
		t.Fatalf("StableVersion=%s, want 2.11.2 (fallback on malformed PyPI JSON)", info.StableVersion.VersionKey.Version)
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("expected exactly 1 PyPI request, got %d", got)
	}
}

// TestFetchLatestRelease_PyPIEmptyInfoObject_FallsBackWithoutError covers a
// syntactically valid PyPI response whose "info" object is empty: decoding
// succeeds, but info.Version is "", which registryStableVersion treats as
// "no usable hint" (found=true, but info.Version == "" short-circuits before
// the yanked check) and falls back to the deps.dev isDefault rule.
func TestFetchLatestRelease_PyPIEmptyInfoObject_FallsBackWithoutError(t *testing.T) {
	depsDevSrv := newDepsDevTestServer(t, "/v3alpha/systems/pypi/packages/pydantic-extra-types", pydanticExtraTypesVersionsJSON)
	pypiSrv, hits := pypiCountingServer(t, pydanticExtraTypesPyPIPath, http.StatusOK, `{"info":{}}`)

	pypiClient := pypi.NewClient()
	pypiClient.SetBaseURL(pypiSrv.URL)
	pypiClient.SetCacheTTL(0)

	client := newDepsDevClient(depsDevSrv).WithPyPI(pypiClient)

	info, err := client.fetchLatestRelease(context.Background(), "pkg:pypi/pydantic-extra-types@2.11.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.StableVersion.VersionKey.Version != "2.11.2" {
		t.Fatalf("StableVersion=%s, want 2.11.2 (empty info.version is not a usable hint)", info.StableVersion.VersionKey.Version)
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("expected exactly 1 PyPI request, got %d", got)
	}
}

// racedCancelContext wraps a live, working context but reports itself as
// already cancelled through Err(), while its Done() channel is the
// embedded context's — never closed here. This deterministically simulates
// the race ADR-0023 calls out: "a registry failure that happens to race an
// unrelated cancellation must stay a suppressed registry failure." A real
// concurrent context.CancelFunc call racing an in-flight HTTP round trip is
// inherently non-deterministic (net/http may or may not abort the request
// depending on exact timing), which would make a test built on real
// concurrent cancellation flaky. Overriding only Err() reproduces the
// observable symptom — ctx.Err() != nil at classification time — without
// touching Done(), so the underlying HTTP call is never actually aborted and
// the PyPI server's 500 response is delivered normally.
//
// This directly targets the ADR-0023 fix: registryStableVersion must
// classify from the returned error value with errors.Is, not from
// ctx.Err() != nil. A regression to the old ctx.Err()-based check would
// misclassify this plain HTTP failure as a cancellation and propagate it as
// an error instead of suppressing it.
type racedCancelContext struct {
	context.Context
}

func (racedCancelContext) Err() error { return context.Canceled }

// TestRegistryStableVersion_HTTPErrorRacesUnrelatedCancellation_Suppressed
// unit-tests registryStableVersion directly: PyPI returns HTTP 500 (a plain,
// non-context error) while ctx.Err() independently reports Canceled. The
// hint must be suppressed to ("", nil), not propagated as a cancellation
// error.
func TestRegistryStableVersion_HTTPErrorRacesUnrelatedCancellation_Suppressed(t *testing.T) {
	pypiSrv, hits := pypiCountingServer(t, pydanticExtraTypesPyPIPath, http.StatusInternalServerError, "")
	pypiClient := pypi.NewClient()
	pypiClient.SetBaseURL(pypiSrv.URL)
	pypiClient.SetCacheTTL(0)
	pypiClient.SetRetryConfig(noRetryConfig())

	client := (&DepsDevClient{}).WithPyPI(pypiClient)

	parsed, err := purlpkgToParsed("pkg:pypi/pydantic-extra-types@2.11.0")
	if err != nil {
		t.Fatalf("failed to parse test PURL: %v", err)
	}

	ctx := racedCancelContext{Context: context.Background()}

	hint, err := client.registryStableVersion(ctx, parsed)
	if err != nil {
		t.Fatalf("registryStableVersion must suppress a plain HTTP failure even when ctx.Err() != nil, got: %v", err)
	}
	if hint != "" {
		t.Fatalf("hint=%q, want empty (bound does not apply on a suppressed registry failure)", hint)
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("expected exactly 1 PyPI request, got %d", got)
	}
}

// TestFetchLatestRelease_HTTPErrorRacesUnrelatedCancellation_Suppressed is
// the end-to-end counterpart: the same racedCancelContext is threaded
// through fetchLatestRelease (deps.dev call + PyPI hint call), and the
// suppressed PyPI 500 must leave Stable on the deps.dev isDefault fallback,
// with no error returned to the caller.
func TestFetchLatestRelease_HTTPErrorRacesUnrelatedCancellation_Suppressed(t *testing.T) {
	depsDevSrv := newDepsDevTestServer(t, "/v3alpha/systems/pypi/packages/pydantic-extra-types", pydanticExtraTypesVersionsJSON)
	pypiSrv, hits := pypiCountingServer(t, pydanticExtraTypesPyPIPath, http.StatusInternalServerError, "")

	pypiClient := pypi.NewClient()
	pypiClient.SetBaseURL(pypiSrv.URL)
	pypiClient.SetCacheTTL(0)
	pypiClient.SetRetryConfig(noRetryConfig())

	client := newDepsDevClient(depsDevSrv).WithPyPI(pypiClient)

	ctx := racedCancelContext{Context: context.Background()}

	info, err := client.fetchLatestRelease(ctx, "pkg:pypi/pydantic-extra-types@2.11.0")
	if err != nil {
		t.Fatalf("fetchLatestRelease must not propagate a cancellation error for a suppressed PyPI failure, got: %v", err)
	}
	if info.StableVersion.VersionKey.Version != "2.11.2" {
		t.Fatalf("StableVersion=%s, want 2.11.2 (fallback on suppressed PyPI 500 despite ctx.Err() != nil)", info.StableVersion.VersionKey.Version)
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("expected exactly 1 PyPI request, got %d", got)
	}
}

// noRetryConfig returns a retry configuration that never retries, so a
// deliberately-500ing test server is hit exactly once.
func noRetryConfig() httpclient.RetryConfig {
	return httpclient.RetryConfig{MaxRetries: 0}
}
