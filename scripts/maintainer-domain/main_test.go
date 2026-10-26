package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/csv"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

// TestIncidentUsesPrecedingManifestAndFirstAppearance checks incident cutoff data.
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
	if r.missing != "" || len(r.findings) != 1 {
		t.Fatalf("row=%+v", r)
	}
	if r.findings[0].Kind != analysis.MaintainerDomainReregisteredBefore || !r.findings[0].FirstSeen.Equal(time.Date(2020, 11, 11, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("finding=%+v", r.findings[0])
	}
	e := r.findings[0].PublisherEmailEvidence
	if e == nil || e.LatestObserved == nil {
		t.Fatalf("publisher evidence=%+v, want a latest observed publish", e)
	}
	if e.Status != analysis.PublisherEmailObserved || e.Scope != analysis.PublisherScopeIncidentPackument || e.DomainRelation != analysis.PublisherDomainSame || e.RegistrationRelation != analysis.PublisherRegistrationBefore || e.NewerPublishUnknown || e.LatestObserved.Version != "12.0.0" {
		t.Errorf("publisher evidence=%+v latest=%+v", e, *e.LatestObserved)
	}
}

// TestLockfileCountsUniqueNamesAndNotices checks lockfile deduplication.
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
	for _, want := range []string{"packages=1", "notices=1", "notice_drop_catch=0", "distinct_notice_domains=1", "reregistered_before=1"} {
		if !bytes.Contains(summary.Bytes(), []byte(want)) {
			t.Errorf("summary missing %s: %s", want, summary.String())
		}
	}
	if bytes.Contains(summary.Bytes(), []byte("node-ipc")) {
		t.Errorf("summary contains package name: %s", summary.String())
	}
}

// TestWriteRowsPrivateAndRectangular checks existing permissions and CSV width.
func TestWriteRowsPrivateAndRectangular(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "results.csv")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	rows := []row{{scope: "cases", name: "node-ipc", missing: "packument_unavailable"}, {scope: "cases", name: "example", findings: []analysis.MaintainerDomainFinding{{Kind: analysis.MaintainerDomainReregisteredBefore, Registrar: "DropCatch.com 538 LLC", DropCatch: true, PublisherEmailEvidence: &analysis.PublisherEmailEvidence{Status: analysis.PublisherEmailObserved, Source: analysis.PublisherEvidenceSource, Scope: analysis.PublisherScopeIncidentPackument, DomainRelation: analysis.PublisherDomainSame, RegistrationRelation: analysis.PublisherRegistrationBefore, LatestObserved: &analysis.PublisherLatestObserved{PublishedAt: time.Date(2024, 8, 12, 16, 28, 35, 0, time.UTC), Package: "node-ipc", Version: "12.0.0", EmailDomain: "atlantis-software.net"}}}}}}
	if err := writeRows(path, rows); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v err=%v", info.Mode().Perm(), err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil || len(records) != 3 || len(records[0]) != 23 || len(records[1]) != 23 || len(records[2]) != 23 || records[2][10] != "DropCatch.com 538 LLC" || records[2][11] != "true" || records[2][12] != "observed" || records[2][17] != "node-ipc" || records[2][19] != "atlantis-software.net" {
		t.Fatalf("records=%v err=%v", records, err)
	}
	wantHeader := []string{"publisher_status", "publisher_source", "publisher_scope", "account_history_complete", "publisher_published_at", "publisher_package", "publisher_version", "publisher_email_domain", "publisher_domain_relation", "publisher_registration_relation", "newer_publish_unknown"}
	wantEvidence := []string{"observed", "versions._npmUser", "incident_packument_before_incident", "false", "2024-08-12T16:28:35Z", "node-ipc", "12.0.0", "atlantis-software.net", "same", "before", "false"}
	for i := range wantHeader {
		if got := records[0][12+i]; got != wantHeader[i] {
			t.Errorf("header[%d]=%q, want %q", 12+i, got, wantHeader[i])
		}
		if got := records[2][12+i]; got != wantEvidence[i] {
			t.Errorf("%s=%q, want %q", wantHeader[i], got, wantEvidence[i])
		}
		if got := records[1][12+i]; got != "" {
			t.Errorf("row without a finding: %s=%q, want empty", wantHeader[i], got)
		}
	}
	// Unknown times (row date, first_seen, registered) are empty cells, not year 1.
	if records[1][2] != "" || records[2][7] != "" || records[2][8] != "" {
		t.Errorf("zero times written: %v", records)
	}
}

// TestParsePopulationMergesRepeatedPackages checks one entry per package.
func TestParsePopulationMergesRepeatedPackages(t *testing.T) {
	t.Parallel()
	got, err := parsePopulation(strings.NewReader("package,mal_versions\nnode-ipc,12.0.1\nother,1.0.0\nnode-ipc,9.2.2 9.2.3\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []incidentCase{{Package: "node-ipc", Version: "12.0.1 9.2.2 9.2.3"}, {Package: "other", Version: "1.0.0"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// TestIncidentCasesGroupVersions checks that one case package has one row.
func TestIncidentCasesGroupVersions(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	population := filepath.Join(dir, "population.csv")
	cases := filepath.Join(dir, "cases.json")
	if err := os.WriteFile(population, []byte("package,mal_versions\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cases, []byte(`[{"package":"node-ipc","version":"12.0.1"},{"package":"node-ipc","version":"12.0.0"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"events":[{"eventAction":"registration","eventDate":"2026-05-07T11:49:33Z"}]}`))
	}))
	t.Cleanup(srv.Close)
	client := rdap.NewClient()
	client.SetBaseURL("net", srv.URL)
	c := &command{packumentDir: cachedFixture(t), rdap: client}
	if err := c.incidents(context.Background(), population, cases); err != nil {
		t.Fatal(err)
	}
	if len(c.rows) != 1 || c.rows[0].name != "node-ipc" || c.rows[0].missing != "" {
		t.Fatalf("rows=%+v", c.rows)
	}
	if !strings.EqualFold(c.rows[0].scope, "cases") {
		t.Errorf("scope=%s", c.rows[0].scope)
	}
}

// TestMarkWaves checks wave thresholds.
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

// TestCSVCellQuotesFormulaPrefixes checks spreadsheet formula neutralising.
func TestCSVCellQuotesFormulaPrefixes(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"": "", "node-ipc": "node-ipc", "@scope/name": "'@scope/name", "=HYPERLINK(1)": "'=HYPERLINK(1)",
		"+1": "'+1", "-x": "'-x", "\tx": "'\tx", "\rx": "'\rx", "\nx": "'\nx",
	} {
		if got := csvCell(in); got != want {
			t.Errorf("csvCell(%q)=%q, want %q", in, got, want)
		}
	}
}

// TestPackageCell keeps well-formed scoped names and neutralises the rest.
func TestPackageCell(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"node-ipc": "node-ipc", "@babel/core": "@babel/core", "@scope/name": "@scope/name", "@scope/_name": "@scope/_name", "@_scope/name": "@_scope/name",
		"@SUM(1)": "'@SUM(1)", "=cmd|' /C calc'!A0": "'=cmd|' /C calc'!A0", "+1": "'+1",
	} {
		if got := packageCell(in); got != want {
			t.Errorf("packageCell(%q)=%q, want %q", in, got, want)
		}
	}
}

// TestLockfilePathName matches whole node_modules path components only.
func TestLockfilePathName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path, want string
		ok         bool
	}{
		{"node_modules/left-pad", "left-pad", true},
		{"node_modules/@scope/name", "@scope/name", true},
		{"node_modules/a/node_modules/b", "b", true},
		{"node_modules/@node_modules/pkg", "@node_modules/pkg", true},
		{"packages/app", "", false},
		{"", "", false},
		{"node_modules/@scope", "", false},
	}
	for _, tt := range tests {
		got, ok := lockfilePathName(tt.path)
		if got != tt.want || ok != tt.ok {
			t.Errorf("lockfilePathName(%q)=%q,%v, want %q,%v", tt.path, got, ok, tt.want, tt.ok)
		}
	}
}

// TestPackumentCachePathDistinct checks names that a separator substitution would merge.
func TestPackumentCachePathDistinct(t *testing.T) {
	t.Parallel()
	a := packumentCachePath("dir", "@scope/name__other")
	b := packumentCachePath("dir", "@scope__name/other")
	if a == b {
		t.Errorf("both names map to %q", a)
	}
	if got, want := packumentCachePath("dir", "node-ipc"), filepath.Join("dir", "node-ipc.json.gz"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if strings.Contains(filepath.Base(a), "/") {
		t.Errorf("file name %q contains a separator", a)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestHistoryCachesOnlyParsedPackuments checks that a malformed response is
// not cached, so a later valid response is fetched and cached.
func TestHistoryCachesOnlyParsedPackuments(t *testing.T) {
	t.Parallel()
	valid, err := os.ReadFile(filepath.Join("..", "..", "internal", "infrastructure", "npmjs", "testdata", "node-ipc-maintainers.json"))
	if err != nil {
		t.Fatal(err)
	}
	bodies := []string{`{"time":`, string(valid)}
	var calls int
	c := &command{packumentDir: t.TempDir(), http: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		body := bodies[min(calls, len(bodies)-1)]
		calls++
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}}
	cachePath := packumentCachePath(c.packumentDir, "node-ipc")
	if _, err := c.history(context.Background(), "node-ipc"); err == nil {
		t.Fatal("malformed packument: want error")
	}
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Fatalf("malformed packument was cached: stat err=%v", err)
	}
	if _, err := c.history(context.Background(), "node-ipc"); err != nil {
		t.Fatalf("valid packument: %v", err)
	}
	if _, err := c.history(context.Background(), "node-ipc"); err != nil {
		t.Fatalf("cached packument: %v", err)
	}
	if calls != 2 {
		t.Errorf("calls=%d, want 2 (third read served from cache)", calls)
	}
}

// TestReadLimited checks that a body over the limit is rejected, not truncated.
func TestReadLimited(t *testing.T) {
	t.Parallel()
	if got, err := readLimited(strings.NewReader("abcd"), 4); err != nil || string(got) != "abcd" {
		t.Errorf("at limit: got %q, %v", got, err)
	}
	if got, err := readLimited(strings.NewReader("abcde"), 4); err == nil {
		t.Errorf("over limit: got %q, want error", got)
	}
}
