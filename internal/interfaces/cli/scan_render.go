package cli

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	domainaudit "github.com/future-architect/uzomuzo-oss/internal/domain/audit"
	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
)

// jsonSummary holds verdict counts for JSON output.
type jsonSummary struct {
	Total   int `json:"total"`
	OK      int `json:"ok"`
	Caution int `json:"caution"`
	Replace int `json:"replace"`
	Review  int `json:"review"`
}

// entryMaintenanceEOL extracts maintenance status and EOL state from an audit entry.
// Returns placeholder strings when Analysis is nil.
func entryMaintenanceEOL(e *domainaudit.AuditEntry, placeholder string) (maintenance, eol string) {
	if e.Analysis != nil {
		return e.Analysis.FinalMaintenanceStatus(), e.Analysis.EOL.HumanState()
	}
	return placeholder, placeholder
}

// computeSummary counts verdict occurrences across audit entries.
func computeSummary(entries []domainaudit.AuditEntry) jsonSummary {
	var s jsonSummary
	s.Total = len(entries)
	for _, e := range entries {
		switch e.Verdict {
		case domainaudit.VerdictOK:
			s.OK++
		case domainaudit.VerdictCaution:
			s.Caution++
		case domainaudit.VerdictReplace:
			s.Replace++
		case domainaudit.VerdictReview:
			s.Review++
		}
	}
	return s
}

// Scan output format constants.
const (
	FormatDetailed = "detailed"
	FormatTable    = "table"
	FormatJSON     = "json"
	FormatCSV      = "csv"
)

// smartDefaultThreshold is the input count threshold for auto-selecting detailed vs table.
const smartDefaultThreshold = 3

// ResolveFormat determines the effective output format.
// If explicit is non-empty, it is validated and returned.
// Otherwise, the smart default is applied: detailed for ≤3 inputs, table for more.
func ResolveFormat(explicit string, inputCount int) (string, error) {
	if explicit != "" {
		normalized := strings.TrimSpace(strings.ToLower(explicit))
		switch normalized {
		case FormatDetailed, FormatTable, FormatJSON, FormatCSV:
			return normalized, nil
		default:
			return "", fmt.Errorf("unsupported format %q (use detailed, table, json, or csv)", explicit)
		}
	}
	if inputCount <= smartDefaultThreshold {
		return FormatDetailed, nil
	}
	return FormatTable, nil
}

// renderScanOutput dispatches to the correct renderer by format.
func renderScanOutput(w io.Writer, entries []domainaudit.AuditEntry, format string) error {
	switch format {
	case FormatDetailed:
		return renderScanDetailed(w, entries)
	case FormatTable:
		return renderScanTable(w, entries)
	case FormatJSON:
		return renderScanJSON(w, entries)
	case FormatCSV:
		return renderScanCSV(w, entries)
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
}

// Section markers for machine-parseable output.
const (
	// MarkerSummaryTableBegin marks the start of the summary table section.
	MarkerSummaryTableBegin = "--- Summary Table ---"
	// MarkerDetailedReportBegin marks the start of the detailed report section.
	MarkerDetailedReportBegin = "--- Detailed Report ---"
)

// sourceDisplayName returns the human-readable display name for an EntrySource.
func sourceDisplayName(s domainaudit.EntrySource) string {
	switch s {
	case domainaudit.SourceDirect:
		return "direct"
	case domainaudit.SourceActions:
		return "action"
	case domainaudit.SourceActionsTransitive:
		return "action-transitive"
	case domainaudit.SourceActionsLocal:
		return "action-local"
	default:
		return string(s)
	}
}

// hasMultipleSources reports whether entries contain more than one distinct Source value.
func hasMultipleSources(entries []domainaudit.AuditEntry) bool {
	if len(entries) == 0 {
		return false
	}
	first := entries[0].Source
	for _, e := range entries[1:] {
		if e.Source != first {
			return true
		}
	}
	return false
}

// hasRelationInfo returns true if any entry has a known dependency relation.
func hasRelationInfo(entries []domainaudit.AuditEntry) bool {
	for i := range entries {
		if entries[i].Relation != depparser.RelationUnknown {
			return true
		}
	}
	return false
}

// formatRelation returns the display string for a dependency's relation.
// For transitive deps with known parents: "transitive (express, lodash)"
// For direct deps: "direct"
// For unknown: "—"
func formatRelation(e *domainaudit.AuditEntry) string {
	relation := e.Relation.String()
	if relation == "" {
		return "—"
	}
	if e.Relation == depparser.RelationTransitive && len(e.ViaParents) > 0 {
		return fmt.Sprintf("transitive (%s)", strings.Join(e.ViaParents, ", "))
	}
	return relation
}

// detailedEntryHeader returns the header for a detailed report entry.
// When showSource is true, the source is embedded: "--- PURL 1 (action) ---".
func detailedEntryHeader(counter int, source domainaudit.EntrySource, showSource bool) string {
	if showSource {
		return fmt.Sprintf("--- PURL %d (%s) ---", counter, sourceDisplayName(source))
	}
	return fmt.Sprintf("--- PURL %d ---", counter)
}

// renderScanDetailed prints a summary table followed by rich per-package output.
// The two sections are separated by markers for machine extraction.
func renderScanDetailed(w io.Writer, entries []domainaudit.AuditEntry) error {
	// Summary table section
	if _, err := fmt.Fprintln(w, MarkerSummaryTableBegin); err != nil {
		return fmt.Errorf("failed to write marker: %w", err)
	}
	if err := renderScanTable(w, entries); err != nil {
		return fmt.Errorf("failed to write summary table: %w", err)
	}

	// Detailed report section
	if _, err := fmt.Fprintf(w, "\n%s\n", MarkerDetailedReportBegin); err != nil {
		return fmt.Errorf("failed to write marker: %w", err)
	}

	showSource := hasMultipleSources(entries)
	counter := 0
	for i := range entries {
		e := &entries[i]
		if e.Analysis == nil || e.Analysis.Error != nil {
			counter++
			if _, err := fmt.Fprintf(w, "\n%s\n", detailedEntryHeader(counter, e.Source, showSource)); err != nil {
				return fmt.Errorf("failed to write entry: %w", err)
			}
			if _, err := fmt.Fprintf(w, "Package: %s\n", e.PURL); err != nil {
				return fmt.Errorf("failed to write entry: %w", err)
			}
			if e.Via != "" {
				if _, err := fmt.Fprintf(w, "🔗 Via: %s\n", e.Via); err != nil {
					return fmt.Errorf("failed to write entry: %w", err)
				}
			}
			verdict := string(e.Verdict)
			if e.ErrorMsg != "" {
				if _, err := fmt.Fprintf(w, "Verdict: %s (error: %s)\n", verdict, e.ErrorMsg); err != nil {
					return fmt.Errorf("failed to write entry: %w", err)
				}
			} else {
				if _, err := fmt.Fprintf(w, "Verdict: %s\n", verdict); err != nil {
					return fmt.Errorf("failed to write entry: %w", err)
				}
			}
			continue
		}
		counter++
		// Print source-annotated header, then delegate body to printAnalysisBody (stdout).
		fmt.Printf("\n%s\n", detailedEntryHeader(counter, e.Source, showSource))
		if e.Relation != depparser.RelationUnknown {
			if _, err := fmt.Fprintf(w, "🔗 Relation: %s\n", formatRelation(e)); err != nil {
				return fmt.Errorf("failed to write relation: %w", err)
			}
		}
		printAnalysisBody(e.PURL, e.Analysis, e.Via)
	}
	if counter == 0 {
		if _, err := fmt.Fprintln(w, "No results to display"); err != nil {
			return fmt.Errorf("failed to write: %w", err)
		}
	}
	return nil
}

// renderScanTable renders the VERDICT table format.
// Conditional columns: SOURCE (when multiple sources), RELATION (when relation info present).
func renderScanTable(w io.Writer, entries []domainaudit.AuditEntry) error {
	showSource := hasMultipleSources(entries)
	showRelation := hasRelationInfo(entries)

	writeHeader := func(tw *tabwriter.Writer) error {
		var cols []string
		cols = append(cols, "VERDICT")
		if showSource {
			cols = append(cols, "SOURCE")
		}
		cols = append(cols, "PURL")
		if showRelation {
			cols = append(cols, "RELATION")
		}
		cols = append(cols, "LIFECYCLE", "EOL")
		_, err := fmt.Fprintln(tw, strings.Join(cols, "\t"))
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if err := writeHeader(tw); err != nil {
		return fmt.Errorf("failed to write table header: %w", err)
	}

	for i := range entries {
		maintenance, eol := entryMaintenanceEOL(&entries[i], "—")
		var cols []string
		cols = append(cols, string(entries[i].Verdict))
		if showSource {
			cols = append(cols, sourceDisplayName(entries[i].Source))
		}
		cols = append(cols, entries[i].PURL)
		if showRelation {
			cols = append(cols, formatRelation(&entries[i]))
		}
		cols = append(cols, maintenance, eol)
		if _, err := fmt.Fprintln(tw, strings.Join(cols, "\t")); err != nil {
			return fmt.Errorf("failed to write table row: %w", err)
		}
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("failed to flush table output: %w", err)
	}
	s := computeSummary(entries)
	if _, err := fmt.Fprintf(w, "\nSummary: %d dependencies | %d ok | %d caution | %d replace | %d review\n",
		s.Total, s.OK, s.Caution, s.Replace, s.Review); err != nil {
		return fmt.Errorf("failed to write summary: %w", err)
	}
	return nil
}

// enrichedJSONEntry is the DTO for --format json with full analysis data.
type enrichedJSONEntry struct {
	PURL      string `json:"purl"`
	Verdict   string `json:"verdict"`
	Lifecycle string `json:"lifecycle"`
	EOL       string `json:"eol"`
	EOLReason string `json:"eol_reason,omitempty"`
	Successor string `json:"successor,omitempty"`

	RepoURL         string   `json:"repo_url,omitempty"`
	Archived        bool     `json:"archived,omitempty"`
	OverallScore    float64  `json:"overall_score,omitempty"`
	DependentCount  int      `json:"dependent_count,omitempty"`
	StableVersion   string   `json:"stable_version,omitempty"`
	ProjectLicense  string   `json:"project_license,omitempty"`
	VersionLicenses []string `json:"version_licenses,omitempty"`

	AdvisoryCount       int     `json:"advisory_count,omitempty"`
	MaxAdvisorySeverity string  `json:"max_advisory_severity,omitempty"`
	MaxCVSS3Score       float64 `json:"max_cvss3_score,omitempty"`

	Reason      string   `json:"reason,omitempty"`
	Error       string   `json:"error,omitempty"`
	Source      string   `json:"source,omitempty"`
	Via         string   `json:"via,omitempty"`
	Relation    string   `json:"relation,omitempty"`
	RelationVia []string `json:"relation_via,omitempty"`
}

type enrichedJSONOutput struct {
	Summary jsonSummary         `json:"summary"`
	Entries []enrichedJSONEntry `json:"packages"`
}

func renderScanJSON(w io.Writer, entries []domainaudit.AuditEntry) error {
	out := enrichedJSONOutput{
		Summary: computeSummary(entries),
		Entries: make([]enrichedJSONEntry, 0, len(entries)),
	}
	for i := range entries {
		out.Entries = append(out.Entries, newEnrichedJSONEntry(&entries[i]))
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("failed to encode JSON output: %w", err)
	}
	return nil
}

// newEnrichedJSONEntry converts a single AuditEntry into the enriched JSON DTO.
func newEnrichedJSONEntry(e *domainaudit.AuditEntry) enrichedJSONEntry {
	maintenance, eol := entryMaintenanceEOL(e, "—")

	je := enrichedJSONEntry{
		PURL:        e.PURL,
		Verdict:     string(e.Verdict),
		Lifecycle:   maintenance,
		EOL:         eol,
		Error:       e.ErrorMsg,
		Source:      string(e.Source),
		Via:         e.Via,
		Relation:    e.Relation.String(),
		RelationVia: e.ViaParents,
	}

	a := e.Analysis
	if a == nil {
		return je
	}

	je.RepoURL = a.RepoURL
	je.OverallScore = a.OverallScore
	je.DependentCount = a.DependentCount
	je.EOLReason = a.EOL.FinalReason()
	je.Successor = a.EOL.Successor
	je.Archived = a.IsArchived()

	if a.ReleaseInfo != nil && a.ReleaseInfo.StableVersion != nil {
		je.StableVersion = a.ReleaseInfo.StableVersion.Version
		je.AdvisoryCount = len(a.ReleaseInfo.StableVersion.Advisories)
		if maxScore := a.ReleaseInfo.StableVersion.MaxCVSS3(); maxScore > 0 {
			je.MaxCVSS3Score = maxScore
			je.MaxAdvisorySeverity = domain.SeverityFromCVSS3(maxScore)
		}
	}
	if a.ProjectLicense.Identifier != "" {
		je.ProjectLicense = a.ProjectLicense.Identifier
	}
	if len(a.RequestedVersionLicenses) > 0 {
		ids := make([]string, 0, len(a.RequestedVersionLicenses))
		for _, lic := range a.RequestedVersionLicenses {
			ids = append(ids, lic.Identifier)
		}
		je.VersionLicenses = ids
	}
	if lr := a.GetLifecycleResult(); lr != nil {
		je.Reason = lr.Reason
	}

	return je
}

func renderScanCSV(w io.Writer, entries []domainaudit.AuditEntry) error {
	cw := csv.NewWriter(w)
	showRelation := hasRelationInfo(entries)

	header := []string{"verdict", "purl"}
	if showRelation {
		header = append(header, "relation", "relation_via")
	}
	header = append(header, "lifecycle", "eol", "eol_reason", "successor", "advisory_count", "max_advisory_severity", "repo_url", "source", "via")
	if err := cw.Write(header); err != nil {
		return fmt.Errorf("failed to write CSV header: %w", err)
	}
	for i := range entries {
		e := &entries[i]
		maintenance, eol := entryMaintenanceEOL(e, "")

		eolReason := ""
		successor := ""
		repoURL := ""
		advisoryCount := ""
		maxSeverity := ""
		if a := e.Analysis; a != nil {
			eolReason = a.EOL.FinalReason()
			successor = a.EOL.Successor
			repoURL = a.RepoURL
			if a.ReleaseInfo != nil && a.ReleaseInfo.StableVersion != nil {
				advisoryCount = fmt.Sprintf("%d", len(a.ReleaseInfo.StableVersion.Advisories))
				if maxScore := a.ReleaseInfo.StableVersion.MaxCVSS3(); maxScore > 0 {
					maxSeverity = fmt.Sprintf("%s (%.1f)", domain.SeverityFromCVSS3(maxScore), maxScore)
				}
			}
		}

		row := []string{string(e.Verdict), e.PURL}
		if showRelation {
			row = append(row, e.Relation.String(), strings.Join(e.ViaParents, ";"))
		}
		row = append(row, maintenance, eol, eolReason, successor, advisoryCount, maxSeverity, repoURL, string(e.Source), e.Via)
		if err := cw.Write(row); err != nil {
			return fmt.Errorf("failed to write CSV row for %s: %w", e.PURL, err)
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("failed to flush CSV output: %w", err)
	}
	return nil
}
