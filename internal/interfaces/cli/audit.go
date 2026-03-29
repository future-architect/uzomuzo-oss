package cli

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"text/tabwriter"

	auditapp "github.com/future-architect/uzomuzo-oss/internal/application/audit"
	domainaudit "github.com/future-architect/uzomuzo-oss/internal/domain/audit"
	domaincfg "github.com/future-architect/uzomuzo-oss/internal/domain/config"
	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
)

// ErrAuditReplaceFound is returned by RunAudit when at least one dependency
// has a "replace" verdict, signaling the caller to exit with code 1.
var ErrAuditReplaceFound = errors.New("audit: one or more dependencies require replacement")

// RunAudit is the entry point for the "audit" subcommand.
//
// Input resolution order:
//  1. sbomPath (non-empty): CycloneDX SBOM JSON file, or "-" for stdin
//  2. filePath (non-empty): go.mod convenience fallback
//  3. Auto-detect: look for go.mod in current working directory
//
// Output: table (default), json, or csv via format parameter.
// Returns ErrAuditReplaceFound if any verdict is "replace".
//
// The parsers parameter provides available DependencyParser implementations,
// keyed by input type ("sbom" and "gomod"). This avoids the Interfaces layer
// importing Infrastructure directly (DDD layer compliance).
//
// DDD Layer: Interfaces (CLI handler, delegates to Application)
func RunAudit(ctx context.Context, cfg *domaincfg.Config, sbomPath, filePath, format string, parsers map[string]depparser.DependencyParser) error {
	data, parser, err := resolveAuditInput(sbomPath, filePath, parsers)
	if err != nil {
		return fmt.Errorf("failed to resolve audit input: %w", err)
	}

	analysisService := createAnalysisService(cfg)
	auditService := auditapp.NewService(analysisService)

	entries, hasReplace, err := auditService.Run(ctx, parser, data)
	if err != nil {
		return fmt.Errorf("audit failed: %w", err)
	}

	if err := renderAuditOutput(os.Stdout, entries, format); err != nil {
		return fmt.Errorf("failed to render output: %w", err)
	}

	if hasReplace {
		return ErrAuditReplaceFound
	}
	return nil
}

// resolveAuditInput determines the input data and parser based on flags.
func resolveAuditInput(sbomPath, filePath string, parsers map[string]depparser.DependencyParser) ([]byte, depparser.DependencyParser, error) {
	// Priority 1: SBOM input
	if sbomPath != "" {
		var data []byte
		var err error
		if sbomPath == "-" {
			data, err = io.ReadAll(os.Stdin)
		} else {
			data, err = os.ReadFile(sbomPath)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read SBOM from '%s': %w", sbomPath, err)
		}
		return data, parsers["sbom"], nil
	}

	// Priority 2: Explicit go.mod path
	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read file '%s': %w", filePath, err)
		}
		return data, parsers["gomod"], nil
	}

	// Priority 3: Auto-detect go.mod in cwd
	if data, err := os.ReadFile("go.mod"); err == nil {
		slog.Info("auto-detected go.mod in current directory")
		return data, parsers["gomod"], nil
	}

	return nil, nil, fmt.Errorf("no input: use --sbom <file>, --file <go.mod>, or run from a directory with go.mod")
}

// renderAuditOutput renders audit entries in the specified format.
func renderAuditOutput(w io.Writer, entries []domainaudit.AuditEntry, format string) error {
	switch strings.ToLower(format) {
	case "json":
		return renderJSON(w, entries)
	case "csv":
		return renderCSV(w, entries)
	case "table", "":
		return renderTable(w, entries)
	default:
		return fmt.Errorf("unsupported format %q (use table, json, or csv)", format)
	}
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

func renderTable(w io.Writer, entries []domainaudit.AuditEntry) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	// Header uses "LIFECYCLE" intentionally for backward compatibility with existing tooling/scripts.
	_, _ = fmt.Fprintln(tw, "VERDICT\tPURL\tLIFECYCLE\tEOL")

	for i := range entries {
		maintenance, eol := entryMaintenanceEOL(&entries[i], "—")
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", entries[i].Verdict, entries[i].PURL, maintenance, eol)
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("failed to flush table output: %w", err)
	}

	s := computeSummary(entries)
	_, _ = fmt.Fprintf(w, "\nSummary: %d dependencies | %d ok | %d caution | %d replace | %d review\n",
		s.Total, s.OK, s.Caution, s.Replace, s.Review)
	return nil
}

type jsonEntry struct {
	PURL      string `json:"purl"`
	Verdict   string `json:"verdict"`
	Lifecycle string `json:"lifecycle"`
	EOL       string `json:"eol"`
	Error     string `json:"error,omitempty"`
}

type jsonOutput struct {
	Summary jsonSummary `json:"summary"`
	Entries []jsonEntry `json:"packages"`
}

type jsonSummary struct {
	Total   int `json:"total"`
	OK      int `json:"ok"`
	Caution int `json:"caution"`
	Replace int `json:"replace"`
	Review  int `json:"review"`
}

func renderJSON(w io.Writer, entries []domainaudit.AuditEntry) error {
	out := jsonOutput{
		Summary: computeSummary(entries),
		Entries: make([]jsonEntry, 0, len(entries)),
	}
	for i := range entries {
		maintenance, eol := entryMaintenanceEOL(&entries[i], "—")
		out.Entries = append(out.Entries, jsonEntry{
			PURL:      entries[i].PURL,
			Verdict:   string(entries[i].Verdict),
			Lifecycle: maintenance,
			EOL:       eol,
			Error:     entries[i].ErrorMsg,
		})
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("failed to encode JSON output: %w", err)
	}
	return nil
}

func renderCSV(w io.Writer, entries []domainaudit.AuditEntry) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"verdict", "purl", "lifecycle", "eol"}); err != nil {
		return fmt.Errorf("failed to write CSV header: %w", err)
	}
	for i := range entries {
		maintenance, eol := entryMaintenanceEOL(&entries[i], "")
		if err := cw.Write([]string{string(entries[i].Verdict), entries[i].PURL, maintenance, eol}); err != nil {
			return fmt.Errorf("failed to write CSV row for %s: %w", entries[i].PURL, err)
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("failed to flush CSV output: %w", err)
	}
	return nil
}
