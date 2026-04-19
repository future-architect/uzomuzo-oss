package depsdev

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	commonpurl "github.com/future-architect/uzomuzo-oss/internal/common/purl"
	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

// TestIssue313Reproduction mocks deps.dev at the HTTP layer and runs the exact
// five PURLs from issue #313 through FetchDependenciesBatch + CountByRelation,
// then renders the same table the bug reporter produced. This is a temporary
// verification test for the PR; response shapes mirror what deps.dev returns
// for each PURL (derived from the upstream package manifests at the time of
// writing).
func TestIssue313Reproduction(t *testing.T) {
	fixtures := map[string]string{
		"/v3alpha/systems/npm/packages/express/versions/4.21.2:dependencies":   relations(1, 28, 39),
		"/v3alpha/systems/npm/packages/react/versions/19.1.0:dependencies":     relations(1, 0, 0),
		"/v3alpha/systems/pypi/packages/requests/versions/2.32.3:dependencies": relations(1, 4, 0),
		"/v3alpha/systems/pypi/packages/django/versions/5.1.0:dependencies":    relations(1, 2, 0),
		"/v3alpha/systems/cargo/packages/serde/versions/1.0.210:dependencies":  relations(1, 0, 0),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := fixtures[r.URL.Path]
		if !ok {
			// t.Errorf is goroutine-safe; t.Fatalf is not (handler runs outside the test goroutine).
			t.Errorf("unexpected request path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	client := NewDepsDevClient(&config.DepsDevConfig{
		BaseURL:    srv.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 0,
		BatchSize:  10,
	})

	testCases := []struct {
		purl           string
		wantDirect     int
		wantTransitive int
	}{
		{purl: "pkg:npm/express@4.21.2", wantDirect: 28, wantTransitive: 39},
		{purl: "pkg:npm/react@19.1.0", wantDirect: 0, wantTransitive: 0},
		{purl: "pkg:pypi/requests@2.32.3", wantDirect: 4, wantTransitive: 0},
		{purl: "pkg:pypi/django@5.1.0", wantDirect: 2, wantTransitive: 0},
		{purl: "pkg:cargo/serde@1.0.210", wantDirect: 0, wantTransitive: 0},
	}

	inputs := make([]string, 0, len(testCases))
	for _, tc := range testCases {
		inputs = append(inputs, tc.purl)
	}

	results := client.FetchDependenciesBatch(context.Background(), inputs)

	var b strings.Builder
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| PURL | direct | transitive | HasDependencyGraph |")
	fmt.Fprintln(&b, "|---|---|---|---|")
	for _, tc := range testCases {
		key := commonpurl.CanonicalKey(tc.purl)
		resp, ok := results[key]
		if !ok {
			fmt.Fprintf(&b, "| `%s` | - | - | false (missing response entry) |\n", tc.purl)
			t.Errorf("missing response for input %q (lookup key %q)", tc.purl, key)
			continue
		}
		if resp == nil {
			fmt.Fprintf(&b, "| `%s` | - | - | false (nil response) |\n", tc.purl)
			t.Errorf("nil response for input %q (lookup key %q)", tc.purl, key)
			continue
		}

		d, tr := resp.CountByRelation()
		fmt.Fprintf(&b, "| `%s` | %d | %d | true |\n", tc.purl, d, tr)

		if d != tc.wantDirect || tr != tc.wantTransitive {
			t.Errorf(
				"unexpected dependency counts for %q: got direct=%d transitive=%d, want direct=%d transitive=%d",
				tc.purl, d, tr, tc.wantDirect, tc.wantTransitive,
			)
		}
	}
	t.Log(b.String())
}

// relations renders a deps.dev :dependencies payload with the given counts
// of SELF / DIRECT / INDIRECT nodes. Edges are omitted because CountByRelation
// does not use them.
func relations(self, direct, indirect int) string {
	var nodes []string
	for i := 0; i < self; i++ {
		nodes = append(nodes, `{"relation":"SELF"}`)
	}
	for i := 0; i < direct; i++ {
		nodes = append(nodes, `{"relation":"DIRECT"}`)
	}
	for i := 0; i < indirect; i++ {
		nodes = append(nodes, `{"relation":"INDIRECT"}`)
	}
	return `{"nodes":[` + strings.Join(nodes, ",") + `],"edges":[]}`
}
