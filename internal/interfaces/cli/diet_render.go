package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	domaindiet "github.com/future-architect/uzomuzo-oss/internal/domain/diet"
)

// --- JSON output ---

type dietJSONOutput struct {
	Summary    dietJSONSummary `json:"summary"`
	Entries    []dietJSONEntry `json:"dependencies"`
	SBOMPath   string          `json:"sbom_path"`
	SourceRoot string          `json:"source_root,omitempty"`
	AnalyzedAt string          `json:"analyzed_at"`
}

type dietJSONSummary struct {
	TotalDirect              int `json:"total_direct"`
	TotalTransitive          int `json:"total_transitive"`
	TotalExclusiveTransitive int `json:"total_exclusive_transitive"`
	UnusedDirect             int `json:"unused_direct"`
	EasyWins                 int `json:"easy_wins"`
	EstimatedRemovable       int `json:"estimated_removable"`
}

type dietJSONEntry struct {
	Rank      int    `json:"rank"`
	PURL      string `json:"purl"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Ecosystem string `json:"ecosystem"`

	PriorityScore  float64 `json:"priority_score"`
	GraphImpact    float64 `json:"graph_impact"`
	CouplingEffort float64 `json:"coupling_effort"`
	HealthRisk     float64 `json:"health_risk"`
	Difficulty     string  `json:"difficulty"`

	ExclusiveTransitive int `json:"exclusive_transitive"`
	TotalTransitive     int `json:"total_transitive"`

	ImportFileCount int      `json:"import_file_count"`
	CallSiteCount   int      `json:"call_site_count"`
	APIBreadth      int      `json:"api_breadth"`
	IsUnused        bool     `json:"is_unused"`
	ImportFiles     []string `json:"import_files,omitempty"`

	Lifecycle          string  `json:"lifecycle"`
	HasVulnerabilities bool    `json:"has_vulnerabilities,omitempty"`
	VulnerabilityCount int     `json:"vulnerability_count,omitempty"`
	MaxCVSSScore       float64 `json:"max_cvss_score,omitempty"`
	OverallScore       float64 `json:"overall_score,omitempty"`
}

func renderDietOutput(w io.Writer, plan *domaindiet.DietPlan, format string) error {
	switch format {
	case "json":
		return renderDietJSON(w, plan)
	case "table":
		return renderDietTable(w, plan)
	case "detailed":
		return renderDietDetailed(w, plan)
	default:
		return fmt.Errorf("unknown format: %s", format)
	}
}

func renderDietJSON(w io.Writer, plan *domaindiet.DietPlan) error {
	output := dietJSONOutput{
		Summary: dietJSONSummary{
			TotalDirect:              plan.Summary.TotalDirect,
			TotalTransitive:          plan.Summary.TotalTransitive,
			TotalExclusiveTransitive: plan.Summary.TotalExclusiveTransitive,
			UnusedDirect:             plan.Summary.UnusedDirect,
			EasyWins:                 plan.Summary.EasyWins,
			EstimatedRemovable:       plan.Summary.EstimatedRemovable,
		},
		SBOMPath:   plan.SBOMPath,
		SourceRoot: plan.SourceRoot,
		AnalyzedAt: plan.AnalyzedAt.Format("2006-01-02T15:04:05Z07:00"),
	}

	output.Entries = make([]dietJSONEntry, len(plan.Entries))
	for i, e := range plan.Entries {
		output.Entries[i] = dietJSONEntry{
			Rank:                e.Scores.Rank,
			PURL:                e.PURL,
			Name:                e.Name,
			Version:             e.Version,
			Ecosystem:           e.Ecosystem,
			PriorityScore:       e.Scores.PriorityScore,
			GraphImpact:         e.Scores.GraphImpact,
			CouplingEffort:      e.Scores.CouplingEffort,
			HealthRisk:          e.Scores.HealthRisk,
			Difficulty:          e.Scores.Difficulty,
			ExclusiveTransitive: e.Graph.ExclusiveTransitiveCount,
			TotalTransitive:     e.Graph.TotalTransitiveCount,
			ImportFileCount:     e.Coupling.ImportFileCount,
			CallSiteCount:       e.Coupling.CallSiteCount,
			APIBreadth:          e.Coupling.APIBreadth,
			IsUnused:            e.Coupling.IsUnused,
			ImportFiles:         e.Coupling.ImportFiles,
			Lifecycle:           e.Health.MaintenanceStatus,
			HasVulnerabilities:  e.Health.HasVulnerabilities,
			VulnerabilityCount:  e.Health.VulnerabilityCount,
			MaxCVSSScore:        e.Health.MaxCVSSScore,
			OverallScore:        e.Health.OverallScore,
		}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

func renderDietTable(w io.Writer, plan *domaindiet.DietPlan) error {
	// Summary header
	fmt.Fprintf(w, "\n── Diet Plan (%d direct dependencies) ─────────────────────────\n\n", plan.Summary.TotalDirect)
	if plan.Summary.UnusedDirect > 0 {
		fmt.Fprintf(w, "  Unused direct deps:  %d\n", plan.Summary.UnusedDirect)
	}
	if plan.Summary.EasyWins > 0 {
		fmt.Fprintf(w, "  Easy wins:           %d\n", plan.Summary.EasyWins)
	}
	fmt.Fprintf(w, "  Estimated removable: %d\n\n", plan.Summary.EstimatedRemovable)

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "RANK\tPRIORITY\tDIFFICULTY\tPURL\tEXCLUSIVE\tFILES\tCALLS\tLIFECYCLE\n")
	fmt.Fprintf(tw, "────\t────────\t──────────\t────\t─────────\t─────\t─────\t─────────\n")
	for _, e := range plan.Entries {
		fmt.Fprintf(tw, "%d\t%.2f\t%s\t%s\t%d\t%d\t%d\t%s\n",
			e.Scores.Rank,
			e.Scores.PriorityScore,
			e.Scores.Difficulty,
			e.PURL,
			e.Graph.ExclusiveTransitiveCount,
			e.Coupling.ImportFileCount,
			e.Coupling.CallSiteCount,
			e.Health.MaintenanceStatus,
		)
	}
	tw.Flush()

	fmt.Fprintf(w, "\n── Expected Impact ─────────────────────────────────────────────\n")
	fmt.Fprintf(w, "  Direct deps:     %d\n", plan.Summary.TotalDirect)
	fmt.Fprintf(w, "  Transitive deps: %d (exclusive removable: %d)\n",
		plan.Summary.TotalTransitive, plan.Summary.TotalExclusiveTransitive)
	fmt.Fprintln(w)

	return nil
}

func renderDietDetailed(w io.Writer, plan *domaindiet.DietPlan) error {
	// Header
	fmt.Fprintf(w, "\n══════════════════════════════════════════════════════════════\n")
	fmt.Fprintf(w, "  Diet Plan — %d direct dependencies analyzed\n", plan.Summary.TotalDirect)
	fmt.Fprintf(w, "  SBOM: %s\n", plan.SBOMPath)
	if plan.SourceRoot != "" {
		fmt.Fprintf(w, "  Source: %s\n", plan.SourceRoot)
	}
	fmt.Fprintf(w, "══════════════════════════════════════════════════════════════\n\n")

	for _, e := range plan.Entries {
		fmt.Fprintf(w, "┌─ #%d %s (%s) ─────────────────────\n", e.Scores.Rank, e.Name, e.Version)
		fmt.Fprintf(w, "│  PURL:       %s\n", e.PURL)
		fmt.Fprintf(w, "│  Priority:   %.2f  Difficulty: %s\n", e.Scores.PriorityScore, e.Scores.Difficulty)
		fmt.Fprintf(w, "│\n")
		fmt.Fprintf(w, "│  Graph Impact\n")
		fmt.Fprintf(w, "│    Exclusive transitive: %d\n", e.Graph.ExclusiveTransitiveCount)
		fmt.Fprintf(w, "│    Shared transitive:    %d\n", e.Graph.SharedTransitiveCount)
		fmt.Fprintf(w, "│    Total transitive:     %d\n", e.Graph.TotalTransitiveCount)
		fmt.Fprintf(w, "│\n")
		fmt.Fprintf(w, "│  Coupling\n")
		if e.Coupling.IsUnused {
			fmt.Fprintf(w, "│    Status: UNUSED (0 imports found)\n")
		} else {
			fmt.Fprintf(w, "│    Files:      %d\n", e.Coupling.ImportFileCount)
			fmt.Fprintf(w, "│    Call sites: %d\n", e.Coupling.CallSiteCount)
			fmt.Fprintf(w, "│    API breadth: %d distinct symbols\n", e.Coupling.APIBreadth)
		}
		fmt.Fprintf(w, "│\n")
		fmt.Fprintf(w, "│  Health\n")
		fmt.Fprintf(w, "│    Lifecycle: %s\n", e.Health.MaintenanceStatus)
		if e.Health.HasVulnerabilities {
			fmt.Fprintf(w, "│    Vulnerabilities: %d (max CVSS: %.1f)\n", e.Health.VulnerabilityCount, e.Health.MaxCVSSScore)
		}
		if e.Health.OverallScore > 0 {
			fmt.Fprintf(w, "│    Scorecard: %.1f/10\n", e.Health.OverallScore)
		}
		fmt.Fprintf(w, "│\n")
		fmt.Fprintf(w, "│  Scores\n")
		fmt.Fprintf(w, "│    Graph impact:    %.2f\n", e.Scores.GraphImpact)
		fmt.Fprintf(w, "│    Coupling effort: %.2f\n", e.Scores.CouplingEffort)
		fmt.Fprintf(w, "│    Health risk:     %.2f\n", e.Scores.HealthRisk)
		fmt.Fprintf(w, "└────────────────────────────────────────────────\n\n")
	}

	// Summary
	fmt.Fprintf(w, "── Summary ─────────────────────────────────────────────────\n")
	fmt.Fprintf(w, "  Direct:     %d\n", plan.Summary.TotalDirect)
	fmt.Fprintf(w, "  Transitive: %d (exclusive: %d)\n", plan.Summary.TotalTransitive, plan.Summary.TotalExclusiveTransitive)
	fmt.Fprintf(w, "  Unused:     %d\n", plan.Summary.UnusedDirect)
	fmt.Fprintf(w, "  Easy wins:  %d\n", plan.Summary.EasyWins)
	fmt.Fprintln(w)

	return nil
}
