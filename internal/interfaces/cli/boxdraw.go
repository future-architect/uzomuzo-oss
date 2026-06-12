package cli

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	analysispkg "github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	domainaudit "github.com/future-architect/uzomuzo-oss/internal/domain/audit"
	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
)

// defaultBarWidth is the character width of decorative ── bars in left-border output.
const defaultBarWidth = 60

// maxDisplayAdvisories is the maximum number of advisories shown inline.
// Remaining advisories are summarized with a deps.dev link.
const maxDisplayAdvisories = 3

// minBarPadding is the minimum number of trailing ─ characters on a bar line.
const minBarPadding = 3

// boxContext holds all data needed to render a single box entry.
type boxContext struct {
	w        io.Writer
	entry    *domainaudit.AuditEntry
	analysis *analysispkg.Analysis // shortcut: entry.Analysis (may be nil)
	barWidth int
}

// newBoxContext creates a boxContext from an AuditEntry.
func newBoxContext(w io.Writer, entry *domainaudit.AuditEntry, barWidth int) *boxContext {
	return &boxContext{
		w:        w,
		entry:    entry,
		analysis: entry.Analysis,
		barWidth: barWidth,
	}
}

// ---------------------------------------------------------------------------
// Border primitives (left-border only — no right border)
// ---------------------------------------------------------------------------

// writeTopBar writes: ── title ──────────...
func writeTopBar(ctx *boxContext) error {
	title := boxTitle(ctx.entry)
	bar := buildBar("──", " "+title+" ", ctx.barWidth)
	if _, err := fmt.Fprintln(ctx.w, bar); err != nil {
		return fmt.Errorf("failed to write top bar: %w", err)
	}
	return nil
}

// writeSectionBar writes: ├─ label ──────────...
func writeSectionBar(ctx *boxContext, label string) error {
	bar := buildBar("├─", " "+label+" ", ctx.barWidth)
	if _, err := fmt.Fprintln(ctx.w, bar); err != nil {
		return fmt.Errorf("failed to write section bar %q: %w", label, err)
	}
	return nil
}

// writeBottomBar writes: └──────────────────...
func writeBottomBar(ctx *boxContext) error {
	if _, err := fmt.Fprintln(ctx.w, "└"+strings.Repeat("─", ctx.barWidth-1)); err != nil {
		return fmt.Errorf("failed to write bottom bar: %w", err)
	}
	return nil
}

// writeLine writes: │ content
// Long text lines are word-wrapped at barWidth when isWrappableLine returns
// true for known free-text fields such as Reason: and Description:.
// All other lines — including URLs, identifiers, structured data, and
// evidence summary lines — are left unwrapped to preserve terminal link
// detection and copy-paste usability.
func writeLine(ctx *boxContext, format string, args ...any) error {
	content := fmt.Sprintf(format, args...)
	maxWidth := ctx.barWidth - 2 // subtract "│ " prefix width

	// Only wrap known free-text fields; skip everything else.
	if maxWidth <= 0 || utf8.RuneCountInString(content) <= maxWidth || !isWrappableLine(content) {
		if _, err := fmt.Fprintf(ctx.w, "│ %s\n", content); err != nil {
			return fmt.Errorf("failed to write box line: %w", err)
		}
		return nil
	}

	lines := wrapContent(content, maxWidth)
	for _, line := range lines {
		if _, err := fmt.Fprintf(ctx.w, "│ %s\n", line); err != nil {
			return fmt.Errorf("failed to write box line: %w", err)
		}
	}
	return nil
}

// isWrappableLine returns true only for the labeled free-text fields handled
// by writeLine: verdict+reason lines (emoji prefix), Catalog Reason, and
// Description: lines.
// Unlabeled description text is wrapped separately by writeBoxIdentity.
// Everything else — including URLs, identifiers, structured data, and
// evidence summary lines — is left unwrapped.
func isWrappableLine(s string) bool {
	trimmed := strings.TrimLeft(s, " ")

	// Never wrap lines that contain URLs — preserve terminal link detection.
	if strings.Contains(trimmed, "://") {
		return false
	}

	switch {
	case strings.HasPrefix(trimmed, "✅"),
		strings.HasPrefix(trimmed, "⚠️"),
		strings.HasPrefix(trimmed, "🔴"),
		strings.HasPrefix(trimmed, "🔍"):
		// Verdict line: "icon Label: reason"
		return true
	case strings.HasPrefix(trimmed, "Catalog Reason:"):
		return true
	case strings.HasPrefix(trimmed, "Description:"):
		return true
		// EOL evidence summary lines ("[npmjs] ...") are already condensed summaries
		// — wrapping them reduces readability. Let the terminal handle overflow.
	}
	return false
}

// wrapContent breaks content into lines that fit within maxWidth runes.
// The first line keeps the original indent. Continuation lines are indented
// to align with the text after the label (e.g., "Reason: " → 8-char indent).
func wrapContent(content string, maxWidth int) []string {
	// Determine continuation indent from label prefix (e.g., "Reason: " → 8).
	indent := findLabelIndent(content)
	if indent >= maxWidth/2 {
		// Label is too wide for meaningful wrap — fall back to 2-char indent.
		indent = 2
	}
	return wrapContentWithIndent(content, maxWidth, indent)
}

// wrapContentWithIndent breaks content into lines with a caller-specified
// continuation indent. Use this for unlabeled free text where the automatic
// label-detection in wrapContent would misfire on content containing ": ".
func wrapContentWithIndent(content string, maxWidth, indent int) []string {

	var result []string
	remaining := content
	first := true
	for utf8.RuneCountInString(remaining) > 0 {
		budget := maxWidth
		if !first {
			budget = maxWidth - indent
		}
		if budget <= 0 {
			// Budget too small for meaningful wrapping — return content as-is.
			if first {
				result = append(result, remaining)
			} else {
				result = append(result, strings.Repeat(" ", indent)+remaining)
			}
			break
		}
		if utf8.RuneCountInString(remaining) <= budget {
			if first {
				result = append(result, remaining)
			} else {
				result = append(result, strings.Repeat(" ", indent)+remaining)
			}
			break
		}

		// Find the last space within budget for a clean break.
		runes := []rune(remaining)
		breakAt := -1
		for i := budget; i > 0; i-- {
			if runes[i] == ' ' {
				breakAt = i
				break
			}
		}
		if breakAt <= 0 {
			// No whitespace found within budget — preserve the unbroken
			// token (e.g. URL/identifier) instead of splitting mid-token.
			if first {
				result = append(result, remaining)
			} else {
				result = append(result, strings.Repeat(" ", indent)+remaining)
			}
			break
		}

		line := string(runes[:breakAt])
		if first {
			result = append(result, line)
			first = false
		} else {
			result = append(result, strings.Repeat(" ", indent)+line)
		}
		remaining = strings.TrimLeft(string(runes[breakAt:]), " ")
	}
	return result
}

// findLabelIndent returns the number of characters to use as continuation
// indent, based on the label prefix of the content (e.g., "Reason: " → 8).
// For lines without a recognized label pattern, returns 2.
func findLabelIndent(s string) int {
	trimmed := strings.TrimLeft(s, " ")
	leadingSpaces := utf8.RuneCountInString(s) - utf8.RuneCountInString(trimmed)

	// Look for "Label: " pattern — use rune count (not byte index) for
	// correct alignment with multi-byte characters (e.g., emoji prefixes).
	idx := strings.Index(trimmed, ": ")
	if idx > 0 && idx < 30 {
		labelWidth := utf8.RuneCountInString(trimmed[:idx])
		return leadingSpaces + labelWidth + 2 // include ": "
	}
	return 2
}

// buildBar constructs a decorative bar like "── title ────────..." or "├─ label ────────...".
// Uses rune count (not byte count) so multi-byte box-drawing characters size correctly.
func buildBar(prefix, middle string, width int) string {
	remaining := width - utf8.RuneCountInString(prefix) - utf8.RuneCountInString(middle)
	if remaining < 0 {
		remaining = 0
	} else if remaining < minBarPadding {
		remaining = minBarPadding
	}
	return prefix + middle + strings.Repeat("─", remaining)
}

// ---------------------------------------------------------------------------
// Title & verdict helpers
// ---------------------------------------------------------------------------

// boxTitle returns the PURL with optional source/relation annotation for the top bar.
func boxTitle(e *domainaudit.AuditEntry) string {
	purl := e.PURL
	if e.Source != domainaudit.SourceDirect {
		return fmt.Sprintf("[%s] %s", sourceDisplayName(e.Source), purl)
	}
	if e.Relation == depparser.RelationTransitive {
		return fmt.Sprintf("%s (transitive)", purl)
	}
	return purl
}

// verdictIcon returns the emoji for a given verdict.
func verdictIcon(v domainaudit.Verdict) string {
	switch v {
	case domainaudit.VerdictOK:
		return "✅"
	case domainaudit.VerdictCaution:
		return "⚠️"
	case domainaudit.VerdictReplace:
		return "🔴"
	default:
		return "🔍"
	}
}

// verdictLabel returns the human-readable label for a verdict.
func verdictLabel(v domainaudit.Verdict) string {
	switch v {
	case domainaudit.VerdictOK:
		return "OK"
	case domainaudit.VerdictCaution:
		return "Caution"
	case domainaudit.VerdictReplace:
		return "Replace"
	default:
		return "Review Needed"
	}
}

// ---------------------------------------------------------------------------
// Orchestrators
// ---------------------------------------------------------------------------

// renderBoxEntry writes a complete left-border box for one AuditEntry.
func renderBoxEntry(w io.Writer, entry *domainaudit.AuditEntry) error {
	ctx := newBoxContext(w, entry, defaultBarWidth)

	if entry.Analysis == nil || entry.Analysis.Error != nil {
		return renderBoxEntryError(ctx)
	}

	for _, fn := range []func() error{
		func() error { return writeTopBar(ctx) },
		func() error { return writeBoxIdentity(ctx) },
		func() error { return writeBoxVerdict(ctx) },
		func() error { return writeBoxSignals(ctx) },
		func() error { return writeBoxEOL(ctx) },
		func() error { return writeBoxOrigin(ctx) },
		func() error { return writeBoxHealth(ctx) },
		func() error { return writeBoxReleases(ctx) },
		func() error { return writeBoxBuildIntegrity(ctx) },
		func() error { return writeBoxLinks(ctx) },
		func() error { return writeBottomBar(ctx) },
	} {
		if err := fn(); err != nil {
			return fmt.Errorf("failed to render box for %s: %w", entry.PURL, err)
		}
	}
	return nil
}

// renderBoxEntryError writes a minimal box for entries with nil analysis or errors.
func renderBoxEntryError(ctx *boxContext) error {
	wrap := func(err error) error {
		return fmt.Errorf("failed to render error box for %s: %w", ctx.entry.PURL, err)
	}
	if err := writeTopBar(ctx); err != nil {
		return wrap(err)
	}
	// Skip Package: line when identical to top bar title (consistent with writeBoxIdentity)
	if ctx.entry.PURL != boxTitle(ctx.entry) {
		if err := writeLine(ctx, "Package: %s", ctx.entry.PURL); err != nil {
			return wrap(err)
		}
	}
	if ctx.entry.Via != "" {
		if err := writeLine(ctx, "Via: %s", ctx.entry.Via); err != nil {
			return wrap(err)
		}
	}
	icon := verdictIcon(ctx.entry.Verdict)
	label := verdictLabel(ctx.entry.Verdict)
	if ctx.entry.ErrorMsg != "" {
		if err := writeLine(ctx, "%s %s (error: %s)", icon, label, ctx.entry.ErrorMsg); err != nil {
			return wrap(err)
		}
	} else {
		if err := writeLine(ctx, "%s %s", icon, label); err != nil {
			return wrap(err)
		}
	}
	if err := writeBottomBar(ctx); err != nil {
		return wrap(err)
	}
	return nil
}
