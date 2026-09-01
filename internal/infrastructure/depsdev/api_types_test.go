package depsdev

import "testing"

// TestReleaseInfo_HasAnyVersion is a decision-table test over the four
// version slots ReleaseInfo.HasAnyVersion inspects. Each row populates
// exactly one slot (or none), pinning that Stable alone is not treated as a
// sufficient signal — see ADR-0023 and the batch_details_test.go regression
// guards that depend on MaxSemverVersion and RequestedVersion alone also
// being able to keep a ReleaseInfo attached.
func TestReleaseInfo_HasAnyVersion(t *testing.T) {
	tests := []struct {
		name string
		info ReleaseInfo
		want bool
	}{
		{
			name: "only StableVersion populated",
			info: ReleaseInfo{StableVersion: Version{VersionKey: VersionKey{Version: "1.0.0"}}},
			want: true,
		},
		{
			name: "only PreReleaseVersion populated",
			info: ReleaseInfo{PreReleaseVersion: Version{VersionKey: VersionKey{Version: "1.0.0-rc1"}}},
			want: true,
		},
		{
			name: "only MaxSemverVersion populated",
			info: ReleaseInfo{MaxSemverVersion: Version{VersionKey: VersionKey{Version: "3.0.0"}}},
			want: true,
		},
		{
			name: "only RequestedVersion populated",
			info: ReleaseInfo{RequestedVersion: Version{VersionKey: VersionKey{Version: "2.0.0"}}},
			want: true,
		},
		{
			name: "all four slots empty",
			info: ReleaseInfo{},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.info.HasAnyVersion(); got != tt.want {
				t.Fatalf("HasAnyVersion()=%v, want %v", got, tt.want)
			}
		})
	}
}
