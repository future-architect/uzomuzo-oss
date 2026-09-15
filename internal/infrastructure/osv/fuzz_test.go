package osv

import (
	"encoding/json"
	"fmt"
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

		// Duplicate IDs are legal input, and the classifier may admit a later
		// record carrying an ID an earlier one also carries. The invariant is
		// that SOME record with the cited ID is admissible, not that the first
		// one is.
		admissible := false
		var why string
		for i := range recs {
			if recs[i].ID != state.AdvisoryID {
				continue
			}
			if err := checkAdmissible(&recs[i], name, now); err != nil {
				why = err.Error()
				continue
			}
			admissible = true
			break
		}
		if !admissible {
			if why == "" {
				t.Fatalf("verdict cites %q, which is not in the input", state.AdvisoryID)
			}
			t.Fatalf("verdict cites %q, but no record with that ID is admissible: %s", state.AdvisoryID, why)
		}
	})
}

// checkAdmissible restates every admission rule ClassifyUnmaintained applies,
// so a positive verdict can be checked against the input that produced it.
func checkAdmissible(rec *domain.AdvisoryRecord, name string, now time.Time) error {
	if !strings.HasPrefix(rec.ID, "RUSTSEC-") {
		return fmt.Errorf("non-RustSec advisory %q", rec.ID)
	}
	if rec.Withdrawn {
		return fmt.Errorf("advisory %q is withdrawn", rec.ID)
	}
	if rec.Published.IsZero() || now.Sub(rec.Published) < domain.UnmaintainedCooldown {
		return fmt.Errorf("advisory %q is inside the cooldown (published %v)", rec.ID, rec.Published)
	}
	matched := 0
	for _, af := range rec.Affected {
		if !strings.EqualFold(strings.TrimSpace(af.Name), strings.TrimSpace(name)) ||
			!strings.EqualFold(strings.TrimSpace(af.Ecosystem), "crates.io") {
			continue
		}
		matched++
		if af.Informational != domain.InformationalUnmaintained {
			return fmt.Errorf("advisory %q carries informational %q", rec.ID, af.Informational)
		}
		if len(af.Versions) > 0 {
			return fmt.Errorf("advisory %q carries an explicit versions list", rec.ID)
		}
		if len(af.Ranges) == 0 {
			return fmt.Errorf("advisory %q carries no ranges", rec.ID)
		}
		for _, r := range af.Ranges {
			if r.Type != "SEMVER" {
				return fmt.Errorf("advisory %q carries range type %q", rec.ID, r.Type)
			}
			introduced := 0
			for _, ev := range r.Events {
				if ev.Fixed != "" || ev.LastAffected != "" || ev.Limit != "" {
					return fmt.Errorf("advisory %q carries a bounded range %+v", rec.ID, ev)
				}
				if ev.Introduced != "" {
					introduced++
				}
			}
			if introduced != 1 {
				return fmt.Errorf("advisory %q carries %d introduced events", rec.ID, introduced)
			}
		}
	}
	if matched == 0 {
		return fmt.Errorf("advisory %q names no affected entry for %q", rec.ID, name)
	}
	return nil
}
