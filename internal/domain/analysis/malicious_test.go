package analysis

import "testing"

func TestClassifyMalicious(t *testing.T) {
	base := AdvisoryRecord{ID: "MAL-2025-1", Affected: []AdvisoryAffected{{Ecosystem: "npm", Name: "chalk", Versions: []string{"5.6.1"}}}}
	tests := []struct {
		name, eco, pkg, version string
		record                  AdvisoryRecord
		want                    bool
		scope                   MaliciousScope
	}{
		{"listed version", "npm", "chalk", "5.6.1", base, true, MaliciousScopeVersion},
		{"next release", "npm", "chalk", "5.6.2", base, false, ""},
		{"different package", "npm", "other", "5.6.1", base, false, ""},
		{"without version", "npm", "chalk", "", base, false, ""},
		{"withdrawn", "npm", "chalk", "5.6.1", AdvisoryRecord{ID: base.ID, Withdrawn: true, Affected: base.Affected}, false, ""},
		{"GHSA CWE-506", "npm", "chalk", "5.6.1", AdvisoryRecord{ID: "GHSA-abc", CWEIDs: []string{"CWE-506"}, Affected: base.Affected}, true, MaliciousScopeVersion},
		{"GHSA other CWE", "npm", "chalk", "5.6.1", AdvisoryRecord{ID: "GHSA-abc", CWEIDs: []string{"CWE-94"}, Affected: base.Affected}, false, ""},
		{"alias ignored", "npm", "chalk", "5.6.1", AdvisoryRecord{ID: "PYSEC-1", Aliases: []string{"MAL-2025-1"}, Affected: base.Affected}, false, ""},
		{"all versions", "npm", "chalk", "", AdvisoryRecord{ID: base.ID, Affected: []AdvisoryAffected{{Ecosystem: "npm", Name: "chalk", Ranges: []AdvisoryRange{{Type: "SEMVER", Events: []AdvisoryRangeEvent{{Introduced: "0"}}}}}}}, true, MaliciousScopePackage},
		{"ecosystem all", "npm", "chalk", "1.0", AdvisoryRecord{ID: base.ID, Affected: []AdvisoryAffected{{Ecosystem: "npm", Name: "chalk", Ranges: []AdvisoryRange{{Type: "ECOSYSTEM", Events: []AdvisoryRangeEvent{{Introduced: "0"}}}}}}}, true, MaliciousScopePackage},
		{"ecosystem bounded", "npm", "chalk", "1.0.0", AdvisoryRecord{ID: base.ID, Affected: []AdvisoryAffected{{Ecosystem: "npm", Name: "chalk", Ranges: []AdvisoryRange{{Type: "ECOSYSTEM", Events: []AdvisoryRangeEvent{{Introduced: "0"}, {Fixed: "2.0.0"}}}}}}}, false, ""},
		{"go leading v", "Go", "example.com/mod", "v1.2.3", AdvisoryRecord{ID: base.ID, Affected: []AdvisoryAffected{{Ecosystem: "Go", Name: "example.com/mod", Versions: []string{"1.2.3"}}}}, true, MaliciousScopeVersion},
		{"second affected entry", "npm", "chalk", "5.6.1", AdvisoryRecord{ID: base.ID, Affected: []AdvisoryAffected{{Ecosystem: "npm", Name: "chalk", Versions: []string{"1.0.0"}}, {Ecosystem: "npm", Name: "chalk", Versions: []string{"5.6.1"}}}}, true, MaliciousScopeVersion},
		{"PyPI separator variants", "PyPI", "friendly.bard", "1.0.0", AdvisoryRecord{ID: base.ID, Affected: []AdvisoryAffected{{Ecosystem: "PyPI", Name: "Friendly_Bard", Versions: []string{"1.0.0"}}}}, true, MaliciousScopeVersion},
		{"PyPI hyphen variant", "PyPI", "friendly-bard", "1.0.0", AdvisoryRecord{ID: base.ID, Affected: []AdvisoryAffected{{Ecosystem: "PyPI", Name: "Friendly_Bard", Versions: []string{"1.0.0"}}}}, true, MaliciousScopeVersion},
		{"versionless bounded range", "npm", "chalk", "", AdvisoryRecord{ID: base.ID, Affected: []AdvisoryAffected{{Ecosystem: "npm", Name: "chalk", Ranges: []AdvisoryRange{{Type: "SEMVER", Events: []AdvisoryRangeEvent{{Introduced: "1.0.0"}, {Fixed: "2.0.0"}}}}}}}, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyMalicious([]AdvisoryRecord{tt.record}, tt.eco, tt.pkg, tt.version)
			if (got.Status == MaliciousStatusFlagged) != tt.want || got.Scope != tt.scope {
				t.Fatalf("got %+v, want malicious=%v scope=%q", got, tt.want, tt.scope)
			}
		})
	}
}

func TestMaliciousSemverRanges(t *testing.T) {
	for _, tt := range []struct {
		name, version string
		events        []AdvisoryRangeEvent
		want          bool
	}{
		{"introduced inclusive", "1.95.6", []AdvisoryRangeEvent{{Introduced: "1.95.6"}, {Fixed: "1.95.8"}}, true},
		{"prerelease included", "1.95.7-rc.1", []AdvisoryRangeEvent{{Introduced: "1.95.6"}, {Fixed: "1.95.8"}}, true},
		{"fixed exclusive", "1.95.8", []AdvisoryRangeEvent{{Introduced: "1.95.6"}, {Fixed: "1.95.8"}}, false},
		{"limit exclusive", "2.0.0", []AdvisoryRangeEvent{{Introduced: "1.0.0"}, {Limit: "2.0.0"}}, false},
		{"last affected inclusive", "1.5.0", []AdvisoryRangeEvent{{Introduced: "1.0.0"}, {LastAffected: "1.5.0"}}, true},
		{"last affected next", "1.5.1", []AdvisoryRangeEvent{{Introduced: "1.0.0"}, {LastAffected: "1.5.0"}}, false},
		{"second interval", "3.0.0", []AdvisoryRangeEvent{{Introduced: "1.0.0"}, {Fixed: "2.0.0"}, {Introduced: "3.0.0"}, {Fixed: "4.0.0"}}, true},
		{"unknown later event invalidates range", "1.5.0", []AdvisoryRangeEvent{{Introduced: "1.0.0"}, {Fixed: "2.0.0"}, {}}, false},
		{"build metadata", "1.2.3+build.7", []AdvisoryRangeEvent{{Introduced: "1.2.3"}, {Fixed: "1.2.4"}}, true},
		{"invalid version", "not-semver", []AdvisoryRangeEvent{{Introduced: "1.0.0"}, {Fixed: "2.0.0"}}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := AdvisoryRecord{ID: "MAL-1", Affected: []AdvisoryAffected{{Ecosystem: "npm", Name: "x", Ranges: []AdvisoryRange{{Type: "SEMVER", Events: tt.events}}}}}
			if got := ClassifyMalicious([]AdvisoryRecord{rec}, "npm", "x", tt.version).Status == MaliciousStatusFlagged; got != tt.want {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestMaliciousSemverPrecedence(t *testing.T) {
	ordered := []string{"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0"}
	for i := 1; i < len(ordered); i++ {
		left, ok := parseMaliciousSemver(ordered[i-1])
		if !ok {
			t.Fatal(ordered[i-1])
		}
		right, ok := parseMaliciousSemver(ordered[i])
		if !ok {
			t.Fatal(ordered[i])
		}
		if compareMaliciousSemver(left, right) >= 0 {
			t.Fatalf("%q should precede %q", ordered[i-1], ordered[i])
		}
	}
}

func FuzzMaliciousSemver(f *testing.F) {
	for _, seed := range []string{"1.0.0", "1.0.0-alpha.1+build", "v2.3.4", "1.02.3", "-", "999999999999999999999.0.0"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		v, ok := parseMaliciousSemver(raw)
		if ok && compareMaliciousSemver(v, v) != 0 {
			t.Fatal("version must equal itself")
		}
	})
}
