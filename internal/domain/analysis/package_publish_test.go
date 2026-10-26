package analysis

import (
	"strings"
	"testing"
	"time"
)

func TestPackagePublishFor(t *testing.T) {
	at := time.Date(2025, 3, 1, 10, 0, 0, 0, time.UTC)
	h := PublisherPackageHistory{Latest: map[string][]PublisherPublish{"alice": {
		{Version: "2.0.0", PublishedAt: at},
		{Version: "1.9.0", PublishedAt: at},
	}}}
	for _, tc := range []struct {
		account string
		status  PackagePublishStatus
		version string
	}{
		{"alice", PackagePublishFound, "1.9.0"},
		{"bob", PackagePublishNotFound, ""},
		{"", PackagePublishNotFound, ""},
	} {
		got := PackagePublishFor(tc.account, h)
		if got.Status != tc.status || got.Version != tc.version {
			t.Errorf("%q: %+v", tc.account, got)
		}
		if tc.status == PackagePublishFound && !got.LastPublishedAt.Equal(at) {
			t.Errorf("%q: date=%s", tc.account, got.LastPublishedAt)
		}
		if tc.status == PackagePublishNotFound && !got.LastPublishedAt.IsZero() {
			t.Errorf("%q: unexpected date=%s", tc.account, got.LastPublishedAt)
		}
	}
}

func TestVersionPublisherKindFor(t *testing.T) {
	h := &ReleaseHistory{Publishers: map[string]string{"1": "GitHub Actions", "2": "alice", "4": "GitHub\x00 Actions"}}
	for _, tc := range []struct {
		h       *ReleaseHistory
		version string
		want    VersionPublisherKind
	}{
		{h, "1", VersionPublisherTrustedPublishing},
		{h, "2", VersionPublisherOther},
		{h, "3", VersionPublisherUnknown},
		{h, "4", VersionPublisherOther},
		{h, "", VersionPublisherUnknown},
		{nil, "1", VersionPublisherUnknown},
	} {
		if got := VersionPublisherKindFor(tc.h, tc.version); got != tc.want {
			t.Errorf("version %q: got %q, want %q", tc.version, got, tc.want)
		}
	}
}

func TestPackagePublishNoticeContext(t *testing.T) {
	at := time.Date(2025, 3, 1, 10, 0, 0, 0, time.UTC)
	base := MaintainerDomainFinding{Account: "alice", Domain: "example.test", Kind: MaintainerDomainUnregistered}
	for _, tc := range []struct {
		name      string
		publish   *PackagePublish
		publisher VersionPublisherKind
		want      string
		wantTrust bool
	}{
		{"found trusted", &PackagePublish{Status: PackagePublishFound, LastPublishedAt: at}, VersionPublisherTrustedPublishing, "The last publish by this account found in this package's npm records was on 2025-03-01.", true},
		{"found other", &PackagePublish{Status: PackagePublishFound, LastPublishedAt: at}, VersionPublisherOther, "The last publish by this account found in this package's npm records was on 2025-03-01.", false},
		{"found unknown", &PackagePublish{Status: PackagePublishFound, LastPublishedAt: at}, VersionPublisherUnknown, "The last publish by this account found in this package's npm records was on 2025-03-01.", false},
		{"not found trusted", &PackagePublish{Status: PackagePublishNotFound}, VersionPublisherTrustedPublishing, "The npm records retrieved for this package show no dated publish by this account.", true},
		{"not found other", &PackagePublish{Status: PackagePublishNotFound}, VersionPublisherOther, "The npm records retrieved for this package show no dated publish by this account.", false},
		{"not found unknown", &PackagePublish{Status: PackagePublishNotFound}, VersionPublisherUnknown, "The npm records retrieved for this package show no dated publish by this account.", false},
		{"nil unknown", nil, VersionPublisherUnknown, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := base
			f.PackagePublish = tc.publish
			got := maintainerDomainNotice(f, tc.publisher)
			if !strings.Contains(got, tc.want) || strings.Contains(got, "alice@") {
				t.Errorf("notice=%q", got)
			}
			if trusted := strings.Contains(got, "This version was published from GitHub Actions through npm trusted publishing."); trusted != tc.wantTrust {
				t.Errorf("trusted=%v notice=%q", trusted, got)
			}
		})
	}
	base.Account = ""
	base.PackagePublish = &PackagePublish{Status: PackagePublishNotFound}
	if got := maintainerDomainNotice(base, VersionPublisherUnknown); strings.Contains(got, "no dated publish by this account") {
		t.Errorf("unnamed account notice=%q", got)
	}
}
