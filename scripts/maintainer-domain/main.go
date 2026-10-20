// Command maintainer-domain measures npm maintainer domain states from incident
// populations or lockfiles. Standard output contains aggregate counts only.
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/npmjs"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/rdap"
)

type row struct {
	scope, name, missing string
	at                   time.Time
	findings             []analysis.MaintainerDomainFinding
}

type incidentCase struct {
	Package string `json:"package"`
	Version string `json:"version"`
}

func parsePopulation(data io.Reader) ([]incidentCase, error) {
	records, err := csv.NewReader(data).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read population CSV: %w", err)
	}
	if len(records) == 0 {
		return nil, errors.New("empty population CSV")
	}
	columns := map[string]int{}
	for i, name := range records[0] {
		columns[name] = i
	}
	packageCol, okPackage := columns["package"]
	malCol, okMal := columns["mal_versions"]
	if !okPackage || !okMal {
		return nil, errors.New("population CSV needs package and mal_versions columns")
	}
	// Repeated package rows are merged into the first one so a package is
	// measured and counted once.
	var out []incidentCase
	index := map[string]int{}
	for _, record := range records[1:] {
		if len(record) <= packageCol || len(record) <= malCol {
			continue
		}
		name, versions := record[packageCol], record[malCol]
		if i, ok := index[name]; ok {
			out[i].Version = strings.TrimSpace(out[i].Version + " " + versions)
			continue
		}
		index[name] = len(out)
		out = append(out, incidentCase{Package: name, Version: versions})
	}
	return out, nil
}

func parseCases(data []byte) ([]incidentCase, error) {
	var cases []incidentCase
	if err := json.Unmarshal(data, &cases); err != nil {
		return nil, fmt.Errorf("decode cases JSON: %w", err)
	}
	return cases, nil
}

func parseLockfileNames(data []byte) ([]string, error) {
	var lock struct {
		Packages map[string]struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Link    bool   `json:"link"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("decode lockfile: %w", err)
	}
	if lock.Packages == nil {
		return nil, errors.New("lockfile requires a packages map")
	}
	names := map[string]bool{}
	for path, p := range lock.Packages {
		pathName, ok := lockfilePathName(path)
		if !ok || p.Version == "" || p.Link {
			continue
		}
		name := p.Name
		if name == "" {
			name = pathName
		}
		names[name] = true
	}
	return slices.Sorted(maps.Keys(names)), nil
}

type command struct {
	packumentDir string
	rdap         *rdap.Client
	http         *http.Client
	rows         []row
}

const (
	packumentRequestTimeout = 25 * time.Second
	packumentReadLimit      = 64 << 20
	waveMinDailyPackages    = 5
	waveMinTotalPackages    = 10
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: maintainer-domain incidents|lockfile [flags]")
	}
	c := &command{rdap: rdap.NewClient(), http: &http.Client{Timeout: packumentRequestTimeout}}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	packumentDir := fs.String("packument-dir", "", "compressed packument cache")
	rdapDir := fs.String("rdap-cache", "", "RDAP cache")
	outPath := fs.String("out", "", "private per-row CSV path")
	var population, cases, nowRaw *string
	switch args[0] {
	case "incidents":
		population = fs.String("population", "", "population CSV")
		cases = fs.String("cases", "", "cases JSON")
	case "lockfile":
		nowRaw = fs.String("now", "", "RFC3339 evaluation time")
	default:
		return fmt.Errorf("unknown mode %q", args[0])
	}
	if err := fs.Parse(args[1:]); err != nil {
		return fmt.Errorf("parse command flags: %w", err)
	}
	if *outPath == "" {
		return errors.New("-out is required to keep per-row evidence out of standard output")
	}
	c.packumentDir = *packumentDir
	c.rdap.SetCacheDir(*rdapDir)
	switch args[0] {
	case "incidents":
		if *population == "" || *cases == "" {
			return errors.New("incidents requires -population and -cases")
		}
		if err := c.incidents(ctx, *population, *cases); err != nil {
			return fmt.Errorf("measure incidents: %w", err)
		}
	case "lockfile":
		if *nowRaw == "" || fs.NArg() == 0 {
			return errors.New("lockfile requires -now RFC3339 and at least one path")
		}
		now, err := time.Parse(time.RFC3339, *nowRaw)
		if err != nil {
			return fmt.Errorf("invalid -now: %w", err)
		}
		for i, path := range fs.Args() {
			if err := c.lockfile(ctx, path, now, fmt.Sprintf("lockfile_%d", i+1)); err != nil {
				return fmt.Errorf("measure lockfile %q: %w", path, err)
			}
		}
	}
	if err := writeRows(*outPath, c.rows); err != nil {
		return fmt.Errorf("write results to %q: %w", *outPath, err)
	}
	return printSummary(out, c.rows)
}

// lockfilePathName returns the package name after the last node_modules path
// component, e.g. "@scope/name" for "a/node_modules/@scope/name".
func lockfilePathName(path string) (string, bool) {
	parts := strings.Split(path, "/")
	for i := len(parts) - 2; i >= 0; i-- {
		if parts[i] != "node_modules" {
			continue
		}
		rest := parts[i+1:]
		if strings.HasPrefix(rest[0], "@") {
			if len(rest) < 2 {
				return "", false
			}
			return rest[0] + "/" + rest[1], true
		}
		return rest[0], true
	}
	return "", false
}

// packumentCachePath names the cached packument file. PathEscape is
// one-to-one, so two package names never share a file.
func packumentCachePath(dir, name string) string {
	return filepath.Join(dir, url.PathEscape(name)+".json.gz")
}

func (c *command) history(ctx context.Context, name string) (*npmjs.PublishHistory, error) {
	var data []byte
	cachePath := ""
	if c.packumentDir != "" {
		cachePath = packumentCachePath(c.packumentDir, name)
		f, err := os.Open(cachePath)
		switch {
		case err == nil:
			zr, err := gzip.NewReader(f)
			if err == nil {
				data, err = readLimited(zr, packumentReadLimit)
				_ = zr.Close() // read-only; a read error is reported below
			}
			_ = f.Close() // read-only; a read error is reported below
			if err != nil {
				return nil, fmt.Errorf("read cached packument %q: %w", cachePath, err)
			}
		case !errors.Is(err, fs.ErrNotExist):
			return nil, fmt.Errorf("open cached packument %q: %w", cachePath, err)
		}
	}
	if data == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://registry.npmjs.org/"+url.PathEscape(name), nil)
		if err != nil {
			return nil, fmt.Errorf("create npm request for %q: %w", name, err)
		}
		req.Header.Set("Accept", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch npm packument for %q: %w", name, err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetch npm packument for %q: HTTP %d", name, resp.StatusCode)
		}
		data, err = readLimited(resp.Body, packumentReadLimit)
		if err != nil {
			return nil, fmt.Errorf("read npm packument for %q: %w", name, err)
		}
		// Parse before caching so a malformed or oversized response is
		// fetched again on the next run instead of being reused.
		h, err := npmjs.ParsePublishHistoryWithVersions(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("parse npm packument for %q: %w", name, err)
		}
		if cachePath != "" {
			if err := writePackumentCache(c.packumentDir, cachePath, data); err != nil {
				return nil, err
			}
		}
		return h, nil
	}
	return npmjs.ParsePublishHistoryWithVersions(bytes.NewReader(data))
}

// readLimited reads all of r, failing when it holds more than limit bytes
// rather than returning a silently truncated body.
func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("body exceeds %d bytes", limit)
	}
	return data, nil
}

func writePackumentCache(dir, cachePath string, data []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create packument cache directory: %w", err)
	}
	f, err := os.OpenFile(cachePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open packument cache %q: %w", cachePath, err)
	}
	zw := gzip.NewWriter(f)
	_, err = zw.Write(data)
	if closeErr := zw.Close(); err == nil {
		err = closeErr
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write packument cache %q: %w", cachePath, err)
	}
	return nil
}

func (c *command) incidents(ctx context.Context, populationPath, casesPath string) error {
	f, err := os.Open(populationPath)
	if err != nil {
		return fmt.Errorf("open population CSV %q: %w", populationPath, err)
	}
	defer func() { _ = f.Close() }()
	population, err := parsePopulation(f)
	if err != nil {
		return fmt.Errorf("parse population %q: %w", populationPath, err)
	}
	seen := map[string]bool{}
	for _, record := range population {
		name := record.Package
		seen[name] = true
		c.rows = append(c.rows, c.incidentRow(ctx, "population", name, strings.Fields(record.Version)))
	}
	data, err := os.ReadFile(casesPath)
	if err != nil {
		return fmt.Errorf("read cases JSON %q: %w", casesPath, err)
	}
	cases, err := parseCases(data)
	if err != nil {
		return fmt.Errorf("parse cases %q: %w", casesPath, err)
	}
	caseVersions := map[string][]string{}
	for _, tc := range cases {
		if !seen[tc.Package] {
			caseVersions[tc.Package] = append(caseVersions[tc.Package], tc.Version)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(caseVersions)) {
		c.rows = append(c.rows, c.incidentRow(ctx, "cases", name, caseVersions[name]))
	}
	markWaves(c.rows)
	return nil
}

func (c *command) incidentRow(ctx context.Context, scope, name string, versions []string) row {
	r := row{scope: scope, name: name}
	h, err := c.history(ctx, name)
	if err != nil {
		r.missing = "packument_unavailable"
		return r
	}
	malicious := map[string]bool{}
	for _, v := range versions {
		malicious[v] = true
		at := h.PublishedAt[v]
		if !at.IsZero() && (r.at.IsZero() || at.Before(r.at)) {
			r.at = at
		}
	}
	if r.at.IsZero() {
		r.missing = "missing_malicious_time"
		return r
	}
	v0 := ""
	var latest time.Time
	for version, at := range h.PublishedAt {
		if !malicious[version] && at.Before(r.at) && (v0 == "" || at.After(latest)) {
			v0, latest = version, at
		}
	}
	if v0 == "" {
		r.missing = "missing_preceding_version"
		return r
	}
	people, ok := h.VersionMaintainers[v0]
	if !ok {
		r.missing = "missing_preceding_manifest"
		return r
	}
	firstSeen := h.FirstSeenBefore(r.at)
	for _, p := range people {
		finding := c.rdap.Check(ctx, analysis.MaintainerAddress{Account: p.Name, Email: p.Email, FirstSeen: firstSeen[analysis.NormalizeEmail(p.Email)]}, r.at)
		if finding.Kind.WarrantsNotice() {
			source := h.PublisherHistoryBefore(name, r.at)
			finding.PublisherEmailEvidence = analysis.PublisherEmailEvidenceFor(finding, source, []analysis.PublisherPackageHistory{source})
		}
		r.findings = append(r.findings, finding)
	}
	return r
}

func markWaves(rows []row) {
	days := map[time.Time]int{}
	for _, r := range rows {
		if r.scope == "population" && !r.at.IsZero() {
			days[date(r.at)]++
		}
	}
	keys := slices.SortedFunc(maps.Keys(days), time.Time.Compare)
	waveDays := map[time.Time]bool{}
	for i := 0; i < len(keys); {
		if days[keys[i]] < waveMinDailyPackages {
			i++
			continue
		}
		j, count := i, 0
		for j < len(keys) && days[keys[j]] >= waveMinDailyPackages && (j == i || keys[j].Equal(keys[j-1].AddDate(0, 0, 1))) {
			count += days[keys[j]]
			j++
		}
		if count >= waveMinTotalPackages {
			for k := i; k < j; k++ {
				waveDays[keys[k]] = true
			}
		}
		i = j
	}
	for i := range rows {
		if rows[i].scope == "population" {
			if waveDays[date(rows[i].at)] {
				rows[i].scope = "wave"
			} else {
				rows[i].scope = "non-wave"
			}
		}
	}
}

func (c *command) lockfile(ctx context.Context, path string, now time.Time, scope string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read lockfile %q: %w", path, err)
	}
	ordered, err := parseLockfileNames(data)
	if err != nil {
		return fmt.Errorf("parse lockfile %q: %w", path, err)
	}
	for _, name := range ordered {
		r := row{scope: scope, name: name, at: now}
		h, err := c.history(ctx, name)
		if err != nil {
			r.missing = "packument_unavailable"
			c.rows = append(c.rows, r)
			continue
		}
		for _, p := range h.Maintainers {
			r.findings = append(r.findings, c.rdap.Check(ctx, analysis.MaintainerAddress{Account: p.Name, Email: p.Email, FirstSeen: h.FirstSeen[analysis.NormalizeEmail(p.Email)]}, now))
		}
		c.rows = append(c.rows, r)
	}
	return nil
}

func date(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func writeRows(path string, rows []row) (err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open results CSV %q: %w", path, err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close results CSV %q: %w", path, closeErr)
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("restrict results CSV %q: %w", path, err)
	}
	w := csv.NewWriter(f)
	if err := w.Write([]string{"scope", "package", "date", "missing", "account", "domain", "kind", "first_seen", "registered", "reason", "registrar", "drop_catch", "publisher_status", "publisher_source", "publisher_scope", "account_history_complete", "publisher_published_at", "publisher_package", "publisher_version", "publisher_email_domain", "publisher_domain_relation", "publisher_registration_relation", "newer_publish_unknown"}); err != nil {
		return fmt.Errorf("write results CSV header: %w", err)
	}
	for _, r := range rows {
		if len(r.findings) == 0 {
			if err := w.Write(append([]string{r.scope, packageCell(r.name), formatTime(r.at), r.missing, "", "", "", "", "", "", "", ""}, make([]string, 11)...)); err != nil {
				return fmt.Errorf("write results CSV row for %q: %w", r.name, err)
			}
			continue
		}
		for _, finding := range r.findings {
			if err := w.Write(append([]string{r.scope, packageCell(r.name), formatTime(r.at), r.missing, csvCell(finding.Account), csvCell(finding.Domain), string(finding.Kind), formatTime(finding.FirstSeen), formatTime(finding.Registered), string(finding.Reason), csvCell(finding.Registrar), fmt.Sprint(finding.DropCatch)}, publisherCSVFields(finding.PublisherEmailEvidence)...)); err != nil {
				return fmt.Errorf("write results CSV row for %q: %w", r.name, err)
			}
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return fmt.Errorf("flush results CSV %q: %w", path, err)
	}
	return nil
}

// csvCell keeps a spreadsheet from reading an external value as a formula by
// prefixing a single quote to a cell that starts with =, +, -, @, tab, CR or LF.
func csvCell(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r\n", rune(v[0])) {
		return "'" + v
	}
	return v
}

// scopedPackageName matches a well-formed scoped npm name such as @babel/core.
var scopedPackageName = regexp.MustCompile(`^@[a-z0-9_~-][a-z0-9._~-]*/[a-z0-9_~-][a-z0-9._~-]*$`)

// packageCell writes a well-formed scoped npm name as given, so rows join back
// to the caller's input, and passes any other name through csvCell. Package
// names can come from an untrusted lockfile.
func packageCell(name string) string {
	if scopedPackageName.MatchString(name) {
		return name
	}
	return csvCell(name)
}

// formatTime writes an empty cell for an unknown time instead of year 1.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func publisherCSVFields(e *analysis.PublisherEmailEvidence) []string {
	fields := make([]string, 11)
	if e == nil {
		return fields
	}
	fields[0], fields[1], fields[2], fields[3] = string(e.Status), e.Source, e.Scope, fmt.Sprint(e.AccountHistoryComplete)
	if e.LatestObserved != nil {
		fields[4], fields[5], fields[6], fields[7] = formatTime(e.LatestObserved.PublishedAt), packageCell(e.LatestObserved.Package), csvCell(e.LatestObserved.Version), csvCell(e.LatestObserved.EmailDomain)
	}
	fields[8], fields[9], fields[10] = string(e.DomainRelation), string(e.RegistrationRelation), fmt.Sprint(e.NewerPublishUnknown)
	return fields
}

func printSummary(out io.Writer, rows []row) error {
	scopes := []string{"non-wave", "wave", "cases"}
	seenScopes := map[string]bool{}
	for _, r := range rows {
		if strings.HasPrefix(r.scope, "lockfile_") && !seenScopes[r.scope] {
			scopes = append(scopes, r.scope)
			seenScopes[r.scope] = true
		}
	}
	sort.Strings(scopes[3:])
	for _, scope := range scopes {
		var total, checkable, before, every, notices, noticeDropCatch int
		checkableDays, beforeDays := map[time.Time]bool{}, map[time.Time]bool{}
		domains := map[string]bool{}
		kindCounts := map[analysis.MaintainerDomainKind]int{}
		missing := map[string]int{}
		for _, r := range rows {
			if r.scope != scope {
				continue
			}
			total++
			if r.missing != "" {
				missing[r.missing]++
			}
			anyCheck, anyBefore, allCheck, anyNotice, anyDropCatch := false, false, len(r.findings) > 0, false, false
			for _, f := range r.findings {
				kindCounts[f.Kind]++
				isCheck := f.Kind != analysis.MaintainerDomainUnchecked && f.Kind != analysis.MaintainerDomainLookupFailed
				anyCheck = anyCheck || isCheck
				allCheck = allCheck && isCheck
				anyBefore = anyBefore || f.Kind == analysis.MaintainerDomainReregisteredBefore
				if f.Kind.WarrantsNotice() {
					anyNotice = true
					anyDropCatch = anyDropCatch || f.DropCatch
					domains[f.Domain] = true
				}
			}
			if anyCheck {
				checkable++
				checkableDays[date(r.at)] = true
			}
			if anyBefore {
				before++
				beforeDays[date(r.at)] = true
			}
			if allCheck {
				every++
			}
			if anyNotice {
				notices++
			}
			if anyDropCatch {
				noticeDropCatch++
			}
		}
		if total == 0 {
			continue
		}
		if _, err := fmt.Fprintf(out, "%s: packages=%d checkable=%d reregistered_before=%d checkable_dates=%d reregistered_before_dates=%d notices=%d notice_drop_catch=%d distinct_notice_domains=%d every_address_checkable=%d\n", scope, total, checkable, before, len(checkableDays), len(beforeDays), notices, noticeDropCatch, len(domains), every); err != nil {
			return fmt.Errorf("write summary for %q: %w", scope, err)
		}
		for _, kind := range []analysis.MaintainerDomainKind{analysis.MaintainerDomainUnchecked, analysis.MaintainerDomainLookupFailed, analysis.MaintainerDomainUnregistered, analysis.MaintainerDomainLapsing, analysis.MaintainerDomainReregisteredAfter, analysis.MaintainerDomainReregisteredBefore, analysis.MaintainerDomainNormal} {
			if _, err := fmt.Fprintf(out, "  %s=%d\n", kind, kindCounts[kind]); err != nil {
				return fmt.Errorf("write summary count for %q: %w", kind, err)
			}
		}
		for _, key := range slices.Sorted(maps.Keys(missing)) {
			if _, err := fmt.Fprintf(out, "  missing_%s=%d\n", key, missing[key]); err != nil {
				return fmt.Errorf("write missing-data count for %q: %w", key, err)
			}
		}
	}
	return nil
}
