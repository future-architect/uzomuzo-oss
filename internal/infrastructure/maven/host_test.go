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
