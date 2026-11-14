package depsdev

import "testing"

// These fixtures make default, publication order, and version order disagree
// where the choice matters. Each expected value is independent of the selector.
func TestPickStableDevAndMax_PrereleaseDefaults(t *testing.T) {
	const (
		old  = "2025-01-01T00:00:00Z"
		mid  = "2026-01-01T00:00:00Z"
		new  = "2027-01-01T00:00:00Z"
		same = "2026-06-01T00:00:00Z"
	)
	type release struct {
		version string
		date    string
		def     bool
	}
	type testCase struct {
		name, system, hint string
		releases           []release
		stable, dev, max   string
		reverse            bool
	}
	cases := []testCase{
		// Default stability x other GA x preferredStable, with each hint
		// pointing to an eligible exact release.
		{"stable default, GA", "maven", "", []release{{"1.0.0", mid, true}, {"2.0.0", new, false}}, "1.0.0", "", "2.0.0", false},
		{"stable default, no GA", "maven", "", []release{{"1.0.0", mid, true}, {"2.0.0-M1", new, false}}, "1.0.0", "2.0.0-M1", "2.0.0-M1", false},
		{"unstable default, GA", "maven", "", []release{{"2.0.0-M1", new, true}, {"1.0.0", old, false}}, "1.0.0", "2.0.0-M1", "2.0.0-M1", false},
		{"unstable default, no GA", "maven", "", []release{{"2.0.0-M1", new, true}}, "2.0.0-M1", "2.0.0-M1", "2.0.0-M1", false},
		{"stable default, GA, hint", "maven", "2.0.0", []release{{"1.0.0", mid, true}, {"2.0.0", new, false}}, "2.0.0", "", "2.0.0", false},
		{"stable default, no other GA, hint", "maven", "1.0.0", []release{{"1.0.0", mid, true}, {"2.0.0-M1", new, false}}, "1.0.0", "2.0.0-M1", "2.0.0-M1", false},
		{"unstable default, GA, hint", "maven", "2.0.0-M1", []release{{"2.0.0-M1", new, true}, {"1.0.0", old, false}}, "2.0.0-M1", "2.0.0-M1", "2.0.0-M1", false},
		{"unstable default, no GA, hint", "maven", "2.0.0-M1", []release{{"2.0.0-M1", new, true}}, "2.0.0-M1", "2.0.0-M1", "2.0.0-M1", false},

		{"webflux tied dates", "maven", "", []release{{"4.2.0-RC2", new, true}, {"4.0.9", same, false}, {"4.1.2", same, false}}, "4.1.2", "4.2.0-RC2", "4.2.0-RC2", true},
		{"javafx seconds apart", "maven", "", []release{{"28-ea+12", new, true}, {"25.0.5", "2026-06-01T00:00:02Z", false}, {"27.0.1", "2026-06-01T00:00:01Z", false}, {"21.0.13", "2026-06-01T00:00:03Z", false}}, "27.0.1", "28-ea+12", "28-ea+12", true},
		{"guava mixed numeric", "maven", "", []release{{"23.0-rc1", new, true}, {"22.0", old, false}, {"9.0", mid, false}, {"r09", new, false}}, "22.0", "23.0-rc1", "23.0-rc1", true},
		{"old GA apacheds", "maven", "", []release{{"2.0.0.AM27", new, true}, {"1.5.7", old, false}}, "1.5.7", "2.0.0.AM27", "1.5.7", false},
		{"spring boot starter", "maven", "", []release{{"4.2.0-RC1", new, true}, {"4.1.2", old, false}}, "4.1.2", "4.2.0-RC1", "4.2.0-RC1", false},
		{"quarkus Final", "maven", "", []release{{"4.0.0.Beta1", new, true}, {"3.20.0.Final", old, false}}, "3.20.0.Final", "4.0.0.Beta1", "4.0.0.Beta1", false},
		{"wildfly elytron Final", "maven", "", []release{{"3.0.0.Alpha1", new, true}, {"2.9.0.Final", old, false}}, "2.9.0.Final", "3.0.0.Alpha1", "3.0.0.Alpha1", false},
		{"maintenance line", "maven", "", []release{{"4.2.0-RC1", new, true}, {"4.1.2", old, false}, {"3.5.9", mid, false}}, "4.1.2", "4.2.0-RC1", "4.2.0-RC1", false},
		{"maven Final ordering", "maven", "", []release{{"4.0.0-CR1", new, true}, {"3.20.0.Final", old, false}, {"3.19.0.Final", mid, false}}, "3.20.0.Final", "4.0.0-CR1", "4.0.0-CR1", false},
		{"maven jre stable default", "maven", "", []release{{"33.8.0-jre", new, true}, {"23.0", mid, false}, {"22.0", old, false}}, "33.8.0-jre", "", "33.8.0-jre", false},
		{"maven jre android tie", "maven", "", []release{{"34.0-rc1", new, true}, {"33.8.0-jre", same, false}, {"33.8.0-android", same, false}}, "33.8.0-jre", "34.0-rc1", "34.0-rc1", true},
		{"build metadata stable default", "maven", "", []release{{"0.11.0+wasi-snapshot-preview1", new, true}, {"0.10.0", old, false}}, "0.11.0+wasi-snapshot-preview1", "", "0.11.0+wasi-snapshot-preview1", false},
		{"GA above unstable default", "maven", "", []release{{"2.0.0-RC1", new, true}, {"3.0.0", old, false}}, "3.0.0", "2.0.0-RC1", "3.0.0", false},

		{"npm parser prereleases only", "npm", "", []release{{"2.0.0-beta.3", new, true}, {"2.0.0-next.4", mid, false}, {"1.0.0-canary.1", old, false}}, "2.0.0-beta.3", "2.0.0-beta.3", "2.0.0-next.4", false},
		{"npm parser prereleases and GA", "npm", "", []release{{"2.0.0-beta.3", new, true}, {"2.0.0-next.4", mid, false}, {"1.0.0", old, false}}, "1.0.0", "2.0.0-beta.3", "2.0.0-next.4", false},
		{"npm parser-only default", "npm", "", []release{{"2.0.0-next.4", new, true}, {"1.0.0", old, false}}, "1.0.0", "", "2.0.0-next.4", true},
		{"cargo parser prerelease", "cargo", "", []release{{"2.0.0-rc1", mid, true}, {"2.0.0-next.1", new, false}, {"1.0.0", old, false}}, "1.0.0", "2.0.0-rc1", "2.0.0-rc1", false},
		{"cargo parser-only default", "cargo", "", []release{{"2.0.0-next.4", new, true}, {"1.0.0", old, false}}, "1.0.0", "", "2.0.0-next.4", true},
		{"pypi short prerelease and GA", "pypi", "", []release{{"2.0rc1", new, true}, {"2.0b3", mid, false}, {"1.9.0", old, false}}, "1.9.0", "2.0rc1", "1.9.0", false},
		{"pypi short prerelease only", "pypi", "", []release{{"2.0rc1", new, true}, {"2.0b3", mid, false}}, "2.0rc1", "2.0rc1", "2.0rc1", false},
		{"pypi parser-only default", "pypi", "", []release{{"2.0b3", new, true}, {"1.9.0", old, false}}, "1.9.0", "", "1.9.0", true},
		{"npm mixed parse numeric fallback", "npm", "", []release{{"2.0.0-rc1", new, true}, {"1.0.0.0", mid, false}, {"0.9.0", old, false}}, "1.0.0.0", "2.0.0-rc1", "2.0.0-rc1", true},
		{"pypi mixed parse numeric fallback", "pypi", "", []release{{"4.0rc1", new, true}, {"3.20.0.Final", mid, false}, {"3.19.0", old, false}}, "3.20.0.Final", "4.0rc1", "3.19.0", true},
		{"maven numeric v prefix", "maven", "", []release{{"2.0.0-M1", new, true}, {"v3.0.0.Final", mid, false}, {"2.9.0.Final", old, false}}, "v3.0.0.Final", "2.0.0-M1", "2.0.0-M1", true},
		{"maven equal core publication tie", "maven", "", []release{{"34.0-rc1", new, true}, {"33.8.0-jre", old, false}, {"33.8.0-android", mid, false}}, "33.8.0-android", "34.0-rc1", "34.0-rc1", true},

		{"go stable tag and tied pseudos", "golang", "", []release{{"v0.9.1", old, true}, {"v0.0.0-20260327151000-87f8819acf6d", same, false}, {"v0.9.1-0.20260327151000-87f8819acf6d", same, false}, {"v0.9.2-0.20260327151000-87f8819acf6d", same, false}}, "v0.9.1", "", "v0.9.2-0.20260327151000-87f8819acf6d", true},
		{"go lipgloss pseudo exclusion", "golang", "", []release{{"v2.0.0-beta1", new, true}, {"v2.0.0-20241029194924-049a2d260c67", old, false}, {"v2.0.0-20250917201909-41ff0bf215ea", mid, false}}, "v2.0.0-beta1", "v2.0.0-beta1", "v2.0.0-beta1", true},
		{"go override tagged GA", "golang", "", []release{{"v1.1.0-rc.1", new, true}, {"v1.0.0", old, false}, {"v1.1.0-0.20260327151000-87f8819acf6d", mid, false}}, "v1.0.0", "v1.1.0-rc.1", "v1.1.0-rc.1", true},
		{"go next tag excluded from override", "golang", "", []release{{"v2.0.0-beta1", new, true}, {"v2.0.0-next.3", mid, false}, {"v1.9.0", old, false}}, "v1.9.0", "v2.0.0-beta1", "v2.0.0-next.3", true},
		{"go ea hash enters Dev", "golang", "", []release{{"v1.0.0", old, true}, {"v0.0.0-20260327151000-ea1234567890", new, false}}, "v1.0.0", "v0.0.0-20260327151000-ea1234567890", "v1.0.0", false},
		{"excluded system default first", "nuget", "", []release{{"2.0.0-rc1", mid, true}, {"2.0.0-next.1", new, false}}, "2.0.0-rc1", "2.0.0-rc1", "2.0.0-rc1", false},

		{"no default maven version order", "maven", "", []release{{"4.1.2", old, false}, {"3.5.9", new, false}}, "4.1.2", "", "4.1.2", false},
		{"no default npm version order", "npm", "", []release{{"4.1.2", old, false}, {"3.5.9", new, false}}, "4.1.2", "", "4.1.2", false},
		{"no default cargo version order", "cargo", "", []release{{"4.1.2", old, false}, {"3.5.9", new, false}}, "4.1.2", "", "4.1.2", false},
		{"no default pypi version order", "pypi", "", []release{{"4.1.2", old, false}, {"3.5.9", new, false}}, "4.1.2", "", "4.1.2", false},
		{"no default go publication order", "golang", "", []release{{"v4.1.2", old, false}, {"v3.5.9", new, false}}, "v3.5.9", "", "v4.1.2", false},
		{"no default excluded publication order", "", "", []release{{"4.1.2", old, false}, {"3.5.9", new, false}}, "3.5.9", "", "4.1.2", false},
		{"no default npm parser only", "npm", "", []release{{"1.0.0-next.1", mid, false}}, "1.0.0-next.1", "", "1.0.0-next.1", false},
		{"no default pypi parser only", "pypi", "", []release{{"2.0b3", mid, false}}, "2.0b3", "", "2.0b3", false},
		{"no default go pseudo", "golang", "", []release{{"v2.0.0-20241029194924-049a2d260c67", old, false}, {"v2.0.0-20250917201909-41ff0bf215ea", new, false}}, "v2.0.0-20250917201909-41ff0bf215ea", "", "v2.0.0-20250917201909-41ff0bf215ea", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			versions := make([]Version, 0, len(tc.releases))
			for _, r := range tc.releases {
				entry := v(r.version, r.date, r.def)
				entry.VersionKey.System = tc.system
				versions = append(versions, entry)
			}
			check := func(label string, entries []Version) {
				t.Helper()
				stable, dev, max := pickStableDevAndMax(entries, tc.hint)
				for _, gotWant := range []struct{ field, got, want string }{
					{"Stable", stable.VersionKey.Version, tc.stable},
					{"Dev", dev.VersionKey.Version, tc.dev},
					{"Max", max.VersionKey.Version, tc.max},
				} {
					if gotWant.got != gotWant.want {
						t.Errorf("%s %s = %q, want %q", label, gotWant.field, gotWant.got, gotWant.want)
					}
				}
			}
			check("forward", versions)
			if tc.reverse {
				for i, j := 0, len(versions)-1; i < j; i, j = i+1, j-1 {
					versions[i], versions[j] = versions[j], versions[i]
				}
				check("reverse", versions)
			}
		})
	}
}

func TestPickStableDevAndMax_UnstableCargoDefaultSkipsYankedGA(t *testing.T) {
	versions := []Version{
		cargoVersion(t, "2.0.0-rc.1", "2027-01-01T00:00:00Z", true, false, ""),
		cargoVersion(t, "1.5.0", "2026-01-01T00:00:00Z", false, true, "yanked"),
		cargoVersion(t, "1.4.0", "2025-01-01T00:00:00Z", false, false, ""),
	}
	stable, dev, max := pickStableDevAndMax(versions, "")
	if got := stable.VersionKey.Version; got != "1.4.0" {
		t.Errorf("Stable = %q, want 1.4.0", got)
	}
	if got := dev.VersionKey.Version; got != "2.0.0-rc.1" {
		t.Errorf("Dev = %q, want 2.0.0-rc.1", got)
	}
	if got := max.VersionKey.Version; got != "2.0.0-rc.1" {
		t.Errorf("Max = %q, want 2.0.0-rc.1 (unfiltered)", got)
	}
}
