package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/config"
)

func TestCollectTopics(t *testing.T) {
	tests := []struct {
		name string
		in   RepositoryTopicConnection
		want []string
	}{
		{
			name: "empty_connection",
			in:   RepositoryTopicConnection{},
			want: []string{},
		},
		{
			name: "topics_preserve_order",
			in: RepositoryTopicConnection{Nodes: []RepositoryTopicNode{
				{Topic: Topic{Name: "go"}},
				{Topic: Topic{Name: "cli"}},
				{Topic: Topic{Name: "library"}},
			}},
			want: []string{"go", "cli", "library"},
		},
		{
			name: "deduplicates_defensively",
			in: RepositoryTopicConnection{Nodes: []RepositoryTopicNode{
				{Topic: Topic{Name: "go"}},
				{Topic: Topic{Name: "cli"}},
				{Topic: Topic{Name: "go"}}, // duplicate
			}},
			want: []string{"go", "cli"},
		},
		{
			name: "skips_blank_names",
			in: RepositoryTopicConnection{Nodes: []RepositoryTopicNode{
				{Topic: Topic{Name: ""}},
				{Topic: Topic{Name: "   "}},
				{Topic: Topic{Name: "valid"}},
			}},
			want: []string{"valid"},
		},
		{
			name: "caps_at_MaxTopics",
			in: func() RepositoryTopicConnection {
				nodes := make([]RepositoryTopicNode, 0, MaxTopics+5)
				for i := 0; i < MaxTopics+5; i++ {
					nodes = append(nodes, RepositoryTopicNode{Topic: Topic{Name: fmt.Sprintf("t%02d", i)}})
				}
				return RepositoryTopicConnection{Nodes: nodes}
			}(),
			want: func() []string {
				out := make([]string, 0, MaxTopics)
				for i := 0; i < MaxTopics; i++ {
					out = append(out, fmt.Sprintf("t%02d", i))
				}
				return out
			}(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collectTopics(tt.in)
			if got == nil {
				t.Fatalf("collectTopics must never return nil; got nil")
			}
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d (%v), want %d (%v)", len(got), got, len(tt.want), tt.want)
			}
			for i, v := range tt.want {
				if got[i] != v {
					t.Errorf("topics[%d] = %q, want %q", i, got[i], v)
				}
			}
		})
	}
}

// newTestClient builds a Client whose REST and GraphQL endpoints both target the
// given httptest server. Sets cfg.GitHub.BaseURL so the same configuration knob
// drives both code paths.
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	cfg := &config.Config{GitHub: config.GetDefaultGitHub()}
	cfg.GitHub.Token = "test-token"
	cfg.GitHub.Timeout = 5 * time.Second
	cfg.GitHub.BaseURL = baseURL
	return NewClient(cfg)
}

// graphqlResponse encodes a minimal GraphQL response carrying the fields exercised
// by the topics tests.
type graphqlTestRepo struct {
	Description string
	Topics      []string
}

func graphqlResponseBody(repo graphqlTestRepo) string {
	topicNodes := ""
	for i, name := range repo.Topics {
		if i > 0 {
			topicNodes += ","
		}
		topicNodes += fmt.Sprintf(`{"topic":{"name":%q}}`, name)
	}
	return fmt.Sprintf(`{
	  "data": {
	    "repository": {
	      "isArchived": false,
	      "isDisabled": false,
	      "isFork": false,
	      "stargazerCount": 1,
	      "forkCount": 1,
	      "description": %q,
	      "homepageUrl": "",
	      "licenseInfo": null,
	      "repositoryTopics": {"nodes": [%s]},
	      "parent": null,
	      "defaultBranchRef": {"name":"main","target":{"history":{"nodes":[]}}}
	    },
	    "rateLimit": {"cost":1,"remaining":4999,"resetAt":"2099-01-01T00:00:00Z"}
	  }
	}`, repo.Description, topicNodes)
}

func TestFetchRepositoryStates_PopulatesTopicsWhenPresent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, graphqlResponseBody(graphqlTestRepo{
			Description: "A library",
			Topics:      []string{"go", "cli", "library"},
		}))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	repoURL := "https://github.com/owner/repo"
	analyses := map[string]*domain.Analysis{
		repoURL: {RepoURL: repoURL},
	}

	if err := c.FetchRepositoryStates(context.Background(), analyses); err != nil {
		t.Fatalf("FetchRepositoryStates: %v", err)
	}

	got := analyses[repoURL].Repository
	if got == nil {
		t.Fatalf("expected Repository populated")
	}
	if got.Topics == nil {
		t.Fatalf("Topics is nil; want non-nil slice")
	}
	want := []string{"go", "cli", "library"}
	if len(got.Topics) != len(want) {
		t.Fatalf("Topics = %v, want %v", got.Topics, want)
	}
	for i, v := range want {
		if got.Topics[i] != v {
			t.Errorf("Topics[%d] = %q, want %q", i, got.Topics[i], v)
		}
	}
	// Summary derived from description should also be populated.
	if got.Summary != "A library" {
		t.Errorf("Summary = %q, want %q", got.Summary, "A library")
	}
}

func TestFetchRepositoryStates_PopulatesEmptySliceWhenNoTopics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, graphqlResponseBody(graphqlTestRepo{
			Description: "A quiet repo",
			Topics:      nil,
		}))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	repoURL := "https://github.com/owner/quiet"
	analyses := map[string]*domain.Analysis{
		repoURL: {RepoURL: repoURL},
	}

	if err := c.FetchRepositoryStates(context.Background(), analyses); err != nil {
		t.Fatalf("FetchRepositoryStates: %v", err)
	}

	got := analyses[repoURL].Repository
	if got == nil {
		t.Fatalf("expected Repository populated")
	}
	if got.Topics == nil {
		t.Fatalf("Topics is nil; want []string{} sentinel for fetched-zero-topics")
	}
	if len(got.Topics) != 0 {
		t.Fatalf("Topics = %v, want empty slice", got.Topics)
	}
}

// TestFetchRepositoryStates_NonGitHubLeavesTopicsNil verifies that analyses with
// non-GitHub RepoURL do not get Topics populated (nil sentinel preserved).
func TestFetchRepositoryStates_NonGitHubLeavesTopicsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("non-GitHub repos must not trigger GraphQL request to %s", r.URL.String())
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	repoURL := "https://gitlab.com/owner/repo"
	analyses := map[string]*domain.Analysis{
		repoURL: {RepoURL: repoURL},
	}

	if err := c.FetchRepositoryStates(context.Background(), analyses); err != nil {
		t.Fatalf("FetchRepositoryStates: %v", err)
	}

	a := analyses[repoURL]
	if a.Repository != nil && a.Repository.Topics != nil {
		t.Errorf("Topics = %v for non-GitHub repo; want nil sentinel", a.Repository.Topics)
	}
}

// TestFetchRepositoryStates_NoTokenLeavesTopicsNil ensures the no-token early-return
// path does not set Topics, preserving the "not fetched" semantics.
func TestFetchRepositoryStates_NoTokenLeavesTopicsNil(t *testing.T) {
	cfg := &config.Config{GitHub: config.GetDefaultGitHub()}
	cfg.GitHub.Token = "" // explicitly no token
	c := NewClient(cfg)

	repoURL := "https://github.com/owner/repo"
	analyses := map[string]*domain.Analysis{repoURL: {RepoURL: repoURL}}

	if err := c.FetchRepositoryStates(context.Background(), analyses); err != nil {
		t.Fatalf("FetchRepositoryStates: %v", err)
	}

	a := analyses[repoURL]
	if a.Repository != nil && a.Repository.Topics != nil {
		t.Errorf("Topics = %v when token unavailable; want nil", a.Repository.Topics)
	}
}

// TestFetchRepositoryStates_NotFoundLeavesTopicsNil simulates a private/not-found
// repository (GraphQL returns "Could not resolve to a Repository") and verifies
// Topics stays nil.
func TestFetchRepositoryStates_NotFoundLeavesTopicsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"data":{},"errors":[{"message":"Could not resolve to a Repository with the name 'owner/private'."}]}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	repoURL := "https://github.com/owner/private"
	analyses := map[string]*domain.Analysis{repoURL: {RepoURL: repoURL}}

	if err := c.FetchRepositoryStates(context.Background(), analyses); err != nil {
		t.Fatalf("FetchRepositoryStates: %v", err)
	}

	a := analyses[repoURL]
	if a.Repository != nil && a.Repository.Topics != nil {
		t.Errorf("Topics = %v on not-found error; want nil", a.Repository.Topics)
	}
}
