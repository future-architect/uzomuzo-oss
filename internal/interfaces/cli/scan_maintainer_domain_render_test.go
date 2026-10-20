package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/audit"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/rdap"
)

func TestMaintainerDomainJSONAndDetailedNotice(t *testing.T) {
	first := time.Date(2020, 11, 11, 0, 0, 0, 0, time.UTC)
	registered := time.Date(2026, 5, 7, 11, 49, 33, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"events":[{"eventAction":"registration","eventDate":"2026-05-07T11:49:33Z"}]}`))
	}))
	t.Cleanup(srv.Close)
	client := rdap.NewClient()
	client.SetBaseURL("net", srv.URL)
	flagged := client.Check(context.Background(), analysis.MaintainerAddress{Account: "atiertant", Email: "A.Tiertant@Atlantis-Software.NET ", FirstSeen: first}, time.Date(2026, 5, 14, 14, 25, 30, 0, time.UTC))
	a := &analysis.Analysis{MaintainerDomain: &analysis.MaintainerDomainResult{Findings: []analysis.MaintainerDomainFinding{flagged, {Account: "other", Kind: analysis.MaintainerDomainUnchecked, Reason: "free_mail"}, {Account: "third", Kind: analysis.MaintainerDomainLookupFailed}}}}
	e := audit.AuditEntry{PURL: "pkg:npm/node-ipc@12.0.0", Verdict: audit.VerdictOK, Analysis: a}
	var buf bytes.Buffer
	if err := renderScanJSON(&buf, []audit.AuditEntry{e}, []audit.AuditEntry{e}, false); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Packages []struct {
			MaintainerDomain *struct {
				Flagged []struct {
					Account    string    `json:"account"`
					Domain     string    `json:"domain"`
					Kind       string    `json:"kind"`
					FirstSeen  time.Time `json:"first_seen"`
					Registered time.Time `json:"registered"`
				} `json:"flagged"`
				Unchecked         int `json:"unchecked"`
				LookupFailed      int `json:"lookup_failed"`
				UncheckedFindings []struct {
					Account string `json:"account"`
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
	if len(m.Flagged) != 1 || m.Flagged[0].Account != "atiertant" || m.Flagged[0].Domain != "atlantis-software.net" || m.Flagged[0].Kind != "reregistered_before" || !m.Flagged[0].FirstSeen.Equal(first) || !m.Flagged[0].Registered.Equal(registered) || m.Unchecked != 1 || m.LookupFailed != 1 {
		t.Errorf("output=%s", buf.String())
	}
	if len(m.UncheckedFindings) != 1 || m.UncheckedFindings[0].Reason != "free_mail" {
		t.Errorf("unchecked=%s", buf.String())
	}
	if !strings.Contains(a.MaintainerDomainNotice(), "atlantis-software.net registered 2026-05-07") {
		t.Errorf("notice=%q", a.MaintainerDomainNotice())
	}
	buf.Reset()
	if err := renderBoxEntry(&buf, &e); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Maintainer domain:") || !strings.Contains(buf.String(), "atlantis-software.net") {
		t.Errorf("detailed box=%s", buf.String())
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

func TestScanWithoutMaintainerDomainMatchesJSONGolden(t *testing.T) {
	const golden = "{\n  \"summary\": {\n    \"total\": 0,\n    \"ok\": 0,\n    \"caution\": 0,\n    \"replace\": 0,\n    \"review\": 0,\n    \"build_integrity\": {\n      \"hardened\": 0,\n      \"moderate\": 0,\n      \"weak\": 0,\n      \"ungraded\": 0\n    }\n  },\n  \"packages\": [],\n  \"shown\": 0\n}\n"
	var buf bytes.Buffer
	if err := renderScanJSON(&buf, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	if buf.String() != golden {
		t.Errorf("JSON changed:\n%s", buf.String())
	}
}
