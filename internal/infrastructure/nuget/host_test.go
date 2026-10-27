package nuget

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type redirectToDecoyTransport struct{}

func (redirectToDecoyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Hostname() == "aka.ms" {
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://notgithub.com/o/r"}}, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
}

func TestResolveRepoURLHeuristicsRejectsDecoyRedirect(t *testing.T) {
	c := NewClient()
	c.SetHTTPClient(&http.Client{Transport: redirectToDecoyTransport{}})
	if got := c.resolveRepoURLHeuristics(context.Background(), "https://aka.ms/repo"); got != "" {
		t.Fatalf("resolveRepoURLHeuristics = %q, want empty", got)
	}
}
