package npmjs

import (
	"strings"
	"testing"
	"time"
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
