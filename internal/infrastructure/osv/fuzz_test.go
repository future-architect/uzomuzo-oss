package osv

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
)

// osvFuzzSeeds are response bodies covering the shapes the decoder must survive:
// the real atty page, a withdrawn advisory, the version-bounded ring marker,
// wrong types where a string is expected, truncation, and deep nesting.
var osvFuzzSeeds = []string{
	attyPage,
	`{"vulns":[]}`,
	`{}`,
	``,
	`null`,
	`[]`,
	`{"vulns":null,"next_page_token":null}`,
	`{"vulns":[{"id":"RUSTSEC-2025-0010","published":"2025-03-05T12:00:00Z",
	  "affected":[{"package":{"name":"ring","ecosystem":"crates.io"},
	   "ranges":[{"type":"SEMVER","events":[{"introduced":"0.0.0-0"},{"fixed":"0.17.0"}]}],
	   "database_specific":{"informational":"unmaintained"}}]}]}`,
	`{"vulns":[{"id":"RUSTSEC-2025-0007","withdrawn":"2025-02-03T12:00:00Z","published":"2025-02-01T12:00:00Z"}]}`,
	`{"vulns":[{"id":123}]}`,
	`{"vulns":[{"id":"X","affected":[{"package":{"name":"a","ecosystem":"crates.io"},"versions":["1.0.0"]}]}]}`,
	`{"vulns":[{"id":"X","affected":[{"package":{"name":"a"},"ranges":[{"type":"GIT","events":[{}]}]}]}]}`,
	`{"vulns":[{"id":"X","summary":"\u0000 \u001b[31m\u200b\n\r"}]}`,
	`{"vulns":[{"id":"RUSTSEC-1","affected":[{"package":{"name":"a","ecosystem":"crates.io"},` +
		`"ranges":[{"type":"SEMVER","events":[{"introduced":"0"}]}],"database_specific":{"informational":"unmaintained"}}]}]}`,
	`{"vulns":[{"id":"RUSTSEC-2024-0375"`,
}

// FuzzDecodeQueryResponse exercises the OSV response decoder on attacker-shaped
// input: it must never panic, and whatever it produces must already be safe to
// print and carry an advisory identity.
func FuzzDecodeQueryResponse(f *testing.F) {
	for _, s := range osvFuzzSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, body string) {
		var resp queryResponse
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			return
		}
		for i := range resp.Vulns {
			rec, ok := toRecord(&resp.Vulns[i])
			if !ok {
				continue
			}
			if strings.TrimSpace(rec.ID) == "" {
				t.Fatalf("toRecord accepted a record with no advisory ID: %q", body)
			}
			for _, r := range rec.Summary {
				if unicode.IsSpace(r) {
					continue
				}
				if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
					t.Fatalf("summary kept a control/format rune %U: %q", r, body)
				}
			}
			if strings.ContainsAny(rec.Summary, "\n\r") {
				t.Fatalf("summary kept a line break: %q", rec.Summary)
			}
			for _, af := range rec.Affected {
				if strings.TrimSpace(af.Name) == "" {
					t.Fatalf("affected entry with no package name survived: %q", body)
				}
			}
		}
	})
}

// FuzzDecodeAndClassify runs the whole path a real response takes — decode, map,
// classify — and asserts the property that matters: a positive verdict implies
// every admission invariant held. This is the property whose earlier, weaker
// form admitted ring's version-bounded marker.
func FuzzDecodeAndClassify(f *testing.F) {
	for _, s := range osvFuzzSeeds {
		f.Add(s, "atty")
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	f.Fuzz(func(t *testing.T, body, name string) {
		var resp queryResponse
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			return
		}
		recs := make([]domain.AdvisoryRecord, 0, len(resp.Vulns))
		for i := range resp.Vulns {
			if rec, ok := toRecord(&resp.Vulns[i]); ok {
				recs = append(recs, rec)
			}
		}
		state := domain.ClassifyUnmaintained(recs, "crates.io", name, now)
		if !state.Unmaintained {
			if state.AdvisoryID != "" {
				t.Fatalf("negative verdict carried evidence %q", state.AdvisoryID)
			}
			return
		}

		src := findRecord(recs, state.AdvisoryID)
		if src == nil {
			t.Fatalf("verdict cites %q, which is not in the input", state.AdvisoryID)
		}
		if !strings.HasPrefix(src.ID, "RUSTSEC-") {
			t.Fatalf("non-RustSec advisory %q was admitted", src.ID)
		}
		if src.Withdrawn {
			t.Fatalf("withdrawn advisory %q was admitted", src.ID)
		}
		if src.Published.IsZero() || now.Sub(src.Published) < domain.UnmaintainedCooldown {
			t.Fatalf("advisory %q was admitted before the cooldown elapsed (published %v)", src.ID, src.Published)
		}
		matched := 0
		for _, af := range src.Affected {
			if !strings.EqualFold(strings.TrimSpace(af.Name), strings.TrimSpace(name)) ||
				!strings.EqualFold(strings.TrimSpace(af.Ecosystem), "crates.io") {
				continue
			}
			matched++
			if af.Informational != domain.InformationalUnmaintained {
				t.Fatalf("advisory %q admitted with informational %q", src.ID, af.Informational)
			}
			if len(af.Versions) > 0 {
				t.Fatalf("advisory %q admitted with an explicit versions list", src.ID)
			}
			if len(af.Ranges) == 0 {
				t.Fatalf("advisory %q admitted with no ranges", src.ID)
			}
			for _, r := range af.Ranges {
				if r.Type != "SEMVER" {
					t.Fatalf("advisory %q admitted with range type %q", src.ID, r.Type)
				}
				introduced := 0
				for _, ev := range r.Events {
					if ev.Fixed != "" || ev.LastAffected != "" || ev.Limit != "" {
						t.Fatalf("advisory %q admitted with a bounded range %+v", src.ID, ev)
					}
					if ev.Introduced != "" {
						introduced++
					}
				}
				if introduced != 1 {
					t.Fatalf("advisory %q admitted with %d introduced events", src.ID, introduced)
				}
			}
		}
		if matched == 0 {
			t.Fatalf("advisory %q was admitted without naming the queried package %q", src.ID, name)
		}
	})
}

func findRecord(recs []domain.AdvisoryRecord, id string) *domain.AdvisoryRecord {
	for i := range recs {
		if recs[i].ID == id {
			return &recs[i]
		}
	}
	return nil
}
