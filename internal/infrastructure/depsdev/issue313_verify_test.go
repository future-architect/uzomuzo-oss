package depsdev

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
		"/v3alpha/systems/npm/packages/express/versions/4.21.2:dependencies": relations(1, 28, 39),
		"/v3alpha/systems/npm/packages/react/versions/19.1.0:dependencies":   relations(1, 0, 0),
		"/v3alpha/systems/pypi/packages/requests/versions/2.32.3:dependencies": relations(1, 4, 0),
		"/v3alpha/systems/pypi/packages/django/versions/5.1.0:dependencies":    relations(1, 2, 0),
		"/v3alpha/systems/cargo/packages/serde/versions/1.0.210:dependencies":  relations(1, 0, 0),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := fixtures[r.URL.Path]
		if !ok {
			t.Logf("unexpected request: %s", r.URL.Path)
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

	inputs := []string{
		"pkg:npm/express@4.21.2",
		"pkg:npm/react@19.1.0",
		"pkg:pypi/requests@2.32.3",
		"pkg:pypi/django@5.1.0",
		"pkg:cargo/serde@1.0.210",
	}

	results := client.FetchDependenciesBatch(context.Background(), inputs)

	var b strings.Builder
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| PURL | direct | transitive | HasDependencyGraph |")
	fmt.Fprintln(&b, "|---|---|---|---|")
	for _, in := range inputs {
		key := strings.ToLower(strings.SplitN(in, "@", 2)[0])
		resp, ok := results[key]
		if !ok || resp == nil {
			fmt.Fprintf(&b, "| `%s` | - | - | false (not collected) |\n", in)
			continue
		}
		d, tr := resp.CountByRelation()
		fmt.Fprintf(&b, "| `%s` | %d | %d | true |\n", in, d, tr)
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
