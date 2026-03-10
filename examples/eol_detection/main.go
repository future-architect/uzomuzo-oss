// Example: EOL detection using the full evaluation pipeline.
//
// Demonstrates:
//   - Using NewEvaluator + EvaluatePURLs (registry heuristics + lifecycle assessment)
//   - Extracting final lifecycle label vs. raw primary-source EOL state
//   - Displaying EOL evidences, successor info, and scheduled dates
//
// NOTE: Results depend on registry / GitHub metadata freshness.
// Some sample PURLs may yield NotEOL or Unknown.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/future-architect/uzomuzo/pkg/uzomuzo"
)

func main() {
	ctx := context.Background()
	client := uzomuzo.NewEvaluator(os.Getenv("GITHUB_TOKEN"))

	purls := []string{
		"pkg:npm/express@4.18.2",
		"pkg:npm/request@2.88.2", // illustrative potentially legacy package
		"pkg:maven/org.springframework/spring-core@5.3.8",
		"pkg:pypi/requests@2.28.1",
		"pkg:maven/cglib/cglib",
	}

	fmt.Printf("EOL detection demo: %d packages\n\n", len(purls))

	analyses, err := client.EvaluatePURLs(ctx, purls)
	if err != nil {
		fmt.Printf("Analyze failed: %v\n", err)
		return
	}

	now := time.Now()

	for p, a := range analyses {
		fmt.Printf("=== %s ===\n", p)
		if a == nil {
			fmt.Println("  (nil analysis)")
			continue
		}
		if a.Error != nil {
			fmt.Printf("  Error: %v\n\n", a.Error)
			continue
		}

		summary := uzomuzo.BuildLifecycleSummary(a)
		fmt.Printf("  FinalLifecycleLabel: %s\n", summary.FinalLabel)
		fmt.Printf("  Raw EOLState: %s (human=%s)\n", summary.EOLState, summary.EOLHumanState)
		if summary.Successor != "" {
			fmt.Printf("  Successor: %s\n", summary.Successor)
		}
		if summary.ScheduledAt != nil && !summary.ScheduledAt.IsZero() {
			fmt.Printf("  ScheduledAt: %s (in %d days)\n",
				summary.ScheduledAt.Format("2006-01-02"),
				int(summary.ScheduledAt.Sub(now).Hours()/24))
		}
		// Print unified reason (EN/JA fallback logic inside domain)
		finalReason := a.EOL.FinalReason()
		finalReasonJa := a.EOL.FinalReasonJa()
		if finalReasonJa != "" {
			fmt.Printf("  FinalReason(JA): %s\n", finalReasonJa)
		}
		if finalReason != "" {
			fmt.Printf("  FinalReason(EN): %s\n", finalReason)
		} else {
			fmt.Println("  FinalReason: (none)")
		}
		if len(summary.EOLEvidences) > 0 {
			fmt.Println("  Evidences:")
			for _, ev := range summary.EOLEvidences {
				fmt.Printf("    - [%s] %s (ref=%s conf=%.2f)\n", ev.Source, ev.Summary, ev.Reference, ev.Confidence)
			}
		} else {
			fmt.Println("  Evidences: (none)")
		}

		switch summary.EOLState {
		case string(uzomuzo.EOLEndOfLife):
			fmt.Println("  → Confirmed EOL")
		case string(uzomuzo.EOLScheduled):
			fmt.Println("  → Scheduled EOL")
		case string(uzomuzo.EOLNotEOL):
			fmt.Println("  → Not EOL")
		case "", string(uzomuzo.EOLUnknown):
			fmt.Println("  → Unknown / no primary-source signals")
		}
		fmt.Println()
	}
}
