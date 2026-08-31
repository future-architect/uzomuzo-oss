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
	stable, dev, max := pickStableDevAndMax(versions, "")
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
	stable, dev, max := pickStableDevAndMax(versions, "")
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
	stable, dev, max := pickStableDevAndMax(versions, "")
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

// TestPickStableDevAndMax_HintEmpty_MatchesOldBehavior pins that an empty hint
// produces byte-for-byte the same Stable/Dev/Max as before ADR-0022: the
// registry-hint path (pickByRegistryStable) must be a no-op when there is no
// hint, falling straight through to the deps.dev rules.
func TestPickStableDevAndMax_HintEmpty_MatchesOldBehavior(t *testing.T) {
	versions := []Version{
		v("1.0.0-rc1", "2024-01-01T00:00:00Z", false),
		v("1.0.0", "2024-02-01T00:00:00Z", true),
		v("2.0.0", "2024-03-01T00:00:00Z", false),
	}

	wantStable, wantDev, wantMax := pickStableDevAndMax(versions, "")
	gotStable, gotDev, gotMax := pickStableDevAndMax(versions, "")

	if gotStable.VersionKey.Version != wantStable.VersionKey.Version {
		t.Fatalf("stable=%s, want %s", gotStable.VersionKey.Version, wantStable.VersionKey.Version)
	}
	if gotDev.VersionKey.Version != wantDev.VersionKey.Version {
		t.Fatalf("dev=%s, want %s", gotDev.VersionKey.Version, wantDev.VersionKey.Version)
	}
	if gotMax.VersionKey.Version != wantMax.VersionKey.Version {
		t.Fatalf("max=%s, want %s", gotMax.VersionKey.Version, wantMax.VersionKey.Version)
	}
	if gotStable.VersionKey.Version != "1.0.0" {
		t.Fatalf("stable=%s, want 1.0.0 (deps.dev isDefault rule)", gotStable.VersionKey.Version)
	}
}

// TestPickStableDevAndMax_Hint_ExactMatchWins covers rule 1: an exact string
// match on preferredStable wins even when a different version carries
// isDefault=true — the registry hint outranks deps.dev's own flag.
func TestPickStableDevAndMax_Hint_ExactMatchWins(t *testing.T) {
	versions := []Version{
		v("1.0.0", "2024-01-01T00:00:00Z", false),
		v("1.1.0", "2024-02-01T00:00:00Z", true), // isDefault, but not the hint
		v("1.2.0", "2024-03-01T00:00:00Z", false),
	}
	stable, _, _ := pickStableDevAndMax(versions, "1.0.0")
	if stable.VersionKey.Version != "1.0.0" {
		t.Fatalf("stable=%s, want 1.0.0 (exact hint match)", stable.VersionKey.Version)
	}
}

// TestPickStableDevAndMax_Hint_RegressionGuard490 is the core regression guard
// for issue #490: the hint's exact string is absent from the deps.dev list (as
// happens when the registry has moved on to a version deps.dev has not yet
// indexed, or vice versa), and a strictly newer version IS present. That newer
// version must not be selected — rule 2 bounds selection to versions <= hint.
func TestPickStableDevAndMax_Hint_RegressionGuard490(t *testing.T) {
	// Mirrors the pydantic-extra-types case from ADR-0022: PyPI's info.version
	// (2.11.1) is not itself in the deps.dev list, and deps.dev's isDefault
	// (2.11.2) is newer than the hint and yanked in the real-world case.
	versions := []Version{
		v("2.11.0", "2026-01-01T00:00:00Z", false),
		v("2.11.2", "2026-03-01T00:00:00Z", true), // newer than hint, isDefault=true
	}
	stable, _, _ := pickStableDevAndMax(versions, "2.11.1")
	if stable.VersionKey.Version == "2.11.2" {
		t.Fatalf("stable=%s selected the newer isDefault version above the hint; rule 2 must exclude it", stable.VersionKey.Version)
	}
	if stable.VersionKey.Version != "2.11.0" {
		t.Fatalf("stable=%s, want 2.11.0 (greatest version <= hint 2.11.1)", stable.VersionKey.Version)
	}
}

// TestPickStableDevAndMax_Hint_NothingBelowBound_LeavesStableEmpty covers: the
// hint parses fine, but every version in the list is strictly greater than
// it. Rule 2 finds no candidate; the bound still governs (governs=true with a
// zero Version), so Stable is left EMPTY rather than falling back to rule 3.
// This replaces the pre-ADR-0023 fallback, which re-selected a newer,
// possibly-yanked release whenever nothing qualified under the bound — see
// ADR-0023 "Decision" rule 2 and the "Rejected alternatives" discussion of why
// falling back here reopens the same hole. Dev and Max are computed
// independently of the bound and stay populated.
func TestPickStableDevAndMax_Hint_NothingBelowBound_LeavesStableEmpty(t *testing.T) {
	versions := []Version{
		v("2.0.0-rc1", "2024-01-01T00:00:00Z", true),
		v("3.0.0", "2024-02-01T00:00:00Z", false),
	}
	stable, dev, max := pickStableDevAndMax(versions, "1.0.0")
	if stable.VersionKey.Version != "" {
		t.Fatalf("stable=%s, want empty (bound governs, nothing qualifies below it)", stable.VersionKey.Version)
	}
	if dev.VersionKey.Version != "2.0.0-rc1" {
		t.Fatalf("dev=%s, want 2.0.0-rc1 (Dev is unaffected by the bound: it is the latest non-stable by purl.IsStableVersion)", dev.VersionKey.Version)
	}
	if max.VersionKey.Version != "3.0.0" {
		t.Fatalf("max=%s, want 3.0.0 (Max is unaffected by the bound)", max.VersionKey.Version)
	}
}

// TestPickByRegistryStable_NothingBelowBound_GovernsTrueZeroVersion is the
// direct unit-level pin on pickByRegistryStable's contract: when the bound
// applies (a parseable hint) but excludes every candidate, it returns
// (Version{}, true) — governs=true signals "the bound applies", not "a
// version was found". Callers must not mistake a zero Version for
// governs=false.
func TestPickByRegistryStable_NothingBelowBound_GovernsTrueZeroVersion(t *testing.T) {
	versions := []Version{
		v("2.0.0", "2024-01-01T00:00:00Z", true),
		v("3.0.0", "2024-02-01T00:00:00Z", false),
	}
	picked, governs := pickByRegistryStable(versions, "1.0.0")
	if !governs {
		t.Fatalf("governs=false, want true (a parseable hint always governs)")
	}
	if picked.VersionKey.Version != "" {
		t.Fatalf("picked=%s, want zero Version", picked.VersionKey.Version)
	}
}

// TestPickStableDevAndMax_Hint_Unparseable_FallsBack covers: the hint string
// cannot be parsed as a PEP 440 version at all, so rule 2 cannot run, and
// selection falls back to rule 3.
func TestPickStableDevAndMax_Hint_Unparseable_FallsBack(t *testing.T) {
	versions := []Version{
		v("1.0.0", "2024-01-01T00:00:00Z", true),
		v("1.1.0", "2024-02-01T00:00:00Z", false),
	}
	stable, _, _ := pickStableDevAndMax(versions, "not-a-pep440-version-!!!")
	if stable.VersionKey.Version != "1.0.0" {
		t.Fatalf("stable=%s, want 1.0.0 (fallback to isDefault rule on unparseable hint)", stable.VersionKey.Version)
	}
}

// TestPickStableDevAndMax_Hint_PEP440EquivalentSpellings verifies that
// versions the deps.dev feed spells differently from the hint, but which
// PEP 440 considers the SAME version, are still matched by rule 2 (ordering,
// not string equality). Each sub-case was verified directly against
// github.com/aquasecurity/go-pep440-version before being asserted here.
func TestPickStableDevAndMax_Hint_PEP440EquivalentSpellings(t *testing.T) {
	tests := []struct {
		name        string
		hint        string
		listVersion string
	}{
		{
			name:        "case-insensitive pre-release tag: 1.0RC1 vs 1.0rc1",
			hint:        "1.0RC1",
			listVersion: "1.0rc1",
		},
		{
			name:        "post-release dash shorthand: 1.0-1 vs 1.0.post1",
			hint:        "1.0-1",
			listVersion: "1.0.post1",
		},
		{
			name:        "zero-padded release segment: 1.01 vs 1.1",
			hint:        "1.01",
			listVersion: "1.1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			versions := []Version{
				v(tt.listVersion, "2024-01-01T00:00:00Z", false),
			}
			stable, _, _ := pickStableDevAndMax(versions, tt.hint)
			if stable.VersionKey.Version != tt.listVersion {
				t.Fatalf("stable=%q, want %q (hint %q is PEP 440-equal)", stable.VersionKey.Version, tt.listVersion, tt.hint)
			}
		})
	}
}

// TestPickStableDevAndMax_Hint_VersionOrderNotStabilityTier covers the
// ADR-0023 "Stability is not a tier" rule directly: among candidates <=
// hint, ordering inside the bound is by PEP 440 version alone, so a
// pre-release that sorts higher wins over a stable release that sorts lower
// — the opposite of an earlier draft that preferred non-pre-releases ahead
// of version order. The ADR calls out hint=100.0 selecting stable 1.0 over
// 99.0rc1 as the exact wrong answer this rule prevents.
func TestPickStableDevAndMax_Hint_VersionOrderNotStabilityTier(t *testing.T) {
	versions := []Version{
		v("1.0", "2024-01-01T00:00:00Z", false),     // stable, but far lower under PEP 440 order
		v("99.0rc1", "2024-02-01T00:00:00Z", false), // pre-release, higher, still <= hint
	}
	stable, _, _ := pickStableDevAndMax(versions, "100.0")
	if stable.VersionKey.Version != "99.0rc1" {
		t.Fatalf("stable=%s, want 99.0rc1 (version order wins; stability is not a tier)", stable.VersionKey.Version)
	}
}

// TestPickStableDevAndMax_Hint_UnparseableVersion_SkippedByRuleTwo_ButWinsRuleOne
// covers two contrasting behaviors of the same unparseable-version string:
// it cannot be ordered against the bound (so it is silently skipped as a rule
// 2 candidate when something else matches), but it CAN still win via rule 1's
// exact string match, since rule 1 never parses anything.
func TestPickStableDevAndMax_Hint_UnparseableVersion_SkippedByRuleTwo_ButWinsRuleOne(t *testing.T) {
	t.Run("skipped by rule 2 when hint does not match it exactly", func(t *testing.T) {
		versions := []Version{
			v("2.0.0", "2024-01-01T00:00:00Z", false),
			v("weird-legacy-build", "2024-02-01T00:00:00Z", false), // unparseable, cannot be ordered
			v("3.0.0", "2024-03-01T00:00:00Z", false),              // above the bound, excluded
		}
		stable, _, _ := pickStableDevAndMax(versions, "2.5.0")
		if stable.VersionKey.Version != "2.0.0" {
			t.Fatalf("stable=%s, want 2.0.0 (unparseable entry must not win, and 3.0.0 is above the bound)", stable.VersionKey.Version)
		}
	})

	t.Run("wins rule 1 when the hint matches it exactly", func(t *testing.T) {
		versions := []Version{
			v("weird-legacy-build", "2024-02-01T00:00:00Z", false),
			v("3.0.0", "2024-03-01T00:00:00Z", true),
		}
		stable, _, _ := pickStableDevAndMax(versions, "weird-legacy-build")
		if stable.VersionKey.Version != "weird-legacy-build" {
			t.Fatalf("stable=%s, want weird-legacy-build (exact match bypasses PEP 440 parsing entirely)", stable.VersionKey.Version)
		}
	})
}

// TestPickStableDevAndMax_Hint_DoesNotAffectDevOrMax verifies Dev and Max
// selection are computed independently of preferredStable: the same version
// list produces identical Dev and Max regardless of the hint.
func TestPickStableDevAndMax_Hint_DoesNotAffectDevOrMax(t *testing.T) {
	versions := []Version{
		v("1.0.0-rc1", "2024-01-01T00:00:00Z", false),
		v("1.0.0", "2024-02-01T00:00:00Z", false),
		v("2.0.0", "2024-03-01T00:00:00Z", true),
	}

	_, devNoHint, maxNoHint := pickStableDevAndMax(versions, "")
	_, devWithHint, maxWithHint := pickStableDevAndMax(versions, "1.0.0")

	if devNoHint.VersionKey.Version != devWithHint.VersionKey.Version || devNoHint.PublishedAt != devWithHint.PublishedAt {
		t.Fatalf("dev changed with hint: no-hint=%+v with-hint=%+v", devNoHint, devWithHint)
	}
	if maxNoHint.VersionKey.Version != maxWithHint.VersionKey.Version || maxNoHint.PublishedAt != maxWithHint.PublishedAt {
		t.Fatalf("max changed with hint: no-hint=%+v with-hint=%+v", maxNoHint, maxWithHint)
	}
	if devWithHint.VersionKey.Version != "1.0.0-rc1" {
		t.Fatalf("dev=%s, want 1.0.0-rc1", devWithHint.VersionKey.Version)
	}
	if maxWithHint.VersionKey.Version != "2.0.0" {
		t.Fatalf("max=%s, want 2.0.0", maxWithHint.VersionKey.Version)
	}
}

// TestPickStableDevAndMax_EmptyVersionList covers the empty-input boundary:
// no versions at all -> all three results are zero Values, regardless of
// hint.
func TestPickStableDevAndMax_EmptyVersionList(t *testing.T) {
	tests := []struct {
		name string
		hint string
	}{
		{name: "no hint", hint: ""},
		{name: "with hint", hint: "1.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stable, dev, max := pickStableDevAndMax(nil, tt.hint)
			if stable.VersionKey.Version != "" {
				t.Fatalf("stable=%s, want empty", stable.VersionKey.Version)
			}
			if dev.VersionKey.Version != "" {
				t.Fatalf("dev=%s, want empty", dev.VersionKey.Version)
			}
			if max.VersionKey.Version != "" {
				t.Fatalf("max=%s, want empty", max.VersionKey.Version)
			}
		})
	}
}

// TestPickStableDevAndMax_Hint_PEP440ShortFormPreReleases covers the PEP 440
// short-form spellings `1.0a1`, `1.0b1`, `1.0c1` (which pep440.Parse
// normalizes to `1.0rc1` — verified directly against the library before this
// assertion was written) and `1.0.dev1`. ADR-0023 is explicit that
// purl.IsStableVersion is deliberately NOT used inside the bound because it
// wrongly classifies these short forms as stable; this test proves the bound
// rule does not exclude them for "looking like" a pre-release. Each is used
// alone in the version list with a generous bound, so it is the only
// candidate <= the bound and must be selected verbatim (including its
// original, non-normalized spelling in VersionKey.Version).
func TestPickStableDevAndMax_Hint_PEP440ShortFormPreReleases(t *testing.T) {
	tests := []struct {
		name    string
		version string
	}{
		{name: "a-release short form", version: "1.0a1"},
		{name: "b-release short form", version: "1.0b1"},
		{name: "c-release short form (normalizes to rc)", version: "1.0c1"},
		{name: "dev release", version: "1.0.dev1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			versions := []Version{
				v(tt.version, "2024-01-01T00:00:00Z", false),
			}
			stable, _, _ := pickStableDevAndMax(versions, "5.0")
			if stable.VersionKey.Version != tt.version {
				t.Fatalf("stable=%q, want %q (not excluded merely for being a pre-release form)", stable.VersionKey.Version, tt.version)
			}
		})
	}
}

// TestPickStableDevAndMax_Hint_PEP440ShortFormPreReleases_OrderedAmongThemselves
// verifies the relative PEP 440 ordering the bound rule relies on for these
// short forms when several compete for the same bound: verified directly
// against the pep440 library beforehand, the order is
// dev1 < a1 < b1 < c1(==rc1), so with all four <= the bound, c1 (the
// greatest) wins.
func TestPickStableDevAndMax_Hint_PEP440ShortFormPreReleases_OrderedAmongThemselves(t *testing.T) {
	versions := []Version{
		v("1.0.dev1", "2024-01-01T00:00:00Z", false),
		v("1.0a1", "2024-01-02T00:00:00Z", false),
		v("1.0b1", "2024-01-03T00:00:00Z", false),
		v("1.0c1", "2024-01-04T00:00:00Z", false),
	}
	stable, _, _ := pickStableDevAndMax(versions, "1.0")
	if stable.VersionKey.Version != "1.0c1" {
		t.Fatalf("stable=%s, want 1.0c1 (greatest of dev1 < a1 < b1 < c1, all < the 1.0 bound)", stable.VersionKey.Version)
	}
}

// TestPickStableDevAndMax_Hint_AllVersionsUnparseable covers: preferredStable
// is set and PEP 440 parseable, but every candidate version string fails
// PEP 440 parsing and none matches the hint exactly. Rule 2 finds nothing to
// order, and rule 1's exact match also fails, so the bound still governs
// (governs=true) and Stable is left empty.
func TestPickStableDevAndMax_Hint_AllVersionsUnparseable(t *testing.T) {
	versions := []Version{
		v("release-2024-a", "2024-01-01T00:00:00Z", true),
		v("build-xyz-dev", "2024-02-01T00:00:00Z", false),
	}
	stable, dev, max := pickStableDevAndMax(versions, "1.0.0")
	if stable.VersionKey.Version != "" {
		t.Fatalf("stable=%s, want empty (no candidate is PEP 440 orderable or an exact match)", stable.VersionKey.Version)
	}
	if dev.VersionKey.Version != "build-xyz-dev" {
		t.Fatalf("dev=%s, want build-xyz-dev (Dev is unaffected: latest non-stable by purl.IsStableVersion)", dev.VersionKey.Version)
	}
	if max.VersionKey.Version == "" {
		t.Fatalf("max should be populated by the PublishedAt fallback when nothing is valid SemVer")
	}
}

// TestPickStableDevAndMax_Hint_DuplicateVersionStrings_LatestPublishedAtWins
// covers rule 1's exact-match tie-break: when the same version string
// appears more than once (duplicate ingestion, or the same release recorded
// under two registries), the entry with the LATER PublishedAt wins. A second
// sub-case pins the behavior when PublishedAt is exactly equal: any one of
// the duplicates is acceptable since they are indistinguishable, but the
// picked Version string must still be correct.
func TestPickStableDevAndMax_Hint_DuplicateVersionStrings_LatestPublishedAtWins(t *testing.T) {
	t.Run("different PublishedAt: later wins", func(t *testing.T) {
		versions := []Version{
			v("1.0.0", "2024-01-01T00:00:00Z", false),
			v("1.0.0", "2024-06-01T00:00:00Z", true),
		}
		picked, governs := pickByRegistryStable(versions, "1.0.0")
		if !governs {
			t.Fatalf("governs=false, want true (exact match always governs)")
		}
		if !picked.PublishedAt.Equal(mustParseRFC3339(t, "2024-06-01T00:00:00Z")) {
			t.Fatalf("picked.PublishedAt=%s, want 2024-06-01T00:00:00Z (the later duplicate)", picked.PublishedAt)
		}
	})

	t.Run("equal PublishedAt: version string is still correct", func(t *testing.T) {
		versions := []Version{
			v("1.0.0", "2024-01-01T00:00:00Z", false),
			v("1.0.0", "2024-01-01T00:00:00Z", true),
		}
		picked, governs := pickByRegistryStable(versions, "1.0.0")
		if !governs {
			t.Fatalf("governs=false, want true (exact match always governs)")
		}
		if picked.VersionKey.Version != "1.0.0" {
			t.Fatalf("picked=%s, want 1.0.0", picked.VersionKey.Version)
		}
	})
}

// TestPickStableDevAndMax_Hint_PEP440EquivalentSpellingsTie_EqualTimestamps_Deterministic
// covers betterStableCandidate's final tie-break: two PEP 440-equal-ordered
// candidates (neither strictly greater under Compare) with identical
// PublishedAt must resolve deterministically by the greater version string,
// not by input order.
func TestPickStableDevAndMax_Hint_PEP440EquivalentSpellingsTie_EqualTimestamps_Deterministic(t *testing.T) {
	forward := []Version{
		v("1.0RC1", "2024-01-01T00:00:00Z", false),
		v("1.0rc1", "2024-01-01T00:00:00Z", false),
	}
	reversed := []Version{
		v("1.0rc1", "2024-01-01T00:00:00Z", false),
		v("1.0RC1", "2024-01-01T00:00:00Z", false),
	}

	stableForward, _, _ := pickStableDevAndMax(forward, "2.0.0")
	stableReversed, _, _ := pickStableDevAndMax(reversed, "2.0.0")

	if stableForward.VersionKey.Version != stableReversed.VersionKey.Version {
		t.Fatalf("order-dependent result: forward=%s reversed=%s", stableForward.VersionKey.Version, stableReversed.VersionKey.Version)
	}
	// "1.0rc1" > "1.0RC1" as a Go string (lowercase 'r' > uppercase 'R' in ASCII).
	if stableForward.VersionKey.Version != "1.0rc1" {
		t.Fatalf("stable=%s, want 1.0rc1 (greater version string breaks the tie deterministically)", stableForward.VersionKey.Version)
	}
}

// TestPickStableDevAndMax_Hint_PermutationIndependent is a general
// permutation-independence guard for pickByRegistryStable: reversing the
// input slice order must not change the selected Version. This complements
// the narrower PEP-440-tie test above by using a version set where ordering,
// PublishedAt, and version-string tie-breaks are all distinct, so any
// accidental dependency on slice position would be caught.
func TestPickStableDevAndMax_Hint_PermutationIndependent(t *testing.T) {
	versions := []Version{
		v("1.0.0", "2024-01-01T00:00:00Z", false),
		v("1.5.0", "2024-02-01T00:00:00Z", false),
		v("2.0.0", "2024-03-01T00:00:00Z", true),
		v("3.0.0", "2024-04-01T00:00:00Z", false), // above the bound, must never win
	}
	reversed := make([]Version, len(versions))
	for i, ver := range versions {
		reversed[len(versions)-1-i] = ver
	}

	stableForward, _, _ := pickStableDevAndMax(versions, "2.0.0")
	stableReversed, _, _ := pickStableDevAndMax(reversed, "2.0.0")

	if stableForward.VersionKey.Version != "2.0.0" {
		t.Fatalf("stableForward=%s, want 2.0.0", stableForward.VersionKey.Version)
	}
	if stableReversed.VersionKey.Version != stableForward.VersionKey.Version {
		t.Fatalf("permutation changed the result: forward=%s reversed=%s", stableForward.VersionKey.Version, stableReversed.VersionKey.Version)
	}
}

// Note: "hint not PEP 440 parseable but exactly present in the list -> still
// selected" is already covered by
// TestPickStableDevAndMax_Hint_UnparseableVersion_SkippedByRuleTwo_ButWinsRuleOne's
// "wins rule 1 when the hint matches it exactly" subtest above — rule 1 runs
// before any PEP 440 parsing, so an unparseable hint that matches a list
// entry verbatim still wins. See ADR-0023.

// mustParseRFC3339 is a test helper that fails the test on a parse error
// instead of silently discarding it (per .claude/rules/error-handling.md
// rule 5: never silently discard errors).
func mustParseRFC3339(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("mustParseRFC3339(%q): %v", s, err)
	}
	return tm
}
