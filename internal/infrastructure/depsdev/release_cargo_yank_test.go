package depsdev

import (
	"context"
	"testing"
)

// This file exercises the ADR-0024 contract end to end through
// fetchLatestRelease: the deps.dev versions payload is decoded (including
// deprecatedReason), carried onto Version, and consumed by Stable selection.
// The unit tests in selection_yanked_test.go build Version literals directly
// and so cannot catch a break in the decode or carry step. Everything here is
// httptest-local; nothing touches the network.
//
// Fixture mirrors pkg:cargo/owo-colors as observed 2026-09-02: deps.dev marks
// the yanked 5.0.0 isDefault=true with deprecatedReason "yanked", while
// crates.io reports max_stable_version 4.4.0.
const owoColorsVersionsJSON = `{
  "versions": [
    {"versionKey": {"version": "4.4.0"}, "publishedAt": "2026-08-27T00:00:00Z", "isDefault": false, "isDeprecated": false, "deprecatedReason": ""},
    {"versionKey": {"version": "5.0.0"}, "publishedAt": "2024-09-10T00:00:00Z", "isDefault": true,  "isDeprecated": true,  "deprecatedReason": "yanked"}
  ]
}`

// allYankedVersionsJSON mirrors pkg:cargo/promptforge-gateway-config as observed
// 2026-09-02, after every release had been yanked. crates.io reports
// max_stable_version null for such crates.
const allYankedVersionsJSON = `{
  "versions": [
    {"versionKey": {"version": "0.2.0"}, "publishedAt": "2026-08-31T16:02:26Z", "isDefault": false, "isDeprecated": true, "deprecatedReason": "yanked"},
    {"versionKey": {"version": "1.1.0"}, "publishedAt": "2026-08-31T15:42:53Z", "isDefault": true,  "isDeprecated": true, "deprecatedReason": "yanked"}
  ]
}`

// unknownReasonVersionsJSON is the upstream-drift case: deps.dev still marks the
// release deprecated but with a reason we do not recognise. The release must stay
// eligible (previous behaviour) rather than being silently dropped.
const unknownReasonVersionsJSON = `{
  "versions": [
    {"versionKey": {"version": "1.0.0"}, "publishedAt": "2026-01-01T00:00:00Z", "isDefault": false, "isDeprecated": false, "deprecatedReason": ""},
    {"versionKey": {"version": "2.0.0"}, "publishedAt": "2026-02-01T00:00:00Z", "isDefault": true,  "isDeprecated": true,  "deprecatedReason": "withdrawn"}
  ]
}`

func TestFetchLatestRelease_CargoYankedDefaultIsNotStable(t *testing.T) {
	srv := newDepsDevTestServer(t, "/v3alpha/systems/cargo/packages/owo-colors", owoColorsVersionsJSON)
	c := newDepsDevClient(srv)

	info, err := c.fetchLatestRelease(context.Background(), "pkg:cargo/owo-colors")
	if err != nil {
		t.Fatalf("fetchLatestRelease: %v", err)
	}

	if got, want := info.StableVersion.VersionKey.Version, "4.4.0"; got != want {
		t.Errorf("StableVersion = %q, want %q (the yanked isDefault release must not win)", got, want)
	}
	// The reason must survive decoding, or the filter above it is inert.
	if got, want := info.MaxSemverVersion.VersionKey.Version, "5.0.0"; got != want {
		t.Errorf("MaxSemverVersion = %q, want %q (Max is deliberately unfiltered)", got, want)
	}
	if got, want := info.MaxSemverVersion.DeprecatedReason, "yanked"; got != want {
		t.Errorf("MaxSemverVersion.DeprecatedReason = %q, want %q (decode/carry regression)", got, want)
	}
	if !info.MaxSemverVersion.IsDeprecated {
		t.Error("MaxSemverVersion.IsDeprecated = false, want true")
	}
}

func TestFetchLatestRelease_CargoAllReleasesYanked(t *testing.T) {
	srv := newDepsDevTestServer(t, "/v3alpha/systems/cargo/packages/promptforge-gateway-config", allYankedVersionsJSON)
	c := newDepsDevClient(srv)

	info, err := c.fetchLatestRelease(context.Background(), "pkg:cargo/promptforge-gateway-config")
	if err != nil {
		t.Fatalf("fetchLatestRelease: %v", err)
	}

	if got := info.StableVersion.VersionKey.Version; got != "" {
		t.Errorf("StableVersion = %q, want empty when every release is yanked", got)
	}
}

func TestFetchLatestRelease_CargoUnknownDeprecatedReasonStaysEligible(t *testing.T) {
	srv := newDepsDevTestServer(t, "/v3alpha/systems/cargo/packages/example", unknownReasonVersionsJSON)
	c := newDepsDevClient(srv)

	info, err := c.fetchLatestRelease(context.Background(), "pkg:cargo/example")
	if err != nil {
		t.Fatalf("fetchLatestRelease: %v", err)
	}

	if got, want := info.StableVersion.VersionKey.Version, "2.0.0"; got != want {
		t.Errorf("StableVersion = %q, want %q (an unrecognised reason must not withdraw a release)", got, want)
	}
}
