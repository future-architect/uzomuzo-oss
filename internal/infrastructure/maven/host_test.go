package maven

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type decoyPageTransport struct{ decoyCalls int }

func (t *decoyPageTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body string
	switch req.URL.Hostname() {
	case "maven.example":
		body = `<project><url>https://notgithub.com/page</url></project>`
	case "notgithub.com":
		t.decoyCalls++
		body = `<html><a href="https://github.com/owner/repo">repository</a></html>`
	default:
		body = ""
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func TestGetRepoURLScrapesDecoyHostPage(t *testing.T) {
	transport := &decoyPageTransport{}
	c := NewClient()
	c.SetBaseURL("https://maven.example")
	c.SetHTTPClient(&http.Client{Transport: transport})
	got, err := c.GetRepoURL(context.Background(), "owner", "repo", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://github.com/owner/repo" || transport.decoyCalls != 1 {
		t.Fatalf("GetRepoURL = %q, decoy page requests = %d", got, transport.decoyCalls)
	}
}

func TestIsGitHubPage(t *testing.T) {
	tests := []struct {
		page string
		want bool
	}{
		{"https://github.com/org", true},
		{"github.com/org", true},
		{"git@github.com:o/r.git", true},
		{"https://www.github.com:443/org", true},
		{"https://notgithub.com/page", false},
		{"https://api.github.com/org", false},
		{"https://example.com/github.com", false},
	}
	for _, tc := range tests {
		t.Run(tc.page, func(t *testing.T) {
			if got := isGitHubPage(tc.page); got != tc.want {
				t.Errorf("isGitHubPage(%q) = %v, want %v", tc.page, got, tc.want)
			}
		})
	}
}
