package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/audit"
)

// TestMaintainerDomainJSONAndDetailedNotice checks enabled output.
func TestMaintainerDomainJSONAndDetailedNotice(t *testing.T) {
	first := time.Date(2020, 11, 11, 0, 0, 0, 0, time.UTC)
	registered := time.Date(2026, 5, 7, 11, 49, 33, 0, time.UTC)
	flagged := analysis.MaintainerDomainFinding{Account: "atiertant", Domain: "atlantis-software.net", Kind: analysis.MaintainerDomainReregisteredBefore, FirstSeen: first, Registered: registered, Registrar: "DropCatch.com 538 LLC", DropCatch: true}
	flagged.PublisherEmailEvidence = &analysis.PublisherEmailEvidence{Status: analysis.PublisherEmailObserved, Source: analysis.PublisherEvidenceSource, Scope: analysis.PublisherScopeScanPackuments, DomainRelation: analysis.PublisherDomainSame, RegistrationRelation: analysis.PublisherRegistrationBefore, LatestObserved: &analysis.PublisherLatestObserved{PublishedAt: first, Package: "node-ipc", Version: "9.1.2", EmailDomain: "atlantis-software.net"}}
	unregistered := analysis.MaintainerDomainFinding{Account: "fourth", Domain: "lapsed.example", Kind: analysis.MaintainerDomainUnregistered, FirstSeen: first}
	a := &analysis.Analysis{MaintainerDomain: &analysis.MaintainerDomainResult{Findings: []analysis.MaintainerDomainFinding{
		flagged,
		{Account: "other", Domain: "gmail.com", Kind: analysis.MaintainerDomainUnchecked, Reason: analysis.MaintainerDomainReasonSharedMail},
		{Account: "third", Kind: analysis.MaintainerDomainLookupFailed},
		unregistered,
		{Account: "fifth", Domain: "old.example", Kind: analysis.MaintainerDomainNormal, FirstSeen: first, Registered: time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)},
	}}}
	e := audit.AuditEntry{PURL: "pkg:npm/node-ipc@12.0.0", Verdict: audit.VerdictOK, Analysis: a}
	var buf bytes.Buffer
	if err := renderScanJSON(&buf, []audit.AuditEntry{e}, []audit.AuditEntry{e}, false, nil); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Packages []struct {
			MaintainerDomain *struct {
				Flagged []struct {
					Account    string          `json:"account"`
					Domain     string          `json:"domain"`
					Kind       string          `json:"kind"`
					FirstSeen  time.Time       `json:"first_seen"`
					Registered json.RawMessage `json:"registered"`
					Registrar  string          `json:"registrar"`
					DropCatch  *bool           `json:"drop_catch"`
					Evidence   *struct {
						Status               string `json:"status"`
						Source               string `json:"source"`
						Scope                string `json:"scope"`
						AccountHistory       *bool  `json:"account_history_complete"`
						DomainRelation       string `json:"domain_relation"`
						RegistrationRelation string `json:"registration_relation"`
						NewerPublishUnknown  *bool  `json:"newer_publish_unknown"`
						LatestObserved       *struct {
							PublishedAt time.Time `json:"published_at"`
							Package     string    `json:"package"`
							Version     string    `json:"version"`
							EmailDomain string    `json:"email_domain"`
						} `json:"latest_observed"`
					} `json:"publisher_email_evidence"`
				} `json:"flagged"`
				Unchecked         int `json:"unchecked"`
				LookupFailed      int `json:"lookup_failed"`
				UncheckedFindings []struct {
					Account string `json:"account"`
					Domain  string `json:"domain"`
					Reason  string `json:"reason"`
				} `json:"unchecked_findings"`
			} `json:"maintainer_domain"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Packages) != 1 || out.Packages[0].MaintainerDomain == nil {
		t.Fatalf("output=%s", buf.String())
	}
	m := out.Packages[0].MaintainerDomain
	if m.Unchecked != 1 || m.LookupFailed != 1 || len(m.Flagged) != 2 {
		t.Fatalf("output=%s", buf.String())
	}
	f := m.Flagged[0]
	if f.Account != "atiertant" || f.Domain != "atlantis-software.net" || f.Kind != "reregistered_before" || !f.FirstSeen.Equal(first) || string(f.Registered) != `"2026-05-07T11:49:33Z"` || f.Registrar != "DropCatch.com 538 LLC" || f.DropCatch == nil || !*f.DropCatch {
		t.Errorf("flagged[0]=%+v", f)
	}
	ev := f.Evidence
	if ev == nil || ev.LatestObserved == nil || ev.AccountHistory == nil || ev.NewerPublishUnknown == nil {
		t.Fatalf("flagged[0] evidence missing fields: %s", buf.String())
	}
	if ev.Status != "observed" || ev.Source != "versions._npmUser" || ev.Scope != "scan_packuments" || *ev.AccountHistory || ev.DomainRelation != "same" || ev.RegistrationRelation != "before" || *ev.NewerPublishUnknown {
		t.Errorf("flagged[0] evidence=%+v", *ev)
	}
	if lo := ev.LatestObserved; !lo.PublishedAt.Equal(first) || lo.Package != "node-ipc" || lo.Version != "9.1.2" || lo.EmailDomain != "atlantis-software.net" {
		t.Errorf("flagged[0] latest_observed=%+v", *lo)
	}
	u := m.Flagged[1]
	if u.Account != "fourth" || u.Kind != "unregistered" || u.Registered != nil || u.DropCatch == nil || *u.DropCatch || u.Evidence != nil {
		t.Errorf("flagged[1]=%+v, want no registered key, drop_catch false and no publisher_email_evidence key", u)
	}
	if len(m.UncheckedFindings) != 1 || m.UncheckedFindings[0].Account != "other" || m.UncheckedFindings[0].Domain != "gmail.com" || m.UncheckedFindings[0].Reason != "free_mail" {
		t.Errorf("unchecked=%s", buf.String())
	}
	notice := strings.Join(a.MaintainerDomainNotices(), "\n")
	if !strings.Contains(notice, " The account's latest publish seen in this scan (2020-11-11) still used this domain.") {
		t.Errorf("evidence sentence missing: notice=%q", notice)
	}
	buf.Reset()
	if err := renderBoxEntry(&buf, &e); err != nil {
		t.Fatal(err)
	}
	box := buf.String()
	if got := strings.Count(box, maintainerDomainPrefix); got != 2 {
		t.Errorf("notice lines=%d, want 2 (one per flagged address):\n%s", got, box)
	}
	if !strings.Contains(box, "atlantis-software.net") || !strings.Contains(box, "lapsed.example") {
		t.Errorf("detailed box=%s", box)
	}
	a.MaintainerDomain = &analysis.MaintainerDomainResult{}
	buf.Reset()
	if err := renderScanJSON(&buf, []audit.AuditEntry{e}, []audit.AuditEntry{e}, false, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"flagged": []`) || !strings.Contains(buf.String(), `"unchecked_findings": []`) {
		t.Errorf("empty lists missing: %s", buf.String())
	}
	a.MaintainerDomain = nil
	buf.Reset()
	if err := renderScanJSON(&buf, []audit.AuditEntry{e}, []audit.AuditEntry{e}, false, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "maintainer_domain") {
		t.Errorf("flag-off JSON changed: %s", buf.String())
	}
}

// TestEmptyScanJSONGolden pins the JSON document for an empty result.
func TestEmptyScanJSONGolden(t *testing.T) {
	const golden = "{\n  \"summary\": {\n    \"total\": 0,\n    \"ok\": 0,\n    \"caution\": 0,\n    \"replace\": 0,\n    \"review\": 0,\n    \"build_integrity\": {\n      \"hardened\": 0,\n      \"moderate\": 0,\n      \"weak\": 0,\n      \"ungraded\": 0\n    }\n  },\n  \"packages\": [],\n  \"shown\": 0\n}\n"
	var buf bytes.Buffer
	if err := renderScanJSON(&buf, nil, nil, false, nil); err != nil {
		t.Fatal(err)
	}
	if buf.String() != golden {
		t.Errorf("JSON changed:\n%s", buf.String())
	}
}

func TestMaintainerDomainPackageContextJSON(t *testing.T) {
	at := time.Date(2025, 3, 1, 10, 0, 0, 0, time.UTC)
	finding := analysis.MaintainerDomainFinding{Account: "alice", Domain: "example.test", Kind: analysis.MaintainerDomainUnregistered, PackagePublish: &analysis.PackagePublish{Status: analysis.PackagePublishFound, LastPublishedAt: at, Version: "1.0.0"}}
	a := &analysis.Analysis{MaintainerDomain: &analysis.MaintainerDomainResult{AnalysedVersionPublisher: analysis.VersionPublisherTrustedPublishing, Findings: []analysis.MaintainerDomainFinding{finding}}}
	e := audit.AuditEntry{PURL: "pkg:npm/example@1.0.0", Verdict: audit.VerdictOK, Analysis: a}
	render := func() string {
		t.Helper()
		var buf bytes.Buffer
		if err := renderScanJSON(&buf, []audit.AuditEntry{e}, []audit.AuditEntry{e}, false, nil); err != nil {
			t.Fatal(err)
		}
		return buf.String()
	}
	got := render()
	for _, part := range []string{`"version_publisher": "trusted_publishing"`, `"package_publish": {`, `"status": "found"`, `"last_published_at": "2025-03-01T10:00:00Z"`, `"version": "1.0.0"`} {
		if !strings.Contains(got, part) {
			t.Errorf("missing %s in %s", part, got)
		}
	}
	if notice := strings.Join(a.MaintainerDomainNotices(), "\n"); !strings.Contains(notice, "This version was published from GitHub Actions through npm trusted publishing.") || strings.Contains(notice, "alice@") {
		t.Errorf("notice=%q", notice)
	}
	var box bytes.Buffer
	if err := renderBoxEntry(&box, &e); err != nil {
		t.Fatal(err)
	}
	boxText := strings.Join(strings.Fields(strings.ReplaceAll(box.String(), "│", "")), " ")
	if !strings.Contains(boxText, "The last publish by this account found in this package's npm records was on 2025-03-01.") || !strings.Contains(boxText, "This version was published from GitHub Actions through npm trusted publishing.") || strings.Contains(boxText, "alice@") {
		t.Errorf("detailed box=%q", box.String())
	}
	finding.Account = ""
	finding.PackagePublish = &analysis.PackagePublish{Status: analysis.PackagePublishNotFound}
	a.MaintainerDomain.Findings = []analysis.MaintainerDomainFinding{finding}
	got = render()
	if !strings.Contains(got, `"status": "not_found"`) || strings.Contains(got, `"last_published_at"`) {
		t.Errorf("not_found JSON=%s", got)
	}
	if notice := strings.Join(a.MaintainerDomainNotices(), "\n"); strings.Contains(notice, "no publish by this account") {
		t.Errorf("unnamed account notice=%q", notice)
	}
	a.MaintainerDomain.Findings = nil
	got = render()
	if !strings.Contains(got, `"version_publisher": "trusted_publishing"`) || !strings.Contains(got, `"flagged": []`) || len(a.MaintainerDomainNotices()) != 0 {
		t.Errorf("empty findings JSON=%s", got)
	}
	a.MaintainerDomain = nil
	got = render()
	if strings.Contains(got, "version_publisher") || strings.Contains(got, "package_publish") || strings.Contains(got, "maintainer_domain") {
		t.Errorf("flag-off JSON=%s", got)
	}
}

// TestScanJSONRDAPBootstrap checks that rdap_bootstrap appears only when the
// maintainer domain check ran.
func TestScanJSONRDAPBootstrap(t *testing.T) {
	var buf bytes.Buffer
	info := &analysis.RDAPBootstrapInfo{Source: analysis.RDAPBootstrapCache, Publication: "2026-09-30T23:00:03Z"}
	if err := renderScanOutput(&buf, nil, nil, FormatJSON, false, info); err != nil {
		t.Fatal(err)
	}
	var out struct {
		RDAPBootstrap *struct {
			Source      string `json:"source"`
			Publication string `json:"publication"`
		} `json:"rdap_bootstrap"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.RDAPBootstrap == nil || out.RDAPBootstrap.Source != "cache" || out.RDAPBootstrap.Publication != "2026-09-30T23:00:03Z" {
		t.Errorf("output=%s", buf.String())
	}
	buf.Reset()
	if err := renderScanOutput(&buf, nil, nil, FormatJSON, false, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "rdap_bootstrap") {
		t.Errorf("check off but rdap_bootstrap present: %s", buf.String())
	}
}
