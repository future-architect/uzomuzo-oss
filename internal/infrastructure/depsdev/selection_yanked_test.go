package depsdev

import (
	"testing"
	"time"
)

// cargoVersion builds a cargo deps.dev version entry.
func cargoVersion(t testing.TB, version, publishedAt string, isDefault, isDeprecated bool, reason string) Version {
	t.Helper()
	published, err := time.Parse(time.RFC3339, publishedAt)
	if err != nil {
		t.Fatalf("parse publishedAt %q: %v", publishedAt, err)
	}
	return Version{
		VersionKey:       VersionKey{System: "cargo", Name: "example", Version: version},
		PublishedAt:      published,
		IsDefault:        isDefault,
		IsDeprecated:     isDeprecated,
		DeprecatedReason: reason,
	}
}

// TestPickStableDevAndMax_CargoYankedDefaultIsNotStable mirrors
// pkg:cargo/owo-colors as observed 2026-09-02: deps.dev marks the yanked 5.0.0
// isDefault=true, so the pre-fix selection returned it as Stable.
func TestPickStableDevAndMax_CargoYankedDefaultIsNotStable(t *testing.T) {
	versions := []Version{
		cargoVersion(t, "4.4.0", "2026-08-27T17:55:12Z", false, false, ""),
		cargoVersion(t, "5.0.0", "2024-09-10T16:07:38Z", true, true, "yanked"),
	}

	stable, _, max := pickStableDevAndMax(versions, "")

	if got, want := stable.VersionKey.Version, "4.4.0"; got != want {
		t.Errorf("Stable = %q, want %q (the yanked isDefault release must not win)", got, want)
	}
	// Max is deliberately unfiltered. See ADR-0024.
	if got, want := max.VersionKey.Version, "5.0.0"; got != want {
		t.Errorf("Max = %q, want %q (Max is not filtered)", got, want)
	}
}

// TestPickStableDevAndMax_CargoAllStableYanked pins the fully-yanked crate case
// (observed on gap and minae-term, where crates.io reports max_stable_version=null).
func TestPickStableDevAndMax_CargoAllStableYanked(t *testing.T) {
	versions := []Version{
		cargoVersion(t, "0.1.0", "2026-08-31T10:00:00Z", true, true, "yanked"),
	}

	stable, _, max := pickStableDevAndMax(versions, "")

	if stable.VersionKey.Version != "" {
		t.Errorf("Stable = %q, want empty when every stable release is yanked", stable.VersionKey.Version)
	}
	if got, want := max.VersionKey.Version, "0.1.0"; got != want {
		t.Errorf("Max = %q, want %q", got, want)
	}
}

// TestWithdrawnFromStable pins our reading of the deps.dev deprecatedReason
// value. The fixtures are local, so this cannot detect an upstream rename — the
// WARN branch does that at runtime. It guards the literal against local edits.
// See ADR-0024.
func TestWithdrawnFromStable(t *testing.T) {
	tests := []struct {
		name    string
		version Version
		want    bool
	}{
		{
			name:    "cargo yanked",
			version: cargoVersion(t, "1.0.0", "2026-01-01T00:00:00Z", false, true, "yanked"),
			want:    true,
		},
		{
			name:    "cargo yanked, mixed case",
			version: cargoVersion(t, "1.0.0", "2026-01-01T00:00:00Z", false, true, "Yanked"),
			want:    true,
		},
		{
			name:    "cargo yanked, surrounding whitespace",
			version: cargoVersion(t, "1.0.0", "2026-01-01T00:00:00Z", false, true, " yanked "),
			want:    true,
		},
		{
			name:    "cargo deprecated for an unknown reason is not withdrawn",
			version: cargoVersion(t, "1.0.0", "2026-01-01T00:00:00Z", false, true, "retired"),
			want:    false,
		},
		{
			name:    "cargo deprecated with no reason is not withdrawn",
			version: cargoVersion(t, "1.0.0", "2026-01-01T00:00:00Z", false, true, ""),
			want:    false,
		},
		{
			name:    "cargo not deprecated",
			version: cargoVersion(t, "1.0.0", "2026-01-01T00:00:00Z", false, false, ""),
			want:    false,
		},
		{
			name: "npm deprecation is not a yank",
			version: Version{
				VersionKey:       VersionKey{System: "npm", Name: "example", Version: "1.0.0"},
				IsDeprecated:     true,
				DeprecatedReason: "yanked",
			},
			want: false,
		},
		{
			name: "pypi deprecation is not a yank",
			version: Version{
				VersionKey:       VersionKey{System: "pypi", Name: "example", Version: "1.0.0"},
				IsDeprecated:     true,
				DeprecatedReason: "yanked",
			},
			want: false,
		},
		{
			name: "cargo ecosystem match is case-insensitive",
			version: Version{
				VersionKey:       VersionKey{System: "CARGO", Name: "example", Version: "1.0.0"},
				IsDeprecated:     true,
				DeprecatedReason: "yanked",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := withdrawnFromStable(tt.version); got != tt.want {
				t.Errorf("withdrawnFromStable() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestPickStableDevAndMax_NonCargoDeprecatedUnaffected guards the pypi path added
// in ADR-0023: deps.dev does not report pypi yanks through isDeprecated, so a
// deprecated pypi release must still be selectable as Stable.
func TestPickStableDevAndMax_NonCargoDeprecatedUnaffected(t *testing.T) {
	published, err := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}
	versions := []Version{{
		VersionKey:       VersionKey{System: "pypi", Name: "example", Version: "1.0.0"},
		PublishedAt:      published,
		IsDefault:        true,
		IsDeprecated:     true,
		DeprecatedReason: "yanked",
	}}

	stable, _, _ := pickStableDevAndMax(versions, "")

	if got, want := stable.VersionKey.Version, "1.0.0"; got != want {
		t.Errorf("Stable = %q, want %q (non-cargo must be unaffected)", got, want)
	}
}
