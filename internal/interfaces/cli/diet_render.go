package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
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
	TotalDirect         int `json:"total_direct"`
	TotalTransitive     int `json:"total_transitive"`
	TransitiveOnlyByOne int `json:"transitive_only_by_one"`
	UnusedDirect        int `json:"unused_direct"`
	EasyWins            int `json:"easy_wins"`
	ActionableDirect    int `json:"actionable_direct"`
	StaysAsIndirect     int `json:"stays_as_indirect"`
}

type dietJSONEntry struct {
	Rank      int    `json:"rank"`
	PURL      string `json:"purl"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Ecosystem string `json:"ecosystem"`
	Scope     string `json:"scope,omitempty"`

	PriorityScore  float64 `json:"priority_score"`
	GraphImpact    float64 `json:"graph_impact"`
	CouplingEffort float64 `json:"coupling_effort"`
	HealthRisk     float64 `json:"health_risk"`
	Difficulty     string  `json:"difficulty"`

	ExclusiveTransitive int      `json:"exclusive_transitive"`
	TotalTransitive     int      `json:"total_transitive"`
	StaysAsIndirect     bool     `json:"stays_as_indirect"`
	IndirectVia         []string `json:"indirect_via,omitempty"`

	ImportFileCount   int      `json:"import_file_count"`
	CallSiteCount     int      `json:"call_site_count"`
	APIBreadth        int      `json:"api_breadth"`
	Symbols           []string `json:"symbols"`
	IsUnused          bool     `json:"is_unused"`
	ImportFiles       []string `json:"import_files,omitempty"`
	HasBlankImport    bool     `json:"has_blank_import"`
	HasDotImport      bool     `json:"has_dot_import"`
	HasWildcardImport bool     `json:"has_wildcard_import"`

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
			TotalDirect:         plan.Summary.TotalDirect,
			TotalTransitive:     plan.Summary.TotalTransitive,
			TransitiveOnlyByOne: plan.Summary.TotalExclusiveTransitive,
			UnusedDirect:        plan.Summary.UnusedDirect,
			EasyWins:            plan.Summary.EasyWins,
			ActionableDirect:    plan.Summary.EstimatedRemovable,
			StaysAsIndirect:     plan.Summary.StaysAsIndirectCount,
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
			Scope:               e.Scope,
			PriorityScore:       e.Scores.PriorityScore,
			GraphImpact:         e.Scores.GraphImpact,
			CouplingEffort:      e.Scores.CouplingEffort,
			HealthRisk:          e.Scores.HealthRisk,
			Difficulty:          e.Scores.Difficulty,
			ExclusiveTransitive: e.Graph.ExclusiveTransitiveCount,
			TotalTransitive:     e.Graph.TotalTransitiveCount,
			StaysAsIndirect:     e.Graph.StaysAsIndirect(),
			IndirectVia:         e.Graph.IndirectVia,
			ImportFileCount:     e.Coupling.ImportFileCount,
			CallSiteCount:       e.Coupling.CallSiteCount,
			APIBreadth:          e.Coupling.APIBreadth,
			Symbols:             normalizeSymbols(e.Coupling.Symbols),
			IsUnused:            e.Coupling.IsUnused,
			ImportFiles:         e.Coupling.ImportFiles,
			HasBlankImport:      e.Coupling.HasBlankImport,
			HasDotImport:        e.Coupling.HasDotImport,
			HasWildcardImport:   e.Coupling.HasWildcardImport,
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
	p := &errWriter{w: w}

	// Summary header
	p.printf("\n── Diet Plan (%d direct dependencies) ─────────────────────────\n\n", plan.Summary.TotalDirect)
	if plan.Summary.UnusedDirect > 0 {
		p.printf("  Unused (0 imports):  %d\n", plan.Summary.UnusedDirect)
	}
	p.printf("  Quick wins:          %d  (trivial/easy + high impact)\n", plan.Summary.EasyWins)

	if p.err != nil {
		return p.err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	tp := &errWriter{w: tw}
	tp.printf("RANK\tSCORE\tEFFORT\tPURL\tREMOVES\tREMAINS\tIMPORTS\tCALLS\tSTATUS\n")
	tp.printf("────\t─────\t──────\t────\t───────\t───────\t───────\t─────\t──────\n")
	for _, e := range plan.Entries {
		stays := "-"
		if e.Graph.StaysAsIndirect() {
			stays = "yes"
		}
		tp.printf("%d\t%.2f\t%s\t%s\t%d\t%s\t%d\t%d\t%s\n",
			e.Scores.Rank,
			e.Scores.PriorityScore,
			e.Scores.Difficulty,
			e.PURL,
			e.Graph.ExclusiveTransitiveCount,
			stays,
			e.Coupling.ImportFileCount,
			e.Coupling.CallSiteCount,
			e.Health.MaintenanceStatus,
		)
	}
	if tp.err != nil {
		return tp.err
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	p.printf("\n── Dependency Tree ─────────────────────────────────────────────\n")
	p.printf("  Direct deps:          %d\n", plan.Summary.TotalDirect)
	p.printf("  Transitive deps:      %d\n", plan.Summary.TotalTransitive)
	p.printf("  └ removes-with-dep:   %d  (removable if that direct dep is removed)\n",
		plan.Summary.TotalExclusiveTransitive)
	if plan.Summary.StaysAsIndirectCount > 0 {
		p.printf("  ⚠ remains-indirect:   %d  (remain in tree via another direct dep)\n",
			plan.Summary.StaysAsIndirectCount)
	}
	p.printf("\n")

	return p.err
}

func renderDietDetailed(w io.Writer, plan *domaindiet.DietPlan) error {
	p := &errWriter{w: w}

	// Header
	p.printf("\n══════════════════════════════════════════════════════════════\n")
	p.printf("  Diet Plan — %d direct dependencies analyzed\n", plan.Summary.TotalDirect)
	p.printf("  SBOM: %s\n", plan.SBOMPath)
	if plan.SourceRoot != "" {
		p.printf("  Source: %s\n", plan.SourceRoot)
	}
	p.printf("══════════════════════════════════════════════════════════════\n\n")

	for _, e := range plan.Entries {
		p.printf("┌─ #%d %s (%s) ─────────────────────\n", e.Scores.Rank, e.Name, e.Version)
		p.printf("│  PURL:       %s\n", e.PURL)
		if e.Scope != "" {
			p.printf("│  Scope:      %s\n", e.Scope)
		}
		p.printf("│  Score:       %.2f  Effort: %s\n", e.Scores.PriorityScore, e.Scores.Difficulty)
		p.printf("│\n")
		p.printf("│  Graph Impact\n")
		p.printf("│    Removes:             %d  (removed together)\n", e.Graph.ExclusiveTransitiveCount)
		p.printf("│    Shared with others:  %d\n", e.Graph.SharedTransitiveCount)
		p.printf("│    Total transitive:    %d\n", e.Graph.TotalTransitiveCount)
		if e.Graph.StaysAsIndirect() {
			p.printf("│    ⚠ Remains indirect:  yes  (via: %s)\n", formatViaList(e.Graph.IndirectVia))
		} else {
			p.printf("│    Remains indirect:    no   (fully removed from tree)\n")
		}
		p.printf("│\n")
		p.printf("│  Coupling\n")
		switch e.Scope {
		case domaindiet.ScopeTool:
			p.printf("│    Status: TOOL (go.mod tool directive — not imported in source)\n")
		case domaindiet.ScopeRuntime:
			p.printf("│    Status: RUNTIME (reflection/ServiceLoader/classpath resources — not statically imported)\n")
		default:
			if e.Scope == domaindiet.ScopeOptional {
				p.printf("│    Scope:  OPTIONAL (provided by runtime environment — not bundled)\n")
			}
			if e.Coupling.IsUnused {
				p.printf("│    Status: UNUSED (0 imports found)\n")
			} else {
				p.printf("│    Imports:    %d\n", e.Coupling.ImportFileCount)
				p.printf("│    Call sites: %d\n", e.Coupling.CallSiteCount)
				p.printf("│    API breadth: %d distinct symbols\n", e.Coupling.APIBreadth)
				if len(e.Coupling.Symbols) > 0 {
					p.printf("│    Symbols: %s\n", formatSymbolList(e.Coupling.Symbols))
				}
				if e.Coupling.HasBlankImport || e.Coupling.HasDotImport || e.Coupling.HasWildcardImport {
					var flags []string
					if e.Coupling.HasBlankImport {
						flags = append(flags, "blank-import")
					}
					if e.Coupling.HasDotImport {
						flags = append(flags, "dot-import")
					}
					if e.Coupling.HasWildcardImport {
						flags = append(flags, "wildcard-import")
					}
					p.printf("│    ⚠ Import flags: %s\n", strings.Join(flags, ", "))
				}
			}
		}
		p.printf("│\n")
		p.printf("│  Health\n")
		p.printf("│    Status: %s\n", e.Health.MaintenanceStatus)
		if e.Health.HasVulnerabilities {
			p.printf("│    Vulnerabilities: %d (max CVSS: %.1f)\n", e.Health.VulnerabilityCount, e.Health.MaxCVSSScore)
		}
		if e.Health.OverallScore > 0 {
			p.printf("│    Scorecard: %.1f/10\n", e.Health.OverallScore)
		}
		p.printf("│\n")
		p.printf("│  Scores\n")
		p.printf("│    Graph impact:    %.2f\n", e.Scores.GraphImpact)
		p.printf("│    Coupling effort: %.2f\n", e.Scores.CouplingEffort)
		p.printf("│    Health risk:     %.2f\n", e.Scores.HealthRisk)
		p.printf("└────────────────────────────────────────────────\n\n")
	}

	// Summary
	p.printf("── Summary ─────────────────────────────────────────────────\n")
	p.printf("  Direct deps:          %d\n", plan.Summary.TotalDirect)
	p.printf("  Transitive deps:      %d\n", plan.Summary.TotalTransitive)
	p.printf("  └ removes-with-dep:   %d  (removable if that direct dep is removed)\n", plan.Summary.TotalExclusiveTransitive)
	if plan.Summary.StaysAsIndirectCount > 0 {
		p.printf("  ⚠ remains-indirect:   %d  (remain in tree via another direct dep)\n",
			plan.Summary.StaysAsIndirectCount)
	}
	p.printf("  Unused (0 imports):   %d\n", plan.Summary.UnusedDirect)
	p.printf("  Quick wins:           %d\n", plan.Summary.EasyWins)
	p.printf("\n")

	return p.err
}

// normalizeSymbols returns an empty slice when symbols is nil, ensuring JSON
// output emits [] instead of null for consistent schema.
func normalizeSymbols(symbols []string) []string {
	if symbols == nil {
		return []string{}
	}
	return symbols
}

// formatSymbolList formats a list of symbol names for display, truncating if too many.
func formatSymbolList(symbols []string) string {
	const maxDisplay = 10
	if len(symbols) <= maxDisplay {
		return strings.Join(symbols, ", ")
	}
	return strings.Join(symbols[:maxDisplay], ", ") + fmt.Sprintf(" +%d more", len(symbols)-maxDisplay)
}

// formatViaList formats a list of PURLs for display, truncating if too many.
func formatViaList(via []string) string {
	if len(via) == 0 {
		return ""
	}
	if len(via) <= 3 {
		return strings.Join(via, ", ")
	}
	return strings.Join(via[:3], ", ") + fmt.Sprintf(" +%d more", len(via)-3)
}

// errWriter wraps an io.Writer and captures the first error, allowing
// sequential writes without checking each one individually.
type errWriter struct {
	w   io.Writer
	err error
}

func (ew *errWriter) printf(format string, args ...interface{}) {
	if ew.err != nil {
		return
	}
	_, ew.err = fmt.Fprintf(ew.w, format, args...)
}
