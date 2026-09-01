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

// This file exercises the ADR-0023 contract end to end: fetchLatestRelease
// combines deps.dev's versions listing with PyPI's own current-release field
// to bound Stable selection. All servers are httptest-local; nothing here
// touches the network.
//
// Fixture mirrors pkg:pypi/pydantic-extra-types as observed 2026-08-31 (see
// ADR-0023): deps.dev lists 2.11.0/2.11.1/2.11.2 with 2.11.2 isDefault=true,
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
		// Write error intentionally ignored: this is a local httptest response
		// writer, and the test that consumes the (broken) client connection
		// will surface any real failure as a request error instead.
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
			// Write error intentionally ignored: a local httptest response
			// writer; a broken write shows up as a request-level failure on
			// the client side, which the calling test already checks.
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

	pypiClient := newTestPyPIClient(pypiSrv.URL, 0, false)

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

// TestFetchLatestRelease_PyPIFallback_Table covers four distinct PyPI failure
// modes that must all produce the identical, documented fallback: Stable
// stays on the deps.dev isDefault rule (2.11.2), fetchLatestRelease returns no
// error, and PyPI is contacted exactly once. Each row differs only in the
// PyPI response and whether a no-retry client is required to keep the test
// from waiting through real retry backoff:
//   - PyPI 404 (documented fallback, no retry configured for 4xx)
//   - PyPI 500 (RetryOn5xx is on by default, so this row needs the no-retry
//     client, or the deliberately-failing server would be retried)
//   - malformed PyPI JSON body (GetProject's decode fails with a plain,
//     non-context error, suppressed per ADR-0023)
//   - a syntactically valid but empty "info" object (info.Version == "" is
//     "no usable hint", handled before the yanked check)
func TestFetchLatestRelease_PyPIFallback_Table(t *testing.T) {
	tests := []struct {
		name       string
		pypiStatus int
		pypiBody   string
		noRetry    bool
	}{
		{
			name:       "PyPI 404 falls back to deps.dev default",
			pypiStatus: http.StatusNotFound,
			pypiBody:   "",
			noRetry:    false,
		},
		{
			name:       "PyPI 500 falls back without error",
			pypiStatus: http.StatusInternalServerError,
			pypiBody:   "",
			noRetry:    true,
		},
		{
			name:       "malformed PyPI JSON falls back without error",
			pypiStatus: http.StatusOK,
			pypiBody:   `{"info": not valid json`,
			noRetry:    false,
		},
		{
			name:       "empty PyPI info object falls back without error",
			pypiStatus: http.StatusOK,
			pypiBody:   `{"info":{}}`,
			noRetry:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			depsDevSrv := newDepsDevTestServer(t, "/v3alpha/systems/pypi/packages/pydantic-extra-types", pydanticExtraTypesVersionsJSON)
			pypiSrv, hits := pypiCountingServer(t, pydanticExtraTypesPyPIPath, tt.pypiStatus, tt.pypiBody)

			pypiClient := newTestPyPIClient(pypiSrv.URL, 0, tt.noRetry)

			client := newDepsDevClient(depsDevSrv).WithPyPI(pypiClient)

			info, err := client.fetchLatestRelease(context.Background(), "pkg:pypi/pydantic-extra-types@2.11.0")
			if err != nil {
				t.Fatalf("fetchLatestRelease must not return an error on a suppressed PyPI failure, got: %v", err)
			}
			if info.StableVersion.VersionKey.Version != "2.11.2" {
				t.Fatalf("StableVersion=%s, want 2.11.2 (documented fallback to deps.dev isDefault)", info.StableVersion.VersionKey.Version)
			}
			if got := atomic.LoadInt32(hits); got != 1 {
				t.Fatalf("expected exactly 1 PyPI request, got %d", got)
			}
		})
	}
}

func TestFetchLatestRelease_PyPIYanked_HintRejected(t *testing.T) {
	depsDevSrv := newDepsDevTestServer(t, "/v3alpha/systems/pypi/packages/pydantic-extra-types", pydanticExtraTypesVersionsJSON)
	pypiSrv, hits := pypiCountingServer(t, pydanticExtraTypesPyPIPath, http.StatusOK, `{"info":{"name":"pydantic-extra-types","version":"2.11.1","yanked":true}}`)

	pypiClient := newTestPyPIClient(pypiSrv.URL, 0, false)

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
		// Write error intentionally ignored: this handler is only reached if
		// the "must never be contacted" assertion below is about to fail
		// anyway, so a broken write here would not hide a real regression.
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
		// Write error intentionally ignored: this handler is only reached if
		// the "must never be contacted" assertion below is about to fail
		// anyway, so a broken write here would not hide a real regression.
		_, _ = fmt.Fprint(w, pydanticExtraTypesPyPIBody)
	}))
	defer pypiSrv.Close()

	pypiClient := newTestPyPIClient(pypiSrv.URL, 0, false)

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
	pypiClient := newTestPyPIClient(pypiSrv.URL, 0, false)

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
	pypiClient := newTestPyPIClient(pypiSrv.URL, 0, false)

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

	pypiClient := newTestPyPIClient(pypiSrv.URL, 0, false)

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

// TestFetchLatestRelease_CancelledDuringPyPICall_PreservesEndpointAndRequestedVersion
// covers the production change in fetchLatestRelease's registryStableVersion
// error branch: on cancellation, the function must return the ReleaseInfo the
// deps.dev loop had already built (Endpoint and RequestedVersion), with Error
// set on it, rather than a stripped ReleaseInfo{Endpoint, Error} literal.
//
// Unlike TestFetchLatestRelease_ContextCancelled_PropagatesError above (which
// cancels before either call and so never reaches the deps.dev loop that
// populates RequestedVersion), this test needs the deps.dev call to SUCCEED
// and the cancellation to land specifically during the later PyPI call. That
// requires the same ctx to behave differently at two points in one
// synchronous function call, which is only achievable with real timing: a
// goroutine cancels the context after a short delay, while the PyPI server
// deliberately sleeps well past that delay before responding, so the
// cancellation reliably lands mid-PyPI-request. The deps.dev server responds
// immediately (no delay), so it reliably completes before the 20ms mark. The
// margin (20ms cancel vs 200ms PyPI delay, both against a local loopback
// deps.dev round trip) mirrors the timing margin already used by
// TestDo_RateLimitContextCancellationDuringWait in
// internal/infrastructure/httpclient/client_test.go.
func TestFetchLatestRelease_CancelledDuringPyPICall_PreservesEndpointAndRequestedVersion(t *testing.T) {
	depsDevSrv := newDepsDevTestServer(t, "/v3alpha/systems/pypi/packages/pydantic-extra-types", pydanticExtraTypesVersionsJSON)

	var pypiHits int32
	pypiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != pydanticExtraTypesPyPIPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		atomic.AddInt32(&pypiHits, 1)
		time.Sleep(200 * time.Millisecond) // outlasts the 20ms cancellation below
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Write error intentionally ignored: the request is cancelled before
		// this handler returns, so the client never reads this body anyway.
		_, _ = fmt.Fprint(w, pydanticExtraTypesPyPIBody)
	}))
	t.Cleanup(pypiSrv.Close)

	pypiClient := newTestPyPIClient(pypiSrv.URL, 0, true)

	client := newDepsDevClient(depsDevSrv).WithPyPI(pypiClient)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	defer cancel()

	// The requested version (2.11.0) must be present in the deps.dev fixture
	// so the loop in fetchLatestRelease populates RequestedVersion before the
	// PyPI call runs.
	const purlStr = "pkg:pypi/pydantic-extra-types@2.11.0"

	info, err := client.fetchLatestRelease(ctx, purlStr)
	if err == nil {
		t.Fatalf("expected an error from a cancellation mid-PyPI-call, got info=%+v", info)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errors.Is(err, context.Canceled) = false; err = %v", err)
	}
	if info.Error == nil || !errors.Is(info.Error, context.Canceled) {
		t.Fatalf("info.Error = %v, want it to wrap context.Canceled", info.Error)
	}
	if info.Endpoint == "" {
		t.Fatalf("Endpoint was dropped, want the deps.dev endpoint the loop above already resolved")
	}
	if info.RequestedVersion.VersionKey.Version != "2.11.0" {
		t.Fatalf("RequestedVersion=%q, want 2.11.0 (populated by the deps.dev loop before the PyPI call ran)", info.RequestedVersion.VersionKey.Version)
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

	pypiClient := newTestPyPIClient(pypiSrv.URL, 10*time.Minute, false)

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
	pypiClient := newTestPyPIClient(pypiSrv.URL, 0, true)

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

	pypiClient := newTestPyPIClient(pypiSrv.URL, 0, true)

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

// newTestPyPIClient builds a *pypi.Client pointed at baseURL for tests. cacheTTL
// is passed straight through to SetCacheTTL (0 in most tests, so every call
// reaches the test server; TestFetchReleaseInfoBatch_DuplicateAndCaseVariantPyPIPURLs_ConcurrentMiss
// is the one caller that needs a real TTL). noRetry, when true, installs
// noRetryConfig() so a deliberately-failing server (e.g. a 500) is hit exactly
// once instead of being retried — this is the one setting that actually varies
// across call sites, so it is a named parameter rather than folded into a
// fixed setup sequence.
func newTestPyPIClient(baseURL string, cacheTTL time.Duration, noRetry bool) *pypi.Client {
	c := pypi.NewClient()
	c.SetBaseURL(baseURL)
	c.SetCacheTTL(cacheTTL)
	if noRetry {
		c.SetRetryConfig(noRetryConfig())
	}
	return c
}
