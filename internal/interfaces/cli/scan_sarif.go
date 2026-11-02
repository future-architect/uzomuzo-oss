package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/future-architect/uzomuzo-oss/internal/common/purl"
	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/audit"
	"github.com/future-architect/uzomuzo-oss/internal/domain/scan"
)

const (
	sarifRuleAnalysisIncomplete = "uzomuzo/analysis-incomplete"
	sarifRuleMalicious          = "uzomuzo/malicious"
	sarifRuleEOLConfirmed       = "uzomuzo/eol-confirmed"
	sarifRuleEOLEffective       = "uzomuzo/eol-effective"
	sarifRuleEOLScheduled       = "uzomuzo/eol-scheduled"
	sarifRuleArchived           = "uzomuzo/archived"
	sarifRuleDisabled           = "uzomuzo/disabled"
	sarifRuleStalled            = "uzomuzo/stalled"
	sarifRuleLegacySafe         = "uzomuzo/legacy-safe"
	sarifRuleReviewNeeded       = "uzomuzo/review-needed"
	sarifRuleDormantRelease     = "uzomuzo/dormant-release"
	sarifRuleActive             = "uzomuzo/active"
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
	uri := sarifArtifactURI(opts.ArtifactURI)
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

// sarifArtifactURI turns an input path into a relative URI reference. Without escaping,
// "deps#old.txt" reads as file "deps" with a fragment and "a%20b.txt" as "a b.txt".
func sarifArtifactURI(path string) string {
	slashed := strings.TrimPrefix(filepath.ToSlash(path), "./")
	if isWindowsAbsolute(slashed) {
		// As a plain reference, "C:/repo/go.mod" would be escaped into the relative "./C:/repo/go.mod".
		return (&url.URL{Scheme: "file", Path: "/" + slashed}).String()
	}
	return (&url.URL{Path: slashed}).String()
}

func isWindowsAbsolute(slashed string) bool {
	if len(slashed) < 3 || slashed[1] != ':' || slashed[2] != '/' {
		return false
	}
	c := slashed[0] | 0x20 // lowercase an ASCII letter
	return 'a' <= c && c <= 'z'
}

func sarifRules() []sarifRule {
	definitions := []struct{ id, name, description string }{
		{sarifRuleAnalysisIncomplete, "AnalysisIncomplete", "Dependency analysis is incomplete"},
		{sarifRuleMalicious, "Malicious", "Dependency has a malicious-package advisory"},
		{sarifRuleEOLConfirmed, "EOLConfirmed", "Dependency has confirmed end of life"},
		{sarifRuleEOLEffective, "EOLEffective", "Dependency is effectively end of life"},
		{sarifRuleEOLScheduled, "EOLScheduled", "Dependency has scheduled end of life"},
		{sarifRuleArchived, "Archived", "Dependency repository is archived"},
		{sarifRuleDisabled, "Disabled", "Dependency repository is disabled"},
		{sarifRuleStalled, "Stalled", "Dependency maintenance has stalled"},
		{sarifRuleLegacySafe, "LegacySafe", "Dependency is legacy-safe"},
		{sarifRuleReviewNeeded, "ReviewNeeded", "Dependency needs manual review"},
		{sarifRuleDormantRelease, "DormantRelease", "Dependency has a dormant-release marker"},
		{sarifRuleActive, "Active", "Dependency is active"},
	}
	rules := make([]sarifRule, 0, len(definitions))
	for _, d := range definitions {
		rules = append(rules, sarifRule{ID: d.id, Name: d.name, ShortDescription: sarifText{Text: d.description}, HelpURI: "https://github.com/future-architect/uzomuzo-oss/blob/main/docs/usage.md#output-formats", DefaultConfiguration: sarifConfiguration{Level: "warning"}})
	}
	return rules
}

func sarifRuleID(e *audit.AuditEntry) string {
	a := e.Analysis
	if a == nil || a.Error != nil {
		return sarifRuleAnalysisIncomplete
	}
	if a.Malicious() {
		return sarifRuleMalicious
	}
	status := a.FinalMaintenanceStatus()
	switch status {
	case analysis.LabelEOLConfirmed:
		return sarifRuleEOLConfirmed
	case analysis.LabelEOLEffective:
		return sarifRuleEOLEffective
	case analysis.LabelEOLScheduled:
		return sarifRuleEOLScheduled
	}
	if a.IsArchived() {
		return sarifRuleArchived
	}
	if a.IsDisabled() {
		return sarifRuleDisabled
	}
	switch status {
	case analysis.LabelStalled:
		return sarifRuleStalled
	case analysis.LabelLegacySafe:
		return sarifRuleLegacySafe
	case analysis.LabelReviewNeeded:
		return sarifRuleReviewNeeded
	}
	if a.HasDormantRelease() {
		return sarifRuleDormantRelease
	}
	return sarifRuleActive
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
