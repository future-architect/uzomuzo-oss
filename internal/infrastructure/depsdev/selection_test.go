package depsdev

import (
	"testing"
	"time"
)

func v(version string, published string, isDefault bool) Version {
	t := time.Time{}
	if published != "" {
		tm, _ := time.Parse(time.RFC3339, published)
		t = tm
	}
	return Version{
		VersionKey:  VersionKey{Version: version},
		PublishedAt: t,
		IsDefault:   isDefault,
	}
}

func TestPickStableDevAndMax_DefaultPreferred(t *testing.T) {
	versions := []Version{
		v("1.0.0-rc1", "2024-01-01T00:00:00Z", false),
		v("1.0.0", "2024-02-01T00:00:00Z", true),
		v("1.1.0", "2024-03-01T00:00:00Z", false),
	}
	stable, dev, max := pickStableDevAndMax(versions)
	if stable.VersionKey.Version != "1.0.0" {
		t.Fatalf("stable=%s, want 1.0.0", stable.VersionKey.Version)
	}
	if dev.VersionKey.Version != "1.0.0-rc1" {
		t.Fatalf("dev=%s, want 1.0.0-rc1", dev.VersionKey.Version)
	}
	if max.VersionKey.Version != "1.1.0" {
		t.Fatalf("max=%s, want 1.1.0", max.VersionKey.Version)
	}
}

func TestPickStableDevAndMax_NoDefaults_UseLatestStable(t *testing.T) {
	versions := []Version{
		v("1.0.0", "2024-01-01T00:00:00Z", false),
		v("1.1.0", "2024-03-01T00:00:00Z", false),
		v("1.2.0-rc1", "2024-04-01T00:00:00Z", false),
	}
	stable, dev, max := pickStableDevAndMax(versions)
	if stable.VersionKey.Version != "1.1.0" {
		t.Fatalf("stable=%s, want 1.1.0", stable.VersionKey.Version)
	}
	if dev.VersionKey.Version != "1.2.0-rc1" {
		t.Fatalf("dev=%s, want 1.2.0-rc1", dev.VersionKey.Version)
	}
	if max.VersionKey.Version != "1.2.0-rc1" { // max by SemVer is 1.2.0-rc1 < 1.1.0? Actually prerelease is lower; but max among semver is 1.1.0 vs 1.2.0-rc1 -> 1.2.0-rc1 is lower than 1.2.0, but higher than 1.1.0; Masterminds treats 1.2.0-rc1 > 1.1.0
		t.Fatalf("max=%s, want 1.2.0-rc1", max.VersionKey.Version)
	}
}

func TestPickStableDevAndMax_NoStable_DefaultsAbsent(t *testing.T) {
	versions := []Version{
		v("1.0.0-rc1", "2024-01-01T00:00:00Z", false),
		v("1.1.0-beta", "2024-03-01T00:00:00Z", false),
		v("1.2.0-alpha", "2024-04-01T00:00:00Z", false),
	}
	stable, dev, max := pickStableDevAndMax(versions)
	if stable.VersionKey.Version != "" {
		t.Fatalf("stable should be empty, got %s", stable.VersionKey.Version)
	}
	if dev.VersionKey.Version != "1.2.0-alpha" {
		t.Fatalf("dev=%s, want 1.2.0-alpha", dev.VersionKey.Version)
	}
	if max.VersionKey.Version != "1.2.0-alpha" {
		t.Fatalf("max=%s, want 1.2.0-alpha", max.VersionKey.Version)
	}
}
