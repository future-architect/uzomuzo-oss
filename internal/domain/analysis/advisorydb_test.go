package analysis

import (
	"slices"
	"testing"
	"time"
)

// unmaintainedNow is the fixed evaluation clock for every case in this file,
// so the cooldown boundary is deterministic.
var unmaintainedNow = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// goodAtty builds a known-good RUSTSEC advisory that admits: not withdrawn,
// published well past the cooldown, one affected entry naming atty on
// crates.io, informational "unmaintained", one SEMVER range with a single
// introduced:"0" event and no other events. Each test case states only its
// own deviation from this shape.
func goodAtty() AdvisoryRecord {
	return AdvisoryRecord{
		ID:        "RUSTSEC-2024-0375",
		Summary:   "atty is unmaintained",
		Reference: "https://rustsec.org/advisories/RUSTSEC-2024-0375",
		Published: unmaintainedNow.AddDate(0, 0, -30),
		Withdrawn: false,
		Affected: []AdvisoryAffected{
			{
				Ecosystem:     "crates.io",
				Name:          "atty",
				Informational: InformationalUnmaintained,
				Ranges: []AdvisoryRange{
					{
						Type: "SEMVER",
						Events: []AdvisoryRangeEvent{
							{Introduced: "0"},
						},
					},
				},
			},
		},
	}
}

func TestClassifyUnmaintained_PackageWideQuantifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rec  func() AdvisoryRecord
		want bool
	}{
		{
			name: "one range, single introduced 0, no other events",
			rec:  goodAtty,
			want: true,
		},
		{
			name: "one range, single introduced 0.0.0-0",
			rec: func() AdvisoryRecord {
				r := goodAtty()
				r.Affected[0].Ranges[0].Events[0].Introduced = "0.0.0-0"
				return r
			},
			want: true,
		},
		{
			name: "introduced 0 plus a fixed event",
			rec: func() AdvisoryRecord {
				r := goodAtty()
				r.Affected[0].Ranges[0].Events = append(r.Affected[0].Ranges[0].Events,
					AdvisoryRangeEvent{Fixed: "0.17.0"})
				return r
			},
			want: false,
		},
		{
			name: "introduced 0 plus a last_affected event",
			rec: func() AdvisoryRecord {
				r := goodAtty()
				r.Affected[0].Ranges[0].Events = append(r.Affected[0].Ranges[0].Events,
					AdvisoryRangeEvent{LastAffected: "0.2.14"})
				return r
			},
			want: false,
		},
		{
			name: "introduced 0 plus a limit event",
			rec: func() AdvisoryRecord {
				r := goodAtty()
				r.Affected[0].Ranges[0].Events = append(r.Affected[0].Ranges[0].Events,
					AdvisoryRangeEvent{Limit: "1.0.0"})
				return r
			},
			want: false,
		},
		{
			// An OSV event this code does not recognize reaches the domain as
			// an event with every field empty; it must not be skipped over.
			name: "introduced 0 plus an event with no recognized field",
			rec: func() AdvisoryRecord {
				r := goodAtty()
				r.Affected[0].Ranges[0].Events = append(r.Affected[0].Ranges[0].Events,
					AdvisoryRangeEvent{})
				return r
			},
			want: false,
		},
		{
			name: "range with two introduced events",
			rec: func() AdvisoryRecord {
				r := goodAtty()
				r.Affected[0].Ranges[0].Events = append(r.Affected[0].Ranges[0].Events,
					AdvisoryRangeEvent{Introduced: "0"})
				return r
			},
			want: false,
		},
		{
			name: "single introduced 1.0.0, non-zero",
			rec: func() AdvisoryRecord {
				r := goodAtty()
				r.Affected[0].Ranges[0].Events[0].Introduced = "1.0.0"
				return r
			},
			want: false,
		},
		{
			name: "two ranges, one open-ended and one carrying fixed",
			rec: func() AdvisoryRecord {
				r := goodAtty()
				r.Affected[0].Ranges = append(r.Affected[0].Ranges, AdvisoryRange{
					Type: "SEMVER",
					Events: []AdvisoryRangeEvent{
						{Introduced: "0"},
						{Fixed: "0.17.0"},
					},
				})
				return r
			},
			want: false,
		},
		{
			name: "two affected entries both naming atty, one open-ended and one bounded",
			rec: func() AdvisoryRecord {
				r := goodAtty()
				r.Affected = append(r.Affected, AdvisoryAffected{
					Ecosystem:     "crates.io",
					Name:          "atty",
					Informational: InformationalUnmaintained,
					Ranges: []AdvisoryRange{
						{
							Type: "SEMVER",
							Events: []AdvisoryRangeEvent{
								{Introduced: "0"},
								{Fixed: "0.17.0"},
							},
						},
					},
				})
				return r
			},
			want: false,
		},
		{
			name: "affected entry carries a non-empty Versions list",
			rec: func() AdvisoryRecord {
				r := goodAtty()
				r.Affected[0].Versions = []string{"0.2.14"}
				return r
			},
			want: false,
		},
		{
			name: "range Type ECOSYSTEM",
			rec: func() AdvisoryRecord {
				r := goodAtty()
				r.Affected[0].Ranges[0].Type = "ECOSYSTEM"
				return r
			},
			want: false,
		},
		{
			name: "range Type GIT",
			rec: func() AdvisoryRecord {
				r := goodAtty()
				r.Affected[0].Ranges[0].Type = "GIT"
				return r
			},
			want: false,
		},
		{
			name: "affected entry with no ranges at all",
			rec: func() AdvisoryRecord {
				r := goodAtty()
				r.Affected[0].Ranges = nil
				return r
			},
			want: false,
		},
		{
			name: "affected entry with an empty ranges slice",
			rec: func() AdvisoryRecord {
				r := goodAtty()
				r.Affected[0].Ranges = []AdvisoryRange{}
				return r
			},
			want: false,
		},
		{
			name: "a bounded entry for a different package (ring) alongside an open-ended atty entry",
			rec: func() AdvisoryRecord {
				r := goodAtty()
				r.Affected = append(r.Affected, AdvisoryAffected{
					Ecosystem:     "crates.io",
					Name:          "ring",
					Informational: InformationalUnmaintained,
					Ranges: []AdvisoryRange{
						{
							Type: "SEMVER",
							Events: []AdvisoryRangeEvent{
								{Introduced: "0"},
								{Fixed: "0.17.0"},
							},
						},
					},
				})
				return r
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyUnmaintained([]AdvisoryRecord{tt.rec()}, "crates.io", "atty", unmaintainedNow)
			if got.Unmaintained != tt.want {
				t.Fatalf("Unmaintained: got %v, want %v", got.Unmaintained, tt.want)
			}
		})
	}
}

func TestClassifyUnmaintained_AdmissionFilters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		recs func() []AdvisoryRecord
		want bool
	}{
		{
			name: "Informational unsound",
			recs: func() []AdvisoryRecord {
				r := goodAtty()
				r.Affected[0].Informational = "unsound"
				return []AdvisoryRecord{r}
			},
			want: false,
		},
		{
			name: "Informational notice",
			recs: func() []AdvisoryRecord {
				r := goodAtty()
				r.Affected[0].Informational = "notice"
				return []AdvisoryRecord{r}
			},
			want: false,
		},
		{
			name: "Informational empty",
			recs: func() []AdvisoryRecord {
				r := goodAtty()
				r.Affected[0].Informational = ""
				return []AdvisoryRecord{r}
			},
			want: false,
		},
		{
			name: "two affected entries naming atty, one unmaintained and one unsound",
			recs: func() []AdvisoryRecord {
				r := goodAtty()
				r.Affected = append(r.Affected, AdvisoryAffected{
					Ecosystem:     "crates.io",
					Name:          "atty",
					Informational: "unsound",
					Ranges: []AdvisoryRange{
						{
							Type:   "SEMVER",
							Events: []AdvisoryRangeEvent{{Introduced: "0"}},
						},
					},
				})
				return []AdvisoryRecord{r}
			},
			want: false,
		},
		{
			name: "non-RUSTSEC provenance (GHSA) carrying the marker",
			recs: func() []AdvisoryRecord {
				r := goodAtty()
				r.ID = "GHSA-g98v-hv3f-hcfr"
				return []AdvisoryRecord{r}
			},
			want: false,
		},
		{
			name: "Withdrawn true",
			recs: func() []AdvisoryRecord {
				r := goodAtty()
				r.Withdrawn = true
				return []AdvisoryRecord{r}
			},
			want: false,
		},
		{
			name: "marker present only on an affected entry naming a different package",
			recs: func() []AdvisoryRecord {
				r := goodAtty()
				r.Affected[0].Name = "ring"
				return []AdvisoryRecord{r}
			},
			want: false,
		},
		{
			name: "affected entry ecosystem is npm instead of crates.io",
			recs: func() []AdvisoryRecord {
				r := goodAtty()
				r.Affected[0].Ecosystem = "npm"
				return []AdvisoryRecord{r}
			},
			want: false,
		},
		{
			name: "ecosystem and name matching is case-insensitive",
			recs: func() []AdvisoryRecord {
				r := goodAtty()
				r.Affected[0].Ecosystem = "Crates.IO"
				r.Affected[0].Name = "ATTY"
				return []AdvisoryRecord{r}
			},
			want: true,
		},
		{
			name: "empty recs slice",
			recs: func() []AdvisoryRecord {
				return []AdvisoryRecord{}
			},
			want: false,
		},
		{
			name: "nil recs slice",
			recs: func() []AdvisoryRecord {
				return nil
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyUnmaintained(tt.recs(), "crates.io", "atty", unmaintainedNow)
			if got.Unmaintained != tt.want {
				t.Fatalf("Unmaintained: got %v, want %v", got.Unmaintained, tt.want)
			}
		})
	}
}

func TestClassifyUnmaintained_CooldownBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		published func() time.Time
		want      bool
	}{
		{
			name:      "published exactly UnmaintainedCooldown before now",
			published: func() time.Time { return unmaintainedNow.Add(-UnmaintainedCooldown) },
			want:      true,
		},
		{
			name:      "published one second less than UnmaintainedCooldown before now",
			published: func() time.Time { return unmaintainedNow.Add(-UnmaintainedCooldown + time.Second) },
			want:      false,
		},
		{
			name:      "published one second more than UnmaintainedCooldown before now",
			published: func() time.Time { return unmaintainedNow.Add(-UnmaintainedCooldown - time.Second) },
			want:      true,
		},
		{
			name:      "published in the future",
			published: func() time.Time { return unmaintainedNow.Add(time.Hour) },
			want:      false,
		},
		{
			name:      "zero Published",
			published: func() time.Time { return time.Time{} },
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := goodAtty()
			r.Published = tt.published()
			got := ClassifyUnmaintained([]AdvisoryRecord{r}, "crates.io", "atty", unmaintainedNow)
			if got.Unmaintained != tt.want {
				t.Fatalf("Unmaintained: got %v, want %v", got.Unmaintained, tt.want)
			}
		})
	}
}

// TestClassifyUnmaintained_DeterministicWinner pins that, given two qualifying
// advisories, the lexicographically smaller AdvisoryID always wins, in either
// input order, and that all three evidence fields come from that same record.
func TestClassifyUnmaintained_DeterministicWinner(t *testing.T) {
	t.Parallel()

	other := goodAtty()
	other.ID = "RUSTSEC-2024-0375"
	other.Reference = "https://rustsec.org/advisories/RUSTSEC-2024-0375"
	other.Published = unmaintainedNow.AddDate(0, 0, -30)

	winner := goodAtty()
	winner.ID = "RUSTSEC-2021-0999"
	winner.Reference = "https://rustsec.org/advisories/RUSTSEC-2021-0999"
	winner.Published = unmaintainedNow.AddDate(0, 0, -60)

	orders := []struct {
		name string
		recs []AdvisoryRecord
	}{
		{name: "winner first", recs: []AdvisoryRecord{winner, other}},
		{name: "winner second", recs: []AdvisoryRecord{other, winner}},
	}

	for _, o := range orders {
		t.Run(o.name, func(t *testing.T) {
			t.Parallel()
			got := ClassifyUnmaintained(o.recs, "crates.io", "atty", unmaintainedNow)
			if got.AdvisoryID != winner.ID {
				t.Fatalf("AdvisoryID: got %q, want %q", got.AdvisoryID, winner.ID)
			}
			if got.Reference != winner.Reference {
				t.Fatalf("Reference: got %q, want %q", got.Reference, winner.Reference)
			}
			if !got.Published.Equal(winner.Published) {
				t.Fatalf("Published: got %v, want %v", got.Published, winner.Published)
			}
			// Every admitted marker is kept, not only the winner, so the
			// assessor can stop counting each of them as a vulnerability.
			if want := []string{winner.ID, other.ID}; !slices.Equal(got.MarkerIDs, want) {
				t.Fatalf("MarkerIDs: got %v, want %v", got.MarkerIDs, want)
			}
		})
	}
}

// TestClassifyUnmaintained_ReturnedEvidence pins every field of the returned
// AdvisoryDBState, on both a match and a no-match.
func TestClassifyUnmaintained_ReturnedEvidence(t *testing.T) {
	t.Parallel()

	t.Run("match: every field is populated from the winning record", func(t *testing.T) {
		t.Parallel()
		r := goodAtty()
		r.Summary = "  Crate is unmaintained.\n  See advisory.  "
		got := ClassifyUnmaintained([]AdvisoryRecord{r}, "crates.io", "atty", unmaintainedNow)

		if !got.Unmaintained {
			t.Fatalf("Unmaintained: got false, want true")
		}
		if got.AdvisoryID != r.ID {
			t.Errorf("AdvisoryID: got %q, want %q", got.AdvisoryID, r.ID)
		}
		wantSummary := "Crate is unmaintained. See advisory."
		if got.Summary != wantSummary {
			t.Errorf("Summary: got %q, want %q", got.Summary, wantSummary)
		}
		if got.Reference != r.Reference {
			t.Errorf("Reference: got %q, want %q", got.Reference, r.Reference)
		}
		if !got.Published.Equal(r.Published) {
			t.Errorf("Published: got %v, want %v", got.Published, r.Published)
		}
		if want := []string{r.ID}; !slices.Equal(got.MarkerIDs, want) {
			t.Errorf("MarkerIDs: got %v, want %v", got.MarkerIDs, want)
		}
	})

	t.Run("match: aliases of an admitted marker are marker IDs too", func(t *testing.T) {
		t.Parallel()
		// deps.dev lists wee_alloc@0.4.5's marker under both identities.
		r := goodAtty()
		r.ID = "RUSTSEC-2022-0054"
		r.Aliases = []string{"GHSA-rc23-xxgq-x27g", "RUSTSEC-2022-0054"}
		got := ClassifyUnmaintained([]AdvisoryRecord{r}, "crates.io", "atty", unmaintainedNow)

		if want := []string{"GHSA-rc23-xxgq-x27g", "RUSTSEC-2022-0054"}; !slices.Equal(got.MarkerIDs, want) {
			t.Errorf("MarkerIDs: got %v, want %v (sorted, deduplicated)", got.MarkerIDs, want)
		}
		if got.AdvisoryID != "RUSTSEC-2022-0054" {
			t.Errorf("AdvisoryID: got %q, want the RustSec record's own ID", got.AdvisoryID)
		}
	})

	t.Run("match: duplicate IDs pick the same evidence in either order", func(t *testing.T) {
		t.Parallel()
		first := goodAtty()
		first.Summary = "b summary"
		first.Reference = "https://rustsec.org/b.html"
		second := goodAtty()
		second.Summary = "a summary"
		second.Reference = "https://rustsec.org/a.html"

		one := ClassifyUnmaintained([]AdvisoryRecord{first, second}, "crates.io", "atty", unmaintainedNow)
		two := ClassifyUnmaintained([]AdvisoryRecord{second, first}, "crates.io", "atty", unmaintainedNow)
		if one.Summary != two.Summary || one.Reference != two.Reference {
			t.Errorf("evidence depends on input order: %q/%q vs %q/%q",
				one.Summary, one.Reference, two.Summary, two.Reference)
		}
	})

	t.Run("match: the summary is sanitized by the classifier itself", func(t *testing.T) {
		t.Parallel()
		// Not every producer of an AdvisoryRecord is the OSV client; the field
		// reaches a terminal, so the domain strips control runes itself.
		r := goodAtty()
		r.Summary = "atty\u001b[31m is\n\n  unmaintained\u200b"
		got := ClassifyUnmaintained([]AdvisoryRecord{r}, "crates.io", "atty", unmaintainedNow)
		if want := "atty[31m is unmaintained"; got.Summary != want {
			t.Errorf("Summary: got %q, want %q", got.Summary, want)
		}
	})

	t.Run("match: an alias set naming another RustSec advisory is not trusted", func(t *testing.T) {
		t.Parallel()
		// failure@0.1.8: the unmaintained marker lists the separate unsound
		// advisory RUSTSEC-2019-0036 and its CVEs and GHSAs as aliases.
		// Trusting them would stop counting a real vulnerability.
		r := goodAtty()
		r.ID = "RUSTSEC-2020-0036"
		r.Aliases = []string{"CVE-2019-25010", "CVE-2020-25575", "GHSA-jq66-xh47-j9f3",
			"GHSA-r98r-j25q-rmpr", "RUSTSEC-2019-0036"}
		got := ClassifyUnmaintained([]AdvisoryRecord{r}, "crates.io", "atty", unmaintainedNow)

		if want := []string{"RUSTSEC-2020-0036"}; !slices.Equal(got.MarkerIDs, want) {
			t.Errorf("MarkerIDs: got %v, want %v", got.MarkerIDs, want)
		}
	})

	t.Run("no match: aliases of a rejected record are not marker IDs", func(t *testing.T) {
		t.Parallel()
		r := goodAtty()
		r.Withdrawn = true
		r.Aliases = []string{"GHSA-rc23-xxgq-x27g"}
		got := ClassifyUnmaintained([]AdvisoryRecord{r}, "crates.io", "atty", unmaintainedNow)
		if got.MarkerIDs != nil {
			t.Errorf("MarkerIDs: got %v, want nil", got.MarkerIDs)
		}
	})

	t.Run("no match: zero-value AdvisoryDBState", func(t *testing.T) {
		t.Parallel()
		r := goodAtty()
		r.Withdrawn = true
		got := ClassifyUnmaintained([]AdvisoryRecord{r}, "crates.io", "atty", unmaintainedNow)

		if got.Unmaintained {
			t.Errorf("Unmaintained: got true, want false")
		}
		if got.AdvisoryID != "" {
			t.Errorf("AdvisoryID: got %q, want empty", got.AdvisoryID)
		}
		if got.MarkerIDs != nil {
			t.Errorf("MarkerIDs: got %v, want nil", got.MarkerIDs)
		}
	})
}
