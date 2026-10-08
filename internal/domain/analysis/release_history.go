package analysis

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// Registry name recorded in ReleaseHistory.Registry.
const RegistryNpm = "npm"

// DormantBurstWindow is how close together releases must be published to count
// as one burst. Hijacked dormant packages put every line out within minutes
// (node-ipc 55 seconds, rc 28 seconds, coa 46 minutes, is about 7 hours); the
// window is wide enough for the slowest of these. See ADR-0026.
const DormantBurstWindow = 24 * time.Hour

// DormantBurstMinLines is how many release lines a burst must touch; a
// prerelease touches none. A single
// release after a long silence is common and legitimate — a quarter of the
// entries in two real lockfiles were such releases — while a release on two or
// more lines at once after the same silence was about 1% of them. See ADR-0026.
const DormantBurstMinLines = 2

// ReleaseHistory is the registry's record of when each version of a package was
// published, including versions that have since been removed.
//
// A nil pointer means the record was not obtained — the ecosystem is not one we
// ask, the client is unwired, the package was not found, or the request failed.
type ReleaseHistory struct {
	// Registry names the source: RegistryNpm.
	Registry string
	// PublishedAt maps every version the registry has a publish time for. npm
	// keeps the time of a version after the version itself is unpublished, so
	// this can be a superset of the installable versions.
	PublishedAt map[string]time.Time
	// Installable is the set of versions the registry still serves.
	Installable map[string]struct{}
}

// DormantBurst describes a release that came out after a long silence together
// with releases on other lines — the shape left by a takeover of a dormant
// package, where the attacker publishes to every line so that each semver range
// in use picks up the new version.
type DormantBurst struct {
	// Version is the version being assessed.
	Version string
	// PublishedAt is when Version was published.
	PublishedAt time.Time
	// SilentDays is the number of days between the previous release and the
	// first release of the burst.
	SilentDays int
	// PreviousPublishedAt is when the release before the burst came out.
	PreviousPublishedAt time.Time
	// Versions lists every release in the burst, Version included, oldest first.
	Versions []string
	// Lines lists the distinct release lines the burst touched (major, or
	// "0.minor" below 1.0), in ascending order.
	Lines []string
	// Removed is true when the registry no longer serves Version. This is known
	// only after the fact and is reported for display, never used to decide.
	Removed bool
}

// DetectDormantBurst reports whether version was published as part of a burst
// that followed at least minSilence without any release and touched at least
// DormantBurstMinLines release lines. A release older than maxAge at now is not
// reported: the signal describes the moment of publication, and a version that
// has stayed published for that long is no longer that moment. Returns nil when
// the history is missing, the version has no publish time, or the condition is
// not met.
//
// The burst starts at the earliest release reachable from version through gaps
// of at most DormantBurstWindow, and holds every release published within
// DormantBurstWindow of that start. Releases after version count, so a burst
// becomes visible once its second line is published, not at the first.
func DetectDormantBurst(h *ReleaseHistory, version string, now time.Time, minSilence, maxAge time.Duration) *DormantBurst {
	if h == nil || version == "" {
		return nil
	}
	at, ok := h.PublishedAt[version]
	if !ok || at.IsZero() {
		return nil
	}
	if maxAge > 0 && now.Sub(at) > maxAge {
		return nil
	}

	type rel struct {
		v  string
		at time.Time
	}
	all := make([]rel, 0, len(h.PublishedAt))
	for v, t := range h.PublishedAt {
		if t.IsZero() {
			continue
		}
		all = append(all, rel{v, t})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].at.Equal(all[j].at) {
			return all[i].v < all[j].v
		}
		return all[i].at.Before(all[j].at)
	})
	idx := -1
	for i, r := range all {
		if r.v == version {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	first := idx
	for first > 0 && all[first].at.Sub(all[first-1].at) <= DormantBurstWindow {
		first--
	}
	// The burst is bounded by DormantBurstWindow from its first release, so a
	// package that resumes regular releases does not grow one unbounded burst.
	end := all[first].at.Add(DormantBurstWindow)
	if at.After(end) {
		return nil
	}
	last := idx
	for last+1 < len(all) && !all[last+1].at.After(end) {
		last++
	}
	if first == 0 {
		// No release before the burst: this is the package's first publication,
		// not a return from silence.
		return nil
	}
	prev := all[first-1].at
	silence := all[first].at.Sub(prev)
	if silence < minSilence {
		return nil
	}

	versions := make([]string, 0, last-first+1)
	lineSet := map[string]struct{}{}
	for _, r := range all[first : last+1] {
		versions = append(versions, r.v)
		// A prerelease opens no line: "^2" never resolves to "2.0.0-alpha.0",
		// so a patch plus a next-major preview reaches one set of users.
		if isPrerelease(r.v) {
			continue
		}
		if l, ok := releaseLine(r.v); ok {
			lineSet[l] = struct{}{}
		}
	}
	if len(lineSet) < DormantBurstMinLines {
		return nil
	}
	lines := make([]string, 0, len(lineSet))
	for l := range lineSet {
		lines = append(lines, l)
	}
	sort.Slice(lines, func(i, j int) bool { return lineLess(lines[i], lines[j]) })

	_, installable := h.Installable[version]
	return &DormantBurst{
		Version:             version,
		PublishedAt:         at,
		SilentDays:          int(silence.Hours() / 24),
		PreviousPublishedAt: prev,
		Versions:            versions,
		Lines:               lines,
		Removed:             h.Installable != nil && !installable,
	}
}

// releaseLine returns the semver release line of v: the major version, or
// "0.minor" for versions below 1.0, where each minor is its own line under
// caret ranges. ok is false when v does not start with a numeric major.
func releaseLine(v string) (string, bool) {
	parts := strings.SplitN(strings.TrimPrefix(v, "v"), ".", 3)
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return "", false
	}
	if major != 0 {
		return strconv.Itoa(major), true
	}
	if len(parts) < 2 {
		return "", false
	}
	minorDigits := parts[1]
	if i := strings.IndexFunc(minorDigits, func(r rune) bool { return r < '0' || r > '9' }); i >= 0 {
		minorDigits = minorDigits[:i]
	}
	minor, err := strconv.Atoi(minorDigits)
	if err != nil {
		return "", false
	}
	return "0." + strconv.Itoa(minor), true
}

// isPrerelease reports whether v carries a semver prerelease suffix
// ("2.0.0-alpha.0"). Build metadata ("+build") alone is not a prerelease.
func isPrerelease(v string) bool {
	core, _, _ := strings.Cut(v, "+")
	return strings.Contains(core, "-")
}

// lineLess orders release lines numerically ("0.2" < "0.10" < "1" < "9" < "12").
func lineLess(a, b string) bool {
	pa, pb := lineKey(a), lineKey(b)
	if pa[0] != pb[0] {
		return pa[0] < pb[0]
	}
	return pa[1] < pb[1]
}

func lineKey(l string) [2]int {
	if rest, ok := strings.CutPrefix(l, "0."); ok {
		minor, _ := strconv.Atoi(rest)
		return [2]int{0, minor}
	}
	major, _ := strconv.Atoi(l)
	return [2]int{major, 0}
}
