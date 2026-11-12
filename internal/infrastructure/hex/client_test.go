package hex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/packages/cowlib" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"meta":{"links":{"Docs":"https://ninenines.eu/docs","Github":"https://github.com/ninenines/cowlib"}},
			"retirements":{"2.7.1":{"reason":"invalid","message":"bad .app"}}}`))
	}))
	defer srv.Close()
	c := NewClient()
	c.SetBaseURL(srv.URL)
	ctx := context.Background()

	if got, err := c.GetRepoURL(ctx, "CowLib"); err != nil || got != "https://github.com/ninenines/cowlib" {
		t.Fatalf("GetRepoURL = %q, %v", got, err)
	}
	if r, err := c.GetRetirement(ctx, "cowlib", "2.7.1"); err != nil || r == nil || r.Reason != "invalid" {
		t.Fatalf("retired release: %+v, %v", r, err)
	}
	if r, err := c.GetRetirement(ctx, "cowlib", "2.21.0"); err != nil || r != nil {
		t.Fatalf("live release: %+v, %v", r, err)
	}
	if got, err := c.GetRepoURL(ctx, "missing"); err != nil || got != "" {
		t.Fatalf("404: %q, %v", got, err)
	}
}

func TestGetStableAdvisories(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"latest_stable_version":"1.5.0","security_advisories":[
			{"id":"EEF-CVE-2026-1","aliases":["CVE-2026-1","GHSA-aaaa"],"cvss_score":8.2,"affected":[">= 1.0.0 and < 2.0.0"]},
			{"id":"GHSA-aaaa","aliases":["CVE-2026-1","EEF-CVE-2026-1"],"cvss_score":8.2,"affected":[">= 1.0.0 and < 2.0.0"]},
			{"id":"GHSA-fixed","aliases":["CVE-2026-2"],"cvss_score":9.8,"affected":["< 1.4.0", ">= 2.0.0 and < 2.1.0"]},
			{"id":"GHSA-gone","aliases":[],"withdrawn_at":"2026-01-01T00:00:00Z","affected":["< 9.0.0"]},
			{"id":"EEF-CVE-2026-3","aliases":["CVE-2026-3"],"cvss_score":2.1,"affected":["< 1.2.0", ">= 1.5.0 and < 1.5.1"]}]}`))
	}))
	defer srv.Close()
	c := NewClient()
	c.SetBaseURL(srv.URL)

	version, advs, err := c.GetStableAdvisories(context.Background(), "pkg")
	if err != nil || version != "1.5.0" {
		t.Fatalf("version = %q, err = %v", version, err)
	}
	var ids []string
	for _, a := range advs {
		ids = append(ids, a.ID)
	}
	// GHSA wins over its EEF alias; fixed ranges and withdrawn records drop out.
	if want := []string{"GHSA-aaaa", "EEF-CVE-2026-3"}; !slices.Equal(ids, want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
}
