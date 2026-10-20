package npmjs

import (
	"strings"
	"testing"
	"time"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

func TestPublisherHistoryUsesOnlyVisibleDatedNPMUsers(t *testing.T) {
	const packument = `{"name":"node-ipc","time":{"created":"2020-01-01T00:00:00Z","1":"2020-11-11T00:00:00Z","2":"2024-08-12T00:00:00Z","3":"2024-08-12T00:00:00Z","4":"2026-05-14T14:25:30Z"},"versions":{"1":{"_npmUser":{"name":"atiertant","email":"a@atlantis-software.net"}},"2":{"_npmUser":{"name":"atiertant","email":"a@ATLanTIS-Software.NET"}},"3":{"_npmUser":{"name":"atiertant"}},"no-time":{"_npmUser":{"name":"atiertant","email":"a@example.com"}}}}`
	h, err := parsePublishHistory(strings.NewReader(packument))
	if err != nil {
		t.Fatal(err)
	}
	latest := h.PublisherHistory.Latest["atiertant"]
	if len(latest) != 2 || latest[0].PublishedAt.Format(time.DateOnly) != "2024-08-12" {
		t.Fatalf("latest=%+v", latest)
	}
	if len(h.PublisherHistory.HiddenPublishedAt) != 1 || h.PublisherHistory.HiddenPublishedAt[0].Format(time.DateOnly) != "2026-05-14" {
		t.Fatalf("hidden=%+v", h.PublisherHistory.HiddenPublishedAt)
	}
	measurement, err := ParsePublishHistoryWithVersions(strings.NewReader(packument))
	if err != nil {
		t.Fatal(err)
	}
	cutoff := time.Date(2024, 8, 12, 0, 0, 0, 0, time.UTC)
	before := measurement.PublisherHistoryBefore("node-ipc", cutoff)
	if len(before.Latest["atiertant"]) != 1 || before.Latest["atiertant"][0].Version != "1" || len(before.HiddenPublishedAt) != 0 {
		t.Fatalf("before=%+v", before)
	}
}

func TestPublisherHistoryBeforeCountsOnlyHiddenVersionsBeforeCutoff(t *testing.T) {
	const packument = `{"name":"node-ipc","time":{"1":"2024-01-01T00:00:00Z","2":"2024-01-02T00:00:00Z","3":"2024-01-03T00:00:00Z"},"versions":{"1":{"_npmUser":{"name":"atiertant","email":"a@atlantis-software.net"}}}}`
	h, err := ParsePublishHistoryWithVersions(strings.NewReader(packument))
	if err != nil {
		t.Fatal(err)
	}
	f := analysis.MaintainerDomainFinding{Account: "atiertant", Domain: "atlantis-software.net"}
	for _, tc := range []struct {
		name, cutoff string
		wantCount    int
		wantUnknown  bool
	}{
		{"before first hidden", "2024-01-02T00:00:00Z", 0, false},
		{"after first hidden", "2024-01-03T00:00:00Z", 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cutoff, err := time.Parse(time.RFC3339, tc.cutoff)
			if err != nil {
				t.Fatal(err)
			}
			source := h.PublisherHistoryBefore("node-ipc", cutoff)
			e := analysis.PublisherEmailEvidenceFor(f, source, []analysis.PublisherPackageHistory{source})
			if len(source.HiddenPublishedAt) != tc.wantCount || e.NewerPublishUnknown != tc.wantUnknown {
				t.Fatalf("source=%+v evidence=%+v", source, e)
			}
		})
	}
}
