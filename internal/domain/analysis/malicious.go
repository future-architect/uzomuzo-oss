package analysis

import (
	"regexp"
	"strings"
	"time"
)

// MaliciousStatus distinguishes a completed check from a failed one.
type MaliciousStatus string

const (
	MaliciousStatusClean        MaliciousStatus = "clean"
	MaliciousStatusFlagged      MaliciousStatus = "flagged"
	MaliciousStatusLookupFailed MaliciousStatus = "lookup_failed"
)

// MaliciousScope says whether an advisory covers one version or the whole package.
type MaliciousScope string

const (
	MaliciousScopeVersion MaliciousScope = "version"
	MaliciousScopePackage MaliciousScope = "package"
)

// MaliciousState is the result of checking a package against OSV advisories.
// A nil pointer means the package was not checked. See ADR-0027.
type MaliciousState struct {
	Status     MaliciousStatus
	Malicious  bool
	AdvisoryID string
	Summary    string
	Reference  string
	Published  time.Time
	Scope      MaliciousScope
}

// Malicious reports whether this analysis names a malicious package or version.
func (a *Analysis) Malicious() bool {
	return a != nil && a.MaliciousState != nil && a.MaliciousState.Status == MaliciousStatusFlagged && a.MaliciousState.Malicious
}

// ClassifyMalicious checks admitted advisories against the requested package version.
func ClassifyMalicious(recs []AdvisoryRecord, ecosystem, name, version string) MaliciousState {
	clean := MaliciousState{Status: MaliciousStatusClean}
	for _, rec := range recs {
		if rec.Withdrawn || !maliciousAdvisory(rec) {
			continue
		}
		for _, affected := range rec.Affected {
			if affected.Ecosystem != ecosystem || !sameMaliciousPackageName(ecosystem, affected.Name, name) {
				continue
			}
			scope := affectedMaliciousScope(affected, ecosystem, version)
			if scope != "" {
				return MaliciousState{Status: MaliciousStatusFlagged, Malicious: true, AdvisoryID: rec.ID, Summary: rec.Summary, Reference: rec.Reference, Published: rec.Published, Scope: scope}
			}
		}
	}
	return clean
}

func maliciousAdvisory(rec AdvisoryRecord) bool {
	if strings.HasPrefix(rec.ID, "MAL-") {
		return true
	}
	if !strings.HasPrefix(rec.ID, "GHSA-") {
		return false
	}
	for _, cwe := range rec.CWEIDs {
		if cwe == "CWE-506" {
			return true
		}
	}
	return false
}

func sameMaliciousPackageName(ecosystem, left, right string) bool {
	if ecosystem == "PyPI" {
		return normalizePyPIName(left) == normalizePyPIName(right)
	}
	return left == right
}

func normalizePyPIName(name string) string {
	return pep503Separators.ReplaceAllString(strings.ToLower(name), "-")
}

var pep503Separators = regexp.MustCompile(`[-_.]+`)

func affectedMaliciousScope(a AdvisoryAffected, ecosystem, version string) MaliciousScope {
	for _, r := range a.Ranges {
		if allVersionsRange(r) {
			return MaliciousScopePackage
		}
	}
	if version == "" {
		return ""
	}
	for _, v := range a.Versions {
		if ecosystem == "Go" {
			v = strings.TrimPrefix(v, "v")
			version = strings.TrimPrefix(version, "v")
		}
		if v == version {
			return MaliciousScopeVersion
		}
	}
	for _, r := range a.Ranges {
		if r.Type == "SEMVER" && semverRangeContains(r.Events, version, ecosystem == "Go") {
			return MaliciousScopeVersion
		}
	}
	return ""
}

func allVersionsRange(r AdvisoryRange) bool {
	return (r.Type == "SEMVER" || r.Type == "ECOSYSTEM") && len(r.Events) == 1 && r.Events[0] == (AdvisoryRangeEvent{Introduced: "0"})
}

func semverRangeContains(events []AdvisoryRangeEvent, version string, goVersion bool) bool {
	if goVersion {
		version = strings.TrimPrefix(version, "v")
	}
	want, ok := parseMaliciousSemver(version)
	if !ok || !validMaliciousSemverEvents(events, goVersion) {
		return false
	}
	open := false
	matched := false
	for _, ev := range events {
		fields := 0
		for _, s := range []string{ev.Introduced, ev.Fixed, ev.LastAffected, ev.Limit} {
			if s != "" {
				fields++
			}
		}
		if fields != 1 {
			return false
		}
		if ev.Introduced != "" {
			if open {
				return false
			}
			if ev.Introduced == "0" {
				matched = true
			} else {
				bound, valid := parseMaliciousSemver(stripGoV(ev.Introduced, goVersion))
				if !valid {
					return false
				}
				matched = compareMaliciousSemver(want, bound) >= 0
			}
			open = true
			continue
		}
		if !open {
			return false
		}
		boundText := ev.Fixed
		if ev.LastAffected != "" {
			boundText = ev.LastAffected
		}
		if ev.Limit != "" {
			boundText = ev.Limit
		}
		bound, valid := parseMaliciousSemver(stripGoV(boundText, goVersion))
		if !valid {
			return false
		}
		cmp := compareMaliciousSemver(want, bound)
		if matched && (cmp < 0 || (ev.LastAffected != "" && cmp == 0)) {
			return true
		}
		open, matched = false, false
	}
	return open && matched
}

func validMaliciousSemverEvents(events []AdvisoryRangeEvent, goVersion bool) bool {
	open := false
	for _, ev := range events {
		fields := 0
		for _, s := range []string{ev.Introduced, ev.Fixed, ev.LastAffected, ev.Limit} {
			if s != "" {
				fields++
			}
		}
		if fields != 1 {
			return false
		}
		if ev.Introduced != "" {
			if open {
				return false
			}
			if ev.Introduced != "0" {
				if _, ok := parseMaliciousSemver(stripGoV(ev.Introduced, goVersion)); !ok {
					return false
				}
			}
			open = true
			continue
		}
		if !open {
			return false
		}
		bound := ev.Fixed
		if ev.LastAffected != "" {
			bound = ev.LastAffected
		}
		if ev.Limit != "" {
			bound = ev.Limit
		}
		if _, ok := parseMaliciousSemver(stripGoV(bound, goVersion)); !ok {
			return false
		}
		open = false
	}
	return len(events) != 0
}

func stripGoV(s string, enabled bool) string {
	if enabled {
		return strings.TrimPrefix(s, "v")
	}
	return s
}

type maliciousSemver struct {
	core [3]string
	pre  []string
}

var maliciousSemverPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)

func parseMaliciousSemver(s string) (maliciousSemver, bool) {
	m := maliciousSemverPattern.FindStringSubmatch(s)
	if m == nil {
		return maliciousSemver{}, false
	}
	v := maliciousSemver{core: [3]string{m[1], m[2], m[3]}}
	if m[4] != "" {
		v.pre = strings.Split(m[4], ".")
		for _, p := range v.pre {
			if numericSemverID(p) && len(p) > 1 && p[0] == '0' {
				return maliciousSemver{}, false
			}
		}
	}
	return v, true
}

func numericSemverID(s string) bool {
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func compareNumericString(left, right string) int {
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return strings.Compare(left, right)
}

func compareMaliciousSemver(left, right maliciousSemver) int {
	for i := range left.core {
		if c := compareNumericString(left.core[i], right.core[i]); c != 0 {
			return c
		}
	}
	if len(left.pre) == 0 && len(right.pre) > 0 {
		return 1
	}
	if len(right.pre) == 0 && len(left.pre) > 0 {
		return -1
	}
	for i := 0; i < len(left.pre) && i < len(right.pre); i++ {
		l, r := left.pre[i], right.pre[i]
		ln, rn := numericSemverID(l), numericSemverID(r)
		if ln && !rn {
			return -1
		}
		if rn && !ln {
			return 1
		}
		if ln {
			if c := compareNumericString(l, r); c != 0 {
				return c
			}
		} else if c := strings.Compare(l, r); c != 0 {
			return c
		}
	}
	if len(left.pre) < len(right.pre) {
		return -1
	}
	if len(left.pre) > len(right.pre) {
		return 1
	}
	return 0
}
