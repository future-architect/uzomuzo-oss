package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/audit"
	"github.com/future-architect/uzomuzo-oss/internal/domain/scan"
)

// See ADR-0034.
type scanRenderOptions struct {
	Policy      scan.FailPolicy
	ArtifactURI string
	SBOM        bool
	ToolVersion string
}

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}
type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}
type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}
type sarifDriver struct {
	Name    string      `json:"name"`
	Version string      `json:"version"`
	Rules   []sarifRule `json:"rules"`
}
type sarifText struct {
	Text string `json:"text"`
}
type sarifConfiguration struct {
	Level string `json:"level"`
}
type sarifRule struct {
	ID                   string             `json:"id"`
	Name                 string             `json:"name"`
	ShortDescription     sarifText          `json:"shortDescription"`
	HelpURI              string             `json:"helpUri"`
	DefaultConfiguration sarifConfiguration `json:"defaultConfiguration"`
}
type sarifResult struct {
	RuleID              string            `json:"ruleId"`
	RuleIndex           int               `json:"ruleIndex"`
	Level               string            `json:"level"`
	Message             sarifText         `json:"message"`
	Locations           []sarifLocation   `json:"locations,omitempty"`
	PartialFingerprints map[string]string `json:"partialFingerprints"`
}
type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}
type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           *sarifRegion          `json:"region,omitempty"`
}
type sarifArtifactLocation struct {
	URI string `json:"uri"`
}
type sarifRegion struct {
	StartLine int `json:"startLine"`
}

func renderScanSARIF(w io.Writer, entries []audit.AuditEntry, opts scanRenderOptions) error {
	rules := sarifRules()
	indexes := make(map[string]int, len(rules))
	for i, r := range rules {
		indexes[r.ID] = i
	}
	uri := strings.TrimPrefix(filepath.ToSlash(opts.ArtifactURI), "./")
	results := make([]sarifResult, 0, len(entries))
	for i := range entries {
		e := &entries[i]
		matched := len(opts.Policy.Matches(e)) > 0
		if e.Verdict == audit.VerdictOK && !matched {
			continue
		}
		level := "note"
		if matched {
			level = "error"
		} else if e.Verdict == audit.VerdictReplace {
			level = "warning"
		}
		rule := sarifRuleID(e)
		identity := e.PURL
		if strings.HasPrefix(identity, "pkg:") {
			if versionless, err := purl.WithVersion(identity, ""); err == nil {
				identity = versionless
			}
		}
		artifact := uri
		if e.Source != audit.SourceDirect {
			artifact = ""
		}
		digest := sha256.Sum256([]byte(rule + "|" + identity + "|" + artifact))
		result := sarifResult{RuleID: rule, RuleIndex: indexes[rule], Level: level, Message: sarifText{Text: sarifMessage(e)}, PartialFingerprints: map[string]string{"uzomuzoFinding/v1": hex.EncodeToString(digest[:])}}
		if artifact != "" {
			if opts.SBOM {
				result.Locations = []sarifLocation{{PhysicalLocation: sarifPhysicalLocation{ArtifactLocation: sarifArtifactLocation{URI: artifact}}}}
			} else {
				for _, loc := range e.Locations {
					if loc.Line > 0 {
						result.Locations = append(result.Locations, sarifLocation{PhysicalLocation: sarifPhysicalLocation{ArtifactLocation: sarifArtifactLocation{URI: artifact}, Region: &sarifRegion{StartLine: loc.Line}}})
					}
				}
			}
		}
		results = append(results, result)
	}
	log := sarifLog{Schema: "https://json.schemastore.org/sarif-2.1.0.json", Version: "2.1.0", Runs: []sarifRun{{Tool: sarifTool{Driver: sarifDriver{Name: "uzomuzo", Version: opts.ToolVersion, Rules: rules}}, Results: results}}}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(log); err != nil {
		return fmt.Errorf("failed to encode SARIF output: %w", err)
	}
	return nil
}

func sarifRules() []sarifRule {
	definitions := []struct{ id, name, description string }{
		{"analysis-incomplete", "AnalysisIncomplete", "Dependency analysis is incomplete"},
		{string(scan.FailMalicious), "Malicious", "Dependency has a malicious-package advisory"},
		{string(scan.FailEOLConfirmed), "EOLConfirmed", "Dependency has confirmed end of life"},
		{string(scan.FailEOLEffective), "EOLEffective", "Dependency is effectively end of life"},
		{string(scan.FailEOLScheduled), "EOLScheduled", "Dependency has scheduled end of life"},
		{string(scan.FailArchived), "Archived", "Dependency repository is archived"},
		{string(scan.FailDisabled), "Disabled", "Dependency repository is disabled"},
		{string(scan.FailStalled), "Stalled", "Dependency maintenance has stalled"},
		{string(scan.FailLegacySafe), "LegacySafe", "Dependency is legacy-safe"},
		{string(scan.FailReviewNeeded), "ReviewNeeded", "Dependency needs manual review"},
		{string(scan.FailDormantRelease), "DormantRelease", "Dependency has a dormant-release marker"},
		{"active", "Active", "Dependency is active"},
	}
	rules := make([]sarifRule, 0, len(definitions))
	for _, d := range definitions {
		rules = append(rules, sarifRule{ID: "uzomuzo/" + d.id, Name: d.name, ShortDescription: sarifText{Text: d.description}, HelpURI: "https://github.com/future-architect/uzomuzo-oss/blob/main/docs/usage.md#output-formats", DefaultConfiguration: sarifConfiguration{Level: "warning"}})
	}
	return rules
}

func sarifRuleID(e *audit.AuditEntry) string {
	a := e.Analysis
	if a == nil || a.Error != nil {
		return "uzomuzo/analysis-incomplete"
	}
	if a.Malicious() {
		return "uzomuzo/" + string(scan.FailMalicious)
	}
	status := a.FinalMaintenanceStatus()
	switch status {
	case analysis.LabelEOLConfirmed:
		return "uzomuzo/" + string(scan.FailEOLConfirmed)
	case analysis.LabelEOLEffective:
		return "uzomuzo/" + string(scan.FailEOLEffective)
	case analysis.LabelEOLScheduled:
		return "uzomuzo/" + string(scan.FailEOLScheduled)
	}
	if a.IsArchived() {
		return "uzomuzo/" + string(scan.FailArchived)
	}
	if a.IsDisabled() {
		return "uzomuzo/" + string(scan.FailDisabled)
	}
	switch status {
	case analysis.LabelStalled:
		return "uzomuzo/" + string(scan.FailStalled)
	case analysis.LabelLegacySafe:
		return "uzomuzo/" + string(scan.FailLegacySafe)
	case analysis.LabelReviewNeeded:
		return "uzomuzo/" + string(scan.FailReviewNeeded)
	}
	if a.HasDormantRelease() {
		return "uzomuzo/" + string(scan.FailDormantRelease)
	}
	return "uzomuzo/active"
}

func sarifMessage(e *audit.AuditEntry) string {
	je := newEnrichedJSONEntry(e)
	label := je.Lifecycle
	if e.Analysis == nil {
		label = verdictLabel(e.Verdict)
	}
	summary := je.Reason
	if je.Error != "" {
		summary = je.Error
	} else if je.Malicious != nil {
		summary = je.Malicious.Summary
	}
	if a := e.Analysis; a != nil {
		if summary == "" {
			summary = a.EOL.Reason
		}
		if notice := a.DormantReleaseNotice(); notice != "" {
			if summary != "" {
				summary += "; "
			}
			summary += notice
		}
	}
	message := e.PURL + ": " + label
	if summary != "" {
		message += " — " + summary
	}
	if je.Successor != "" {
		message += "; Successor: " + je.Successor
	}
	return message
}
