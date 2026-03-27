package nuget

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetDeprecation_FoundEmbedded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/registration5-semver2/test.package/index.json":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			w.Write([]byte(`{
                "items": [
                    {"items": [
                        {"catalogEntry": {"id": "Test.Package"},
                         "deprecation": {"reasons": ["Legacy"], "message": "Use successor", "alternatePackage": {"id": "New.Package", "range": "[1.0,)"}}}
                    ]}
                ]
            }`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL + "/v3/registration5-semver2")

	info, found, err := c.GetDeprecation(context.Background(), "Test.Package")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !found {
		t.Fatalf("expected found=true")
	}
	if info == nil {
		t.Fatalf("expected info")
	}
	if len(info.Reasons) != 1 || info.Reasons[0] != "Legacy" {
		t.Fatalf("unexpected reasons: %#v", info.Reasons)
	}
	if info.AlternatePackageID != "New.Package" {
		t.Fatalf("unexpected alt: %s", info.AlternatePackageID)
	}
}

func TestGetDeprecation_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL + "/v3/registration5-semver2")
	c.SetHTMLBase(srv.URL) // prevent HTML fallback from hitting real nuget.org

	info, found, err := c.GetDeprecation(context.Background(), "NoSuch")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if found {
		t.Fatalf("expected found=false, got true with info=%#v", info)
	}
}

func TestGetDeprecation_CaseInsensitiveID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/registration5-semver2/microsoft.azure.documentdb/index.json":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			w.Write([]byte(`{
				"items": [
					{"items": [
						{"catalogEntry": {"id": "Microsoft.Azure.DocumentDB"},
						 "deprecation": {"reasons": ["Legacy"], "message": "Use Azure.Cosmos", "alternatePackage": {"id": "Azure.Cosmos", "range": "[1.0,)"}}}
					]}
				]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL + "/v3/registration5-semver2")

	info, found, err := c.GetDeprecation(context.Background(), "Microsoft.Azure.DocumentDB")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !found || info == nil {
		t.Fatalf("expected found=true and info not nil")
	}
	if len(info.Reasons) != 1 || info.Reasons[0] != "Legacy" {
		t.Fatalf("unexpected reasons: %#v", info.Reasons)
	}
	if info.AlternatePackageID != "Azure.Cosmos" {
		t.Fatalf("unexpected alt: %s", info.AlternatePackageID)
	}
}

// Test that when NoCacheNotFound is true, a not-found result is not cached
// and a subsequent server change to 200 returns found=true.
func TestGetDeprecation_NoCacheNotFound_AllowsFreshLookup(t *testing.T) {
	// Stage 1: 404 for index
	mux := http.NewServeMux()
	mux.HandleFunc("/v3/registration5-semver2/samplepkg/index.json", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL + "/v3/registration5-semver2")
	c.SetHTMLBase(srv.URL)   // prevent HTML fallback from hitting real nuget.org
	c.SetCacheTTL(time.Hour) // enable caching with a long TTL
	c.NoCacheNotFound = true

	// First attempt: 404 -> found=false
	if _, found, err := c.GetDeprecation(context.Background(), "SamplePkg"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	} else if found {
		t.Fatalf("expected found=false on first 404")
	}

	// Update handler to return a deprecation now
	mux = http.NewServeMux()
	mux.HandleFunc("/v3/registration5-semver2/samplepkg/index.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"items":[{"items": [{"catalogEntry":{"id":"SamplePkg"}, "deprecation": {"reasons":["Legacy"], "message":"msg"}}]}]}`))
	})
	// Replace server handler in place (httptest.Server supports changing Config.Handler)
	srv.Config.Handler = mux

	// Second attempt: should re-fetch (no negative cache) and find deprecation
	if info, found, err := c.GetDeprecation(context.Background(), "SamplePkg"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	} else if !found || info == nil {
		t.Fatalf("expected found=true after server change")
	}
}

// Test retry behavior by failing first request with 500 then succeeding.
func TestGetDeprecation_RetryOn5xxThenSuccess(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v3/registration5-semver2/retrypkg/index.json" {
			calls++
			if calls == 1 {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			w.Write([]byte(`{"items":[{"items": [{"catalogEntry":{"id":"RetryPkg"}, "deprecation": {"reasons":["Other"], "message":"msg"}}]}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL + "/v3/registration5-semver2")

	info, found, err := c.GetDeprecation(context.Background(), "RetryPkg")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !found || info == nil {
		t.Fatalf("expected deprecation after retry path")
	}
}

func TestGetRepoURL_EmbeddedRepositoryObject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/registration5-semver2/sample/index.json":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			w.Write([]byte(`{
				"items": [
					{"items": [
						{"catalogEntry": {"id": "Sample", "repository": {"type":"git", "url": "https://github.com/Owner/Repo"}}}
					]}
				]
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL + "/v3/registration5-semver2")
	url, err := c.GetRepoURL(context.Background(), "Sample", "")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if url != "https://github.com/Owner/Repo" {
		t.Fatalf("got %q, want https://github.com/Owner/Repo", url)
	}
}

func TestGetRepoURL_EmbeddedRepositoryStringAndProjectURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v3/registration5-semver2/sample2/index.json":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			w.Write([]byte(`{"items":[{"items":[{"catalogEntry":{"id":"Sample2","repository":"git+https://github.com/Org/Repo.git","projectUrl":"https://example.com"}}]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL + "/v3/registration5-semver2")
	url, err := c.GetRepoURL(context.Background(), "Sample2", "")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if url != "git+https://github.com/Org/Repo.git" { // normalization is performed later by deps.dev layer
		t.Fatalf("got %q, want raw repo string preserved", url)
	}
}

func TestGetRepoURL_PageFetch_ProjectURLFallback(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v3/registration5-semver2/pkg/index.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		// Provide a full URL for the registration page ID as NuGet does (including scheme)
		w.Write([]byte(`{"items":[{"@id":"http://` + r.Host + `/page1"}]}`))
	})
	mux.HandleFunc("/page1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"items":[{"catalogEntry":{"projectUrl":"https://github.com/acme/repo"}}]}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL + "/v3/registration5-semver2")
	url, err := c.GetRepoURL(context.Background(), "Pkg", "")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if url != "https://github.com/acme/repo" {
		t.Fatalf("got %q, want https://github.com/acme/repo", url)
	}
}

func TestDiscoverRegistrationBases_UsesServiceIndexAndCaches(t *testing.T) {
	// Fake service index advertising semver2 and gz-semver2
	mux := http.NewServeMux()
	mux.HandleFunc("/v3/index.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{
					"resources": [
						{"@id": "` + r.Host + `/v3/registration5-semver2", "@type": "RegistrationsBaseUrl/3.6.0"},
						{"@id": "` + r.Host + `/v3/registration5-gz-semver2", "@type": "RegistrationsBaseUrl/3.6.0"}
					]
				}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient()
	c.SetServiceIndexURL(srv.URL + "/v3/index.json")
	c.SetServiceIndexTTL(1 * time.Minute)

	// First call should fetch and cache
	bases := c.getRegistrationCandidates(context.Background())
	if len(bases) == 0 {
		t.Fatalf("expected discovered bases")
	}

	// Change handler to something wrong, but due to TTL, cached bases should be returned
	srv.Config.Handler = http.NewServeMux()
	bases2 := c.getRegistrationCandidates(context.Background())
	if len(bases2) == 0 {
		t.Fatalf("expected cached bases even after handler change")
	}
}

// When registration index decoding fails, client should try HTML fallback and detect deprecation.
func TestGetDeprecation_HTMLFallback_DeprecatedWithAlternative(t *testing.T) {
	mux := http.NewServeMux()
	// Registration endpoints return 200 but invalid JSON to trigger decode error
	mux.HandleFunc("/v3/registration5-semver2/microsoft.azure.eventhubs/index.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"items": [ INVALID`))
	})
	mux.HandleFunc("/v3/registration5-gz-semver2/microsoft.azure.eventhubs/index.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"items": [ INVALID`))
	})
	// HTML page for nuget.org package with deprecation banner and Suggested Alternatives link
	mux.HandleFunc("/packages/Microsoft.Azure.EventHubs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(200)
		w.Write([]byte(`<!doctype html><html><body>
			<div class="deprecated">This package has been deprecated.</div>
			<h3>Suggested Alternatives</h3>
			<a href="/packages/Azure.Messaging.EventHubs">Azure.Messaging.EventHubs</a>
		</body></html>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient()
	c.SetBaseURL(srv.URL + "/v3/registration5-semver2")
	c.SetHTMLBase(srv.URL)

	info, found, err := c.GetDeprecation(context.Background(), "Microsoft.Azure.EventHubs")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !found || info == nil {
		t.Fatalf("expected found=true via HTML fallback")
	}
	if info.AlternatePackageID != "Azure.Messaging.EventHubs" {
		t.Fatalf("alt id = %q, want Azure.Messaging.EventHubs", info.AlternatePackageID)
	}
}
