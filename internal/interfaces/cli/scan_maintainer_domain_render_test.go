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
	if err := renderScanJSON(&buf, []audit.AuditEntry{e}, []audit.AuditEntry{e}, false); err != nil {
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
	u := m.Flagged[1]
	if u.Account != "fourth" || u.Kind != "unregistered" || u.Registered != nil || u.DropCatch == nil || *u.DropCatch {
		t.Errorf("flagged[1]=%+v, want no registered key and drop_catch false", u)
	}
	if len(m.UncheckedFindings) != 1 || m.UncheckedFindings[0].Account != "other" || m.UncheckedFindings[0].Domain != "gmail.com" || m.UncheckedFindings[0].Reason != "free_mail" {
		t.Errorf("unchecked=%s", buf.String())
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
	if err := renderScanJSON(&buf, []audit.AuditEntry{e}, []audit.AuditEntry{e}, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"flagged": []`) || !strings.Contains(buf.String(), `"unchecked_findings": []`) {
		t.Errorf("empty lists missing: %s", buf.String())
	}
	a.MaintainerDomain = nil
	buf.Reset()
	if err := renderScanJSON(&buf, []audit.AuditEntry{e}, []audit.AuditEntry{e}, false); err != nil {
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
	if err := renderScanJSON(&buf, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	if buf.String() != golden {
		t.Errorf("JSON changed:\n%s", buf.String())
	}
}
