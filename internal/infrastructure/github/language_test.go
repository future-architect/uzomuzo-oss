package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

func TestPrimaryLanguageName(t *testing.T) {
	tests := []struct {
		name string
		in   *PrimaryLanguage
		want string
	}{
		{name: "nil_returns_empty", in: nil, want: ""},
		{name: "blank_returns_empty", in: &PrimaryLanguage{Name: "  "}, want: ""},
		{name: "trims_whitespace", in: &PrimaryLanguage{Name: " Go "}, want: "Go"},
		{name: "passes_through", in: &PrimaryLanguage{Name: "Python"}, want: "Python"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := primaryLanguageName(tt.in); got != tt.want {
				t.Errorf("primaryLanguageName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFetchRepositoryStates_PopulatesLanguageWhenPresent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, graphqlResponseBody(graphqlTestRepo{
			Description:     "A library",
			PrimaryLanguage: "Go",
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
	if got.Language != "Go" {
		t.Errorf("Repository.Language = %q, want %q", got.Language, "Go")
	}
}

// TestFetchRepositoryStates_LeavesLanguageEmptyWhenNull verifies that a null
// primaryLanguage (e.g., empty repos, docs-only repos) leaves Language as "".
func TestFetchRepositoryStates_LeavesLanguageEmptyWhenNull(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, graphqlResponseBody(graphqlTestRepo{
			Description: "A docs-only repo",
			// PrimaryLanguage left empty → serialized as null.
		}))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	repoURL := "https://github.com/owner/docs"
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
	if got.Language != "" {
		t.Errorf("Repository.Language = %q, want empty when primaryLanguage is null", got.Language)
	}
}

// TestFetchRepositoryStates_PreservesPreExistingLanguage ensures that an
// already-set Language is not overwritten by GitHub enrichment, mirroring the
// guard used for DefaultBranch and other Repository metadata fields.
func TestFetchRepositoryStates_PreservesPreExistingLanguage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, graphqlResponseBody(graphqlTestRepo{
			Description:     "A library",
			PrimaryLanguage: "Go",
		}))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	repoURL := "https://github.com/owner/repo"
	analyses := map[string]*domain.Analysis{
		repoURL: {
			RepoURL:    repoURL,
			Repository: &domain.Repository{URL: repoURL, Language: "Rust"},
		},
	}

	if err := c.FetchRepositoryStates(context.Background(), analyses); err != nil {
		t.Fatalf("FetchRepositoryStates: %v", err)
	}

	got := analyses[repoURL].Repository
	if got.Language != "Rust" {
		t.Errorf("Repository.Language = %q, want %q (pre-existing value must not be overwritten)", got.Language, "Rust")
	}
}
