package maven

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/common"
)

func Test_MapApacheHostedToGitHub_Common(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "gitbox_query_param_cgit",
			in:   "https://gitbox.apache.org/repos/asf?p=cxf.git;a=summary",
			want: "https://github.com/apache/cxf",
		},
		{
			name: "gitbox_path_style",
			in:   "https://gitbox.apache.org/repos/asf/cxf.git",
			want: "https://github.com/apache/cxf",
		},
		{
			name: "git_wip_us_path_style",
			in:   "https://git-wip-us.apache.org/repos/asf/commons-lang.git",
			want: "https://github.com/apache/commons-lang",
		},
		{
			name: "non_apache_host",
			in:   "https://example.com/repos/asf/foo.git",
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := common.MapApacheHostedToGitHub(tc.in)
			if got != tc.want {
				t.Fatalf("MapApacheHostedToGitHub(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func Test_normalizeToGitHub_ApacheGitBox(t *testing.T) {
	in := "scm:git:https://gitbox.apache.org/repos/asf/cxf.git"
	want := "https://github.com/apache/cxf"
	if got := normalizeToGitHub(in); got != want {
		t.Fatalf("normalizeToGitHub(%q) = %q, want %q", in, got, want)
	}
}

func TestSearchByArtifactID(t *testing.T) {
	cases := []struct {
		name       string
		artifactID string
		response   string
		statusCode int
		wantGroup  string
		wantFound  bool
		wantErr    bool
	}{
		{
			name:       "single_result",
			artifactID: "jsr250-api",
			response:   `{"response":{"numFound":1,"docs":[{"g":"javax.annotation","a":"jsr250-api"}]}}`,
			wantGroup:  "javax.annotation",
			wantFound:  true,
		},
		{
			name:       "multiple_results_ambiguous",
			artifactID: "commons-discovery",
			response:   `{"response":{"numFound":6,"docs":[{"g":"de.mhus.ports","a":"commons-discovery"},{"g":"org.lucee","a":"commons-discovery"}]}}`,
			wantFound:  false,
		},
		{
			name:       "no_results",
			artifactID: "nonexistent-artifact-xyz",
			response:   `{"response":{"numFound":0,"docs":[]}}`,
			wantFound:  false,
		},
		{
			name:       "empty_artifact_id",
			artifactID: "",
			wantFound:  false,
		},
		{
			name:       "server_error",
			artifactID: "some-artifact",
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.artifactID == "" {
				c := NewClient()
				g, found, err := c.SearchByArtifactID(context.Background(), tc.artifactID)
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if found != tc.wantFound {
					t.Fatalf("found = %v, want %v", found, tc.wantFound)
				}
				if g != tc.wantGroup {
					t.Fatalf("groupID = %q, want %q", g, tc.wantGroup)
				}
				return
			}

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.statusCode != 0 {
					w.WriteHeader(tc.statusCode)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, tc.response)
			}))
			defer srv.Close()

			c := NewClient()
			c.SetSearchBaseURL(srv.URL)
			c.searchHTTP = c.http // reuse same http client for test

			g, found, err := c.SearchByArtifactID(context.Background(), tc.artifactID)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if found != tc.wantFound {
				t.Fatalf("found = %v, want %v", found, tc.wantFound)
			}
			if g != tc.wantGroup {
				t.Fatalf("groupID = %q, want %q", g, tc.wantGroup)
			}
		})
	}
}
