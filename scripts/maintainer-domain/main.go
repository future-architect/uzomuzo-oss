// Command maintainer-domain measures npm maintainer domain states from incident
// populations or lockfiles. Standard output contains aggregate counts only.
package main

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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

type command struct {
	packumentDir string
	rdap         *rdap.Client
	http         *http.Client
	rows         []row
}

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
	c := &command{rdap: rdap.NewClient(), http: &http.Client{Timeout: 25 * time.Second}}
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
		return err
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
			return err
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
				return err
			}
		}
	}
	if err := writeRows(*outPath, c.rows); err != nil {
		return err
	}
	return printSummary(out, c.rows)
}

func (c *command) history(ctx context.Context, name string) (*npmjs.PublishHistory, error) {
	var data []byte
	cachePath := ""
	if c.packumentDir != "" {
		cachePath = filepath.Join(c.packumentDir, strings.ReplaceAll(name, "/", "__")+".json.gz")
		if f, err := os.Open(cachePath); err == nil {
			zr, err := gzip.NewReader(f)
			if err == nil {
				data, err = io.ReadAll(io.LimitReader(zr, 64<<20))
				_ = zr.Close()
			}
			_ = f.Close()
			if err != nil {
				return nil, err
			}
		}
	}
	if data == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://registry.npmjs.org/"+url.PathEscape(name), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("npm HTTP %d", resp.StatusCode)
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		if err != nil {
			return nil, err
		}
		if cachePath != "" {
			if err := os.MkdirAll(c.packumentDir, 0o700); err != nil {
				return nil, err
			}
			f, err := os.OpenFile(cachePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
			if err != nil {
				return nil, err
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
				return nil, err
			}
		}
	}
	return npmjs.ParsePublishHistory(strings.NewReader(string(data)))
}

func (c *command) incidents(ctx context.Context, populationPath, casesPath string) error {
	f, err := os.Open(populationPath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	records, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return errors.New("empty population CSV")
	}
	columns := map[string]int{}
	for i, name := range records[0] {
		columns[name] = i
	}
	packageCol, okPackage := columns["package"]
	malCol, okMal := columns["mal_versions"]
	if !okPackage || !okMal {
		return errors.New("population CSV needs package and mal_versions columns")
	}
	seen := map[string]bool{}
	for _, record := range records[1:] {
		if len(record) <= packageCol || len(record) <= malCol {
			continue
		}
		name := record[packageCol]
		seen[name] = true
		c.rows = append(c.rows, c.incidentRow(ctx, "population", name, strings.Fields(record[malCol])))
	}
	data, err := os.ReadFile(casesPath)
	if err != nil {
		return err
	}
	var cases []struct {
		Package string `json:"package"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		return err
	}
	for _, tc := range cases {
		if !seen[tc.Package] {
			c.rows = append(c.rows, c.incidentRow(ctx, "cases", tc.Package, []string{tc.Version}))
		}
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
		r.findings = append(r.findings, c.rdap.Check(ctx, analysis.MaintainerAddress{Account: p.Name, Email: p.Email, FirstSeen: firstSeen[npmjs.NormalizeEmail(p.Email)]}, r.at))
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
	keys := make([]time.Time, 0, len(days))
	for day := range days {
		keys = append(keys, day)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].Before(keys[j]) })
	waveDays := map[time.Time]bool{}
	for i := 0; i < len(keys); {
		if days[keys[i]] < 5 {
			i++
			continue
		}
		j, count := i, 0
		for j < len(keys) && days[keys[j]] >= 5 && (j == i || keys[j].Equal(keys[j-1].AddDate(0, 0, 1))) {
			count += days[keys[j]]
			j++
		}
		if count >= 10 {
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
		return err
	}
	var lock struct {
		Packages map[string]struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Link    bool   `json:"link"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return err
	}
	if lock.Packages == nil {
		return errors.New("lockfile requires a packages map")
	}
	names := map[string]bool{}
	for path, p := range lock.Packages {
		i := strings.LastIndex(path, "node_modules/")
		if i < 0 || p.Version == "" || p.Link {
			continue
		}
		name := p.Name
		if name == "" {
			name = path[i+len("node_modules/"):]
		}
		names[name] = true
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	for _, name := range ordered {
		r := row{scope: scope, name: name, at: now}
		h, err := c.history(ctx, name)
		if err != nil {
			r.missing = "packument_unavailable"
			c.rows = append(c.rows, r)
			continue
		}
		for _, p := range h.Maintainers {
			r.findings = append(r.findings, c.rdap.Check(ctx, analysis.MaintainerAddress{Account: p.Name, Email: p.Email, FirstSeen: h.FirstSeen[npmjs.NormalizeEmail(p.Email)]}, now))
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

func writeRows(path string, rows []row) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	w := csv.NewWriter(f)
	if err := w.Write([]string{"scope", "package", "date", "missing", "account", "domain", "kind", "first_seen", "registered", "reason"}); err != nil {
		return err
	}
	for _, r := range rows {
		if len(r.findings) == 0 {
			if err := w.Write([]string{r.scope, r.name, r.at.Format(time.RFC3339), r.missing}); err != nil {
				return err
			}
			continue
		}
		for _, finding := range r.findings {
			if err := w.Write([]string{r.scope, r.name, r.at.Format(time.RFC3339), r.missing, finding.Account, finding.Domain, string(finding.Kind), finding.FirstSeen.Format(time.RFC3339), finding.Registered.Format(time.RFC3339), finding.Reason}); err != nil {
				return err
			}
		}
	}
	w.Flush()
	return w.Error()
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
		var total, checkable, before, every, notices int
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
			anyCheck, anyBefore, allCheck, anyNotice := false, false, len(r.findings) > 0, false
			for _, f := range r.findings {
				kindCounts[f.Kind]++
				isCheck := f.Kind != analysis.MaintainerDomainUnchecked && f.Kind != analysis.MaintainerDomainLookupFailed
				anyCheck = anyCheck || isCheck
				allCheck = allCheck && isCheck
				anyBefore = anyBefore || f.Kind == analysis.MaintainerDomainReregisteredBefore
				if f.Kind == analysis.MaintainerDomainUnregistered || f.Kind == analysis.MaintainerDomainLapsing || f.Kind == analysis.MaintainerDomainReregisteredBefore {
					anyNotice = true
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
		}
		if total == 0 {
			continue
		}
		if _, err := fmt.Fprintf(out, "%s: packages=%d checkable=%d reregistered_before=%d checkable_dates=%d reregistered_before_dates=%d notices=%d distinct_notice_domains=%d every_address_checkable=%d\n", scope, total, checkable, before, len(checkableDays), len(beforeDays), notices, len(domains), every); err != nil {
			return err
		}
		for _, kind := range []analysis.MaintainerDomainKind{analysis.MaintainerDomainUnchecked, analysis.MaintainerDomainLookupFailed, analysis.MaintainerDomainUnregistered, analysis.MaintainerDomainLapsing, analysis.MaintainerDomainReregisteredAfter, analysis.MaintainerDomainReregisteredBefore, analysis.MaintainerDomainNormal} {
			if _, err := fmt.Fprintf(out, "  %s=%d\n", kind, kindCounts[kind]); err != nil {
				return err
			}
		}
		keys := make([]string, 0, len(missing))
		for key := range missing {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if _, err := fmt.Fprintf(out, "  missing_%s=%d\n", key, missing[key]); err != nil {
				return err
			}
		}
	}
	return nil
}
