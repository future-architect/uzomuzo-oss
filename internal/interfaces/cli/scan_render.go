package cli

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	domain "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	domainaudit "github.com/future-architect/uzomuzo-oss/internal/domain/audit"
	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
)

// ParseShowOnly parses a comma-separated --show-only string into a set of Verdict values.
// Returns nil (show all) when raw is empty.
func ParseShowOnly(raw string) (map[domainaudit.Verdict]struct{}, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	valid := map[string]domainaudit.Verdict{
		"ok":      domainaudit.VerdictOK,
		"caution": domainaudit.VerdictCaution,
		"replace": domainaudit.VerdictReplace,
		"review":  domainaudit.VerdictReview,
	}
	parts := strings.Split(raw, ",")
	result := make(map[domainaudit.Verdict]struct{}, len(parts))
	for _, part := range parts {
		v := strings.TrimSpace(strings.ToLower(part))
		if v == "" {
			continue
		}
		verdict, ok := valid[v]
		if !ok {
			return nil, fmt.Errorf("invalid --show-only verdict %q; valid values: ok, caution, replace, review", v)
		}
		result[verdict] = struct{}{}
	}
	return result, nil
}

// filterEntriesByVerdict returns only entries whose Verdict is in the allowed set.
// If allowed is nil, all entries are returned.
func filterEntriesByVerdict(entries []domainaudit.AuditEntry, allowed map[domainaudit.Verdict]struct{}) []domainaudit.AuditEntry {
	if allowed == nil {
		return entries
	}
	filtered := make([]domainaudit.AuditEntry, 0, len(entries))
	for i := range entries {
		if _, ok := allowed[entries[i].Verdict]; ok {
			filtered = append(filtered, entries[i])
		}
	}
	return filtered
}

// buildIntegritySummary holds build integrity label counts for JSON output.
type buildIntegritySummary struct {
	Hardened int `json:"hardened"`
	Moderate int `json:"moderate"`
	Weak     int `json:"weak"`
	Ungraded int `json:"ungraded"`
}

// jsonSummary holds verdict counts for JSON output.
type jsonSummary struct {
	Total          int                   `json:"total"`
	OK             int                   `json:"ok"`
	Caution        int                   `json:"caution"`
	Replace        int                   `json:"replace"`
	Review         int                   `json:"review"`
	BuildIntegrity buildIntegritySummary `json:"build_integrity"`
}

// entryMaintenanceEOL extracts maintenance status and EOL state from an audit entry.
// Returns placeholder strings when Analysis is nil.
func entryMaintenanceEOL(e *domainaudit.AuditEntry, placeholder string) (maintenance, eol string) {
	if e.Analysis != nil {
		return e.Analysis.FinalMaintenanceStatus().String(), e.Analysis.EOL.HumanState()
	}
	return placeholder, placeholder
}

// computeSummary counts verdict and build integrity occurrences across audit entries.
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
		if e.Analysis != nil {
			if br := e.Analysis.GetBuildHealthResult(); br != nil {
				switch domain.BuildIntegrityLabel(br.Label) {
				case domain.BuildLabelHardened:
					s.BuildIntegrity.Hardened++
				case domain.BuildLabelModerate:
					s.BuildIntegrity.Moderate++
				case domain.BuildLabelWeak:
					s.BuildIntegrity.Weak++
				default:
					s.BuildIntegrity.Ungraded++
				}
			} else {
				s.BuildIntegrity.Ungraded++
			}
		} else {
			s.BuildIntegrity.Ungraded++
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
// allEntries is used for summary counts; displayEntries is the (possibly filtered) set to render.
// filterActive indicates whether --show-only was explicitly provided (even if all entries match).
func renderScanOutput(w io.Writer, allEntries, displayEntries []domainaudit.AuditEntry, format string, filterActive bool) error {
	switch format {
	case FormatDetailed:
		return renderScanDetailed(w, allEntries, displayEntries, filterActive)
	case FormatTable:
		return renderScanTable(w, allEntries, displayEntries, filterActive)
	case FormatJSON:
		return renderScanJSON(w, allEntries, displayEntries, filterActive)
	case FormatCSV:
		return renderScanCSV(w, displayEntries)
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

// renderScanDetailed prints a summary table followed by rich per-package box output.
// The two sections are separated by markers for machine extraction.
// allEntries is used for summary counts; displayEntries for the table rows and detailed boxes.
// filterActive indicates whether --show-only was explicitly provided.
func renderScanDetailed(w io.Writer, allEntries, displayEntries []domainaudit.AuditEntry, filterActive bool) error {
	// Summary table section
	if _, err := fmt.Fprintln(w, MarkerSummaryTableBegin); err != nil {
		return fmt.Errorf("failed to write marker: %w", err)
	}
	if err := renderScanTable(w, allEntries, displayEntries, filterActive); err != nil {
		return fmt.Errorf("failed to write summary table: %w", err)
	}

	// Detailed report section
	if _, err := fmt.Fprintf(w, "\n%s\n", MarkerDetailedReportBegin); err != nil {
		return fmt.Errorf("failed to write marker: %w", err)
	}

	counter := 0
	for i := range displayEntries {
		counter++
		// Preserve machine-parseable marker outside the box
		if _, err := fmt.Fprintf(w, "\n--- PURL %d ---\n", counter); err != nil {
			return fmt.Errorf("failed to write entry marker: %w", err)
		}
		if err := renderBoxEntry(w, &displayEntries[i]); err != nil {
			return fmt.Errorf("failed to write box entry: %w", err)
		}
	}
	if counter == 0 {
		if _, err := fmt.Fprintln(w, "No results to display"); err != nil {
			return fmt.Errorf("failed to write: %w", err)
		}
	}
	return nil
}

// tableVerdictDisplay returns the verdict string with emoji prefix for table output.
// The result is fixed-width padded so tabwriter aligns subsequent columns correctly
// despite emoji taking variable display width.
func tableVerdictDisplay(v domainaudit.Verdict) string {
	icon := verdictIcon(v)
	// Pad verdict text to 7 chars (length of "replace") so columns after it align.
	return fmt.Sprintf("%s %-7s", icon, string(v))
}

// renderScanTable renders the STATUS table format.
// allEntries is used for summary counts; displayEntries for the table rows.
// filterActive indicates whether --show-only was explicitly provided.
// Conditional columns: SOURCE (when multiple sources), RELATION (when relation info present).
func renderScanTable(w io.Writer, allEntries, displayEntries []domainaudit.AuditEntry, filterActive bool) error {
	showSource := hasMultipleSources(displayEntries)
	showRelation := hasRelationInfo(displayEntries)

	writeHeader := func(tw *tabwriter.Writer) error {
		var cols []string
		cols = append(cols, "STATUS")
		if showSource {
			cols = append(cols, "SOURCE")
		}
		cols = append(cols, "PURL")
		if showRelation {
			cols = append(cols, "RELATION")
		}
		cols = append(cols, "LIFECYCLE")
		cols = append(cols, "BUILD")
		if _, err := fmt.Fprintln(tw, strings.Join(cols, "\t")); err != nil {
			return fmt.Errorf("failed to write table header: %w", err)
		}
		return nil
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if err := writeHeader(tw); err != nil {
		return err
	}

	for i := range displayEntries {
		maintenance, _ := entryMaintenanceEOL(&displayEntries[i], "—")
		var cols []string
		cols = append(cols, tableVerdictDisplay(displayEntries[i].Verdict))
		if showSource {
			cols = append(cols, sourceDisplayName(displayEntries[i].Source))
		}
		cols = append(cols, displayEntries[i].PURL)
		if showRelation {
			cols = append(cols, formatRelation(&displayEntries[i]))
		}
		cols = append(cols, maintenance)
		cols = append(cols, buildIntegrityDisplay(displayEntries[i].Analysis))
		if _, err := fmt.Fprintln(tw, strings.Join(cols, "\t")); err != nil {
			return fmt.Errorf("failed to write table row: %w", err)
		}
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("failed to flush table output: %w", err)
	}
	// Summary box (always based on all entries)
	if err := renderSummaryBox(w, allEntries, len(displayEntries), filterActive); err != nil {
		return fmt.Errorf("failed to write summary box: %w", err)
	}
	return nil
}

// renderSummaryBox renders the summary line in a left-border box.
// When isFiltered is true, appends "showing X of Y" to indicate filtered output.
func renderSummaryBox(w io.Writer, allEntries []domainaudit.AuditEntry, shownCount int, isFiltered bool) error {
	s := computeSummary(allEntries)
	summaryLine := fmt.Sprintf("%d dependencies | ✅ %d ok | ⚠️ %d caution | 🔴 %d replace | 🔍 %d review",
		s.Total, s.OK, s.Caution, s.Replace, s.Review)
	if isFiltered {
		summaryLine += fmt.Sprintf(" | showing %d of %d", shownCount, s.Total)
	}
	bar := buildBar("── ", "Summary ", defaultBarWidth)
	if _, err := fmt.Fprintf(w, "\n%s\n│ %s\n└%s\n", bar, summaryLine, strings.Repeat("─", defaultBarWidth-1)); err != nil {
		return fmt.Errorf("failed to write summary box: %w", err)
	}
	return nil
}

// buildIntegrityDisplay returns the BUILD column text for table output.
// Format: "Label score" (e.g., "Hardened 8.1") or "—" for Ungraded/missing.
func buildIntegrityDisplay(a *domain.Analysis) string {
	if a == nil {
		return "—"
	}
	br := a.GetBuildHealthResult()
	if br == nil || br.Label == "" || br.Label == string(domain.BuildLabelUngraded) {
		return "—"
	}
	if scoreStr, ok := br.Meta["score"]; ok && scoreStr != "" && scoreStr != domain.ScoreUngraded {
		return fmt.Sprintf("%s %s", br.Label, scoreStr)
	}
	return br.Label
}

// enrichedJSONEntry is the DTO for --format json with full analysis data.
type enrichedJSONEntry struct {
	PURL                string   `json:"purl"`
	Verdict             string   `json:"verdict"`
	Lifecycle           string   `json:"lifecycle"`
	BuildIntegrity      string   `json:"build_integrity,omitempty"`
	BuildIntegrityScore *float64 `json:"build_integrity_score,omitempty"`
	Successor           string   `json:"successor,omitempty"`

	RepoURL         string   `json:"repo_url,omitempty"`
	Archived        bool     `json:"archived,omitempty"`
	OverallScore    float64  `json:"overall_score,omitempty"`
	DependentCount  int      `json:"dependent_count,omitempty"`
	StableVersion   string   `json:"stable_version,omitempty"`
	ProjectLicense  string   `json:"project_license,omitempty"`
	VersionLicenses []string `json:"version_licenses,omitempty"`

	// AdvisoryCount is the total number of advisories (direct + transitive).
	AdvisoryCount       int     `json:"advisory_count,omitempty"`
	MaxAdvisorySeverity string  `json:"max_advisory_severity,omitempty"`
	MaxCVSS3Score       float64 `json:"max_cvss3_score,omitempty"`

	// DirectAdvisoryCount is the number of advisories affecting the package directly.
	DirectAdvisoryCount int `json:"direct_advisory_count,omitempty"`
	// TransitiveAdvisoryCount is the number of advisories from transitive dependencies.
	TransitiveAdvisoryCount       int     `json:"transitive_advisory_count,omitempty"`
	MaxTransitiveAdvisorySeverity string  `json:"max_transitive_advisory_severity,omitempty"`
	MaxTransitiveCVSS3Score       float64 `json:"max_transitive_cvss3_score,omitempty"`

	Reason      string   `json:"reason,omitempty"`
	Error       string   `json:"error,omitempty"`
	Source      string   `json:"source,omitempty"`
	Via         string   `json:"via,omitempty"`
	Relation    string   `json:"relation,omitempty"`
	RelationVia []string `json:"relation_via,omitempty"`
}

type enrichedJSONOutput struct {
	Summary  jsonSummary         `json:"summary"`
	Entries  []enrichedJSONEntry `json:"packages"`
	Filtered bool                `json:"filtered,omitempty"`
	Shown    int                 `json:"shown"`
}

// renderScanJSON renders the JSON output format.
// filterActive indicates whether --show-only was explicitly provided.
func renderScanJSON(w io.Writer, allEntries, displayEntries []domainaudit.AuditEntry, filterActive bool) error {
	out := enrichedJSONOutput{
		Summary: computeSummary(allEntries),
		Entries: make([]enrichedJSONEntry, 0, len(displayEntries)),
	}
	if filterActive {
		out.Filtered = true
		out.Shown = len(displayEntries)
	}
	for i := range displayEntries {
		out.Entries = append(out.Entries, newEnrichedJSONEntry(&displayEntries[i]))
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
	maintenance, _ := entryMaintenanceEOL(e, "—")

	je := enrichedJSONEntry{
		PURL:        e.PURL,
		Verdict:     string(e.Verdict),
		Lifecycle:   maintenance,
		Error:       e.ErrorMsg,
		Source:      string(e.Source),
		Via:         e.Via,
		Relation:    e.Relation.String(),
		RelationVia: e.ViaParents,
	}

	a := e.Analysis
	if a == nil {
		je.BuildIntegrity = string(domain.BuildLabelUngraded)
		return je
	}

	je.RepoURL = a.RepoURL
	je.OverallScore = a.OverallScore
	je.DependentCount = a.DependentCount
	je.Successor = a.EOL.Successor
	je.Archived = a.IsArchived()

	if a.ReleaseInfo != nil {
		if a.ReleaseInfo.StableVersion != nil {
			je.StableVersion = a.ReleaseInfo.StableVersion.Version
		}

		if vd := a.ReleaseInfo.LatestVersionDetail(); vd != nil {
			je.AdvisoryCount = len(vd.Advisories)
			je.DirectAdvisoryCount = vd.DirectAdvisoryCount()
			if maxScore := vd.MaxCVSS3(); maxScore > 0 {
				je.MaxCVSS3Score = maxScore
				je.MaxAdvisorySeverity = domain.SeverityFromCVSS3(maxScore)
			}
			je.TransitiveAdvisoryCount = vd.TransitiveAdvisoryCount()
			if maxTScore := vd.MaxTransitiveCVSS3(); maxTScore > 0 {
				je.MaxTransitiveCVSS3Score = maxTScore
				je.MaxTransitiveAdvisorySeverity = domain.SeverityFromCVSS3(maxTScore)
			}
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
	if br := a.GetBuildHealthResult(); br != nil && br.Label != "" {
		je.BuildIntegrity = br.Label
		if scoreStr, ok := br.Meta["score"]; ok && scoreStr != "" && scoreStr != domain.ScoreUngraded {
			if score, err := strconv.ParseFloat(scoreStr, 64); err == nil {
				je.BuildIntegrityScore = &score
			}
		}
	} else {
		je.BuildIntegrity = string(domain.BuildLabelUngraded)
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
	header = append(header, "lifecycle", "build_integrity", "build_integrity_score", "successor", "advisory_count", "max_advisory_severity", "max_cvss3_score",
		"direct_advisory_count", "transitive_advisory_count", "max_transitive_advisory_severity", "max_transitive_cvss3_score",
		"repo_url", "source", "via")
	if err := cw.Write(header); err != nil {
		return fmt.Errorf("failed to write CSV header: %w", err)
	}
	for i := range entries {
		e := &entries[i]
		maintenance, _ := entryMaintenanceEOL(e, "")

		successor := ""
		repoURL := ""
		advisoryCount := ""
		maxSeverity := ""
		maxCVSS3Score := ""
		directAdvisoryCount := ""
		transitiveAdvisoryCount := ""
		maxTransitiveSeverity := ""
		maxTransitiveCVSS3Score := ""
		if a := e.Analysis; a != nil {
			successor = a.EOL.Successor
			repoURL = a.RepoURL
			if a.ReleaseInfo != nil {
				if vd := a.ReleaseInfo.LatestVersionDetail(); vd != nil {
					advisoryCount = fmt.Sprintf("%d", len(vd.Advisories))
					if maxScore := vd.MaxCVSS3(); maxScore > 0 {
						maxSeverity = domain.SeverityFromCVSS3(maxScore)
						maxCVSS3Score = fmt.Sprintf("%.1f", maxScore)
					}
					directAdvisoryCount = fmt.Sprintf("%d", vd.DirectAdvisoryCount())
					transitiveAdvisoryCount = fmt.Sprintf("%d", vd.TransitiveAdvisoryCount())
					if maxTScore := vd.MaxTransitiveCVSS3(); maxTScore > 0 {
						maxTransitiveSeverity = domain.SeverityFromCVSS3(maxTScore)
						maxTransitiveCVSS3Score = fmt.Sprintf("%.1f", maxTScore)
					}
				}
			}
		}

		buildIntegrity := string(domain.BuildLabelUngraded)
		buildIntegrityScore := ""
		if a := e.Analysis; a != nil {
			if br := a.GetBuildHealthResult(); br != nil {
				if br.Label != "" {
					buildIntegrity = br.Label
				}
				if scoreStr, ok := br.Meta["score"]; ok && scoreStr != "" && scoreStr != domain.ScoreUngraded {
					buildIntegrityScore = scoreStr
				}
			}
		}

		row := []string{string(e.Verdict), e.PURL}
		if showRelation {
			row = append(row, e.Relation.String(), strings.Join(e.ViaParents, ";"))
		}
		row = append(row, maintenance, buildIntegrity, buildIntegrityScore, successor, advisoryCount, maxSeverity, maxCVSS3Score,
			directAdvisoryCount, transitiveAdvisoryCount, maxTransitiveSeverity, maxTransitiveCVSS3Score,
			repoURL, string(e.Source), e.Via)
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
