package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/rdap"
)

func cachedFixture(t *testing.T) string {
	t.Helper()
	cacheDir := t.TempDir()
	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "infrastructure", "npmjs", "testdata", "node-ipc-maintainers.json"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(cacheDir, "node-ipc.json.gz"))
	if err != nil {
		t.Fatal(err)
	}
	zw := gzip.NewWriter(f)
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return cacheDir
}

func TestIncidentUsesPrecedingManifestAndFirstAppearance(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"events":[{"eventAction":"registration","eventDate":"2026-05-07T11:49:33Z"}]}`))
	}))
	t.Cleanup(srv.Close)
	cacheDir := cachedFixture(t)
	client := rdap.NewClient()
	client.SetBaseURL("net", srv.URL)
	c := &command{packumentDir: cacheDir, rdap: client}
	r := c.incidentRow(context.Background(), "population", "node-ipc", []string{"12.0.1"})
	if r.missing != "" || len(r.findings) != 1 || r.findings[0].Kind != analysis.MaintainerDomainReregisteredBefore || !r.findings[0].FirstSeen.Equal(time.Date(2020, 11, 11, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("row=%+v", r)
	}
}

func TestLockfileCountsUniqueNamesAndNotices(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"events":[{"eventAction":"registration","eventDate":"2026-05-07T11:49:33Z"}]}`))
	}))
	t.Cleanup(srv.Close)
	client := rdap.NewClient()
	client.SetBaseURL("net", srv.URL)
	c := &command{packumentDir: cachedFixture(t), rdap: client}
	lockPath := filepath.Join(t.TempDir(), "package-lock.json")
	const lock = `{"packages":{"node_modules/node-ipc":{"version":"12.0.0"},"node_modules/example/node_modules/node-ipc":{"version":"12.0.0"},"node_modules/linked":{"version":"1.0.0","link":true}}}`
	if err := os.WriteFile(lockPath, []byte(lock), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	if err := c.lockfile(context.Background(), lockPath, now, "lockfile_1"); err != nil {
		t.Fatal(err)
	}
	if len(c.rows) != 1 || c.rows[0].name != "node-ipc" || len(c.rows[0].findings) != 2 {
		t.Fatalf("rows=%+v", c.rows)
	}
	var summary bytes.Buffer
	if err := printSummary(&summary, c.rows); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"packages=1", "notices=1", "distinct_notice_domains=1", "reregistered_before=1"} {
		if !bytes.Contains(summary.Bytes(), []byte(want)) {
			t.Errorf("summary missing %s: %s", want, summary.String())
		}
	}
	if bytes.Contains(summary.Bytes(), []byte("node-ipc")) {
		t.Errorf("summary contains package name: %s", summary.String())
	}
}

func TestMarkWaves(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	var rows []row
	for i := 0; i < 5; i++ {
		rows = append(rows, row{scope: "population", at: start})
	}
	for i := 0; i < 5; i++ {
		rows = append(rows, row{scope: "population", at: start.AddDate(0, 0, 1)})
	}
	for i := 0; i < 4; i++ {
		rows = append(rows, row{scope: "population", at: start.AddDate(0, 0, 2)})
	}
	markWaves(rows)
	for i, r := range rows {
		want := "wave"
		if i >= 10 {
			want = "non-wave"
		}
		if r.scope != want {
			t.Errorf("row %d scope=%s, want %s", i, r.scope, want)
		}
	}
}
