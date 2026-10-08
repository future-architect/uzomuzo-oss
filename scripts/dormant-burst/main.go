// dormant-burst replays DetectDormantBurst against live npm registry data, so
// the numbers in ADR-0026 can be reproduced. It does two things:
//
//   - cases: evaluate each incident version listed in cases.json, at the moment
//     one hour after its publication (what was knowable on the day).
//   - lockfile: evaluate every (name, version) in an npm package-lock.json at a
//     given date, with and without the age limit, and print every hit.
//
// Usage:
//
//	go run ./scripts/dormant-burst cases scripts/dormant-burst/cases.json
//	go run ./scripts/dormant-burst lockfile -now 2026-10-08 path/to/package-lock.json
//
// Registry data drifts (removed versions can lose their "time" entry, new
// releases change the history), so results are dated in the ADR.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/npmjs"
)

const year = 365 * 24 * time.Hour

type testCase struct {
	Package string `json:"package"`
	Version string `json:"version"`
	Kind    string `json:"kind"` // "dormant-takeover", "active-takeover", "maintainer-sabotage"
	Note    string `json:"note"`
	Source  string `json:"source"`
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: dormant-burst cases <cases.json> | lockfile [-now YYYY-MM-DD] <package-lock.json>")
		os.Exit(2)
	}
	ctx := context.Background()
	client := npmjs.NewPackumentClient()
	switch os.Args[1] {
	case "cases":
		runCases(ctx, client, os.Args[2])
	case "lockfile":
		fs := flag.NewFlagSet("lockfile", flag.ExitOnError)
		nowFlag := fs.String("now", time.Now().UTC().Format("2006-01-02"), "evaluation date")
		_ = fs.Parse(os.Args[2:]) // ExitOnError: Parse exits on failure and never returns an error
		now, err := time.Parse("2006-01-02", *nowFlag)
		if err != nil || fs.NArg() != 1 {
			fmt.Fprintln(os.Stderr, "lockfile: need -now YYYY-MM-DD and one path")
			os.Exit(2)
		}
		runLockfile(ctx, client, fs.Arg(0), now)
	default:
		fmt.Fprintln(os.Stderr, "unknown mode", os.Args[1])
		os.Exit(2)
	}
}

func history(ctx context.Context, c *npmjs.Client, name string) (*domain.ReleaseHistory, error) {
	h, found, err := c.GetPublishHistory(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", name, err)
	}
	if !found {
		return nil, fmt.Errorf("%s: not found on npm", name)
	}
	return &domain.ReleaseHistory{Registry: domain.RegistryNpm, PublishedAt: h.PublishedAt, Installable: h.Installable}, nil
}

func runCases(ctx context.Context, c *npmjs.Client, path string) {
	raw, err := os.ReadFile(path)
	must(err)
	var cases []testCase
	must(json.Unmarshal(raw, &cases))
	fmt.Println("| package | version | kind | fires | silent days | lines | burst |")
	fmt.Println("|---|---|---|---|---|---|---|")
	for _, tc := range cases {
		h, err := history(ctx, c, tc.Package)
		if err != nil {
			fmt.Printf("| %s | %s | %s | error: %v | | | |\n", tc.Package, tc.Version, tc.Kind, err)
			continue
		}
		at, ok := h.PublishedAt[tc.Version]
		if !ok {
			fmt.Printf("| %s | %s | %s | no time entry | | | |\n", tc.Package, tc.Version, tc.Kind)
			continue
		}
		// One day after publication: the burst window has closed, and the age
		// limit is irrelevant. Releases published later are in the history but
		// cannot be part of a burst that started before them.
		b := domain.DetectDormantBurst(h, tc.Version, at.Add(24*time.Hour), year, year)
		if b == nil {
			fmt.Printf("| %s | %s | %s | no | | | |\n", tc.Package, tc.Version, tc.Kind)
			continue
		}
		fmt.Printf("| %s | %s | %s | **yes** | %d | %s | %s |\n", tc.Package, tc.Version, tc.Kind,
			b.SilentDays, strings.Join(b.Lines, ", "), strings.Join(b.Versions, ", "))
	}
}

func runLockfile(ctx context.Context, c *npmjs.Client, path string, now time.Time) {
	raw, err := os.ReadFile(path)
	must(err)
	var lock struct {
		Packages map[string]struct {
			Version string `json:"version"`
			Link    bool   `json:"link"`
		} `json:"packages"`
	}
	must(json.Unmarshal(raw, &lock))
	type entry struct{ name, version string }
	seen := map[entry]struct{}{}
	names := map[string]struct{}{}
	for k, p := range lock.Packages {
		i := strings.LastIndex(k, "node_modules/")
		if i < 0 || p.Version == "" || p.Link {
			continue
		}
		e := entry{k[i+len("node_modules/"):], p.Version}
		seen[e] = struct{}{}
		names[e.name] = struct{}{}
	}
	hist := map[string]*domain.ReleaseHistory{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 16)
	for n := range names {
		wg.Add(1)
		sem <- struct{}{}
		go func(n string) {
			defer wg.Done()
			defer func() { <-sem }()
			h, err := history(ctx, c, n)
			if err != nil {
				return
			}
			mu.Lock()
			hist[n] = h
			mu.Unlock()
		}(n)
	}
	wg.Wait()

	entries := make([]entry, 0, len(seen))
	for e := range seen {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].name != entries[j].name {
			return entries[i].name < entries[j].name
		}
		return entries[i].version < entries[j].version
	})
	var evaluated, silenceOnly, anyAge, fresh int
	var hits []string
	for _, e := range entries {
		h := hist[e.name]
		if h == nil {
			continue
		}
		if _, ok := h.PublishedAt[e.version]; !ok {
			continue
		}
		evaluated++
		if afterSilence(h, e.version) {
			silenceOnly++
		}
		b := domain.DetectDormantBurst(h, e.version, now, year, 0)
		if b == nil {
			continue
		}
		anyAge++
		mark := ""
		if domain.DetectDormantBurst(h, e.version, now, year, year) != nil {
			fresh++
			mark = " (fires)"
		}
		hits = append(hits, fmt.Sprintf("%s@%s published %s, silent %d days, burst %s%s",
			e.name, e.version, b.PublishedAt.Format("2006-01-02"), b.SilentDays, strings.Join(b.Versions, ", "), mark))
	}
	fmt.Printf("%s at %s: %d distinct name@version pairs with a publish time (of %d in the lockfile)\n"+
		"  published after >=365 days without any release (silence alone, any age): %d\n"+
		"  ... and in a burst across >=2 release lines (any age): %d\n"+
		"  ... and within the 365-day age limit (what uzomuzo reports): %d\n",
		path, now.Format("2006-01-02"), evaluated, len(entries), silenceOnly, anyAge, fresh)
	for _, h := range hits {
		fmt.Println("  " + h)
	}
}

// afterSilence reports whether version was published at least a year after
// the release before it, ignoring the line condition: the rule ADR-0026
// rejected as too broad.
func afterSilence(h *domain.ReleaseHistory, version string) bool {
	at := h.PublishedAt[version]
	var prev time.Time
	for _, t := range h.PublishedAt {
		if t.Before(at) && t.After(prev) {
			prev = t
		}
	}
	return !prev.IsZero() && at.Sub(prev) >= year
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
