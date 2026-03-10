package eoltext

// Unified EOL / deprecation text detector.
//
// DDD Layer: Infrastructure (utility)
// Responsibility: Provide reusable phrase/context/successor extraction for README, PyPI, NuGet messages.
//
// Component-level suppression policy:
//   We frequently encounter release notes / descriptions that start with a *single API element* deprecation
//   such as: "Deprecated function foo() in this project is deprecated permanently." Although the string
//   contains strong / explicit tokens ("this project", "deprecated permanently"), the intent is NOT that the
//   whole project/package is EOL—only the function.
//
//   Policy rules:
//     1. If the FIRST sentence begins with a component-level pattern (Deprecated function/method/class/module ...),
//        we do NOT escalate that sentence to a project-level match.
//     2. If subsequent independent sentences (after a period or newline) contain project-level explicit or strong
//        phrases (e.g. "This project has been discontinued", "This project is deprecated"), we DO classify based on
//        those later sentences. In effect we ignore only the leading component sentence, then re-run detection.
//     3. If no later sentence contains a qualifying project-level phrase, result remains KindNone.
//
//   This heuristic reduces false positives while still allowing legitimate multi-sentence announcements to be
//   detected. Tests cover single-sentence suppression and two-sentence promotion scenarios.

import (
	"regexp"
	"strings"

	"github.com/future-architect/uzomuzo/internal/common/eolphrases"
)

// Platform language/runtime version prefix (beginning of sentence) used to suppress generic
// support-drop notes that are not project/package EOL announcements.
var platformVersionPrefix = regexp.MustCompile(`(?i)^(python|node(?:\.js)?|nodejs|ruby|java|go|golang|php|perl|rust|dotnet|\.net|c#|c\+\+|scala|kotlin|swift|elixir|erlang|haskell)\s+[0-9]`)

// sentenceLooksLikePlatformVersionNotice returns true if the sentence appears to announce
// only a platform/runtime version support drop, without project tokens, thus should not
// be treated as package/project EOL when triggered by an ambiguous generic phrase.
func sentenceLooksLikePlatformVersionNotice(sentence string, projectTokens []string) bool {
	s := strings.ToLower(strings.TrimSpace(sentence))
	if !platformVersionPrefix.MatchString(s) {
		return false
	}
	for _, tk := range projectTokens {
		if tk == "" {
			continue
		}
		if strings.Contains(s, tk) { // has project scope mention -> not a pure platform version note
			return false
		}
	}
	return true
}

// containsCI reports case-insensitive membership in a slice.
func containsCI(list []string, v string) bool {
	vLower := strings.ToLower(v)
	for _, e := range list {
		if strings.ToLower(e) == vLower {
			return true
		}
	}
	return false
}

// DetectionKind classifies how a match was obtained (for future confidence tuning).
type DetectionKind int

const (
	KindNone DetectionKind = iota
	KindStrong
	KindContextual
	KindExplicit
)

// String returns a lowercase label suitable for JSON serialization.
func (k DetectionKind) String() string {
	switch k {
	case KindStrong:
		return "strong"
	case KindContextual:
		return "contextual"
	case KindExplicit:
		return "explicit"
	default:
		return ""
	}
}

// DetectionResult contains match details.
type DetectionResult struct {
	Matched   bool
	Kind      DetectionKind
	Phrase    string
	Successor string
	Date      string // EOL date extracted from date-anchored contextual patterns (e.g. "2025-06-30")
}

// SourceKind distinguishes detection source variants for unified lifecycle detection.
type SourceKind int

const (
	SourcePyPI SourceKind = iota
	SourceReadme
	SourceShortMessage
)

// rxDateLike recognizes date strings that may appear in contextual pattern capture groups.
var rxDateLike = regexp.MustCompile(`(?i)^\d{4}[-/]\d{2}[-/]\d{2}$|^\w+\s+\d{1,2},?\s+\d{4}$`)

// extractDateFromSubmatch returns the first capture group that looks like a date.
func extractDateFromSubmatch(groups []string) string {
	for _, g := range groups {
		g = strings.TrimSpace(g)
		if g != "" && rxDateLike.MatchString(g) {
			return g
		}
	}
	return ""
}

// LifecycleDetectOpts bundles inputs for lifecycle deprecation detection.
type LifecycleDetectOpts struct {
	Source      SourceKind
	PackageName string // for PyPI (package normalized name)
	RepoName    string // for README (repo last segment)
	Text        string // Unified text (PyPI: summary+"\n"+description merged by caller; README: raw)
}

// internal config (kept private to prevent uncontrolled proliferation of knobs externally)
type detectorConfig struct {
	strongPhrases       []string
	negativeSubs        []string
	contextualPatterns  []*regexp.Regexp
	explicitPattern     *regexp.Regexp
	componentPattern    *regexp.Regexp
	requireProjectToken bool
	successorPatterns   []*regexp.Regexp
	maxBytes            int
	// Ambiguous strong phrases (e.g., generic "no longer supported") that require
	// presence of a project token in the same sentence ("this project", package name) to qualify.
	ambiguousStrong []string
	// Project tokens (lower-case) that establish sentence-level project scope.
	projectTokens []string
	// Sentence-level negatives: if any of these appear in the same sentence as an
	// EOL phrase, only that sentence is skipped (unlike negativeSubs which abort all detection).
	sentenceNegatives []string
}

// strongPhrasesFromCatalog returns the TierStrong phrases from the canonical catalog.
// This replaces the formerly hard-coded 7-word list.
func strongPhrasesFromCatalog() []string {
	return eolphrases.TextsAtTier(eolphrases.TierStrong)
}

var negativeContexts = []string{
	"not deprecated",
	"still maintained",
}

// LabeledPattern pairs a human-readable label with a compiled regex.
// Used by keyword-stats to report per-pattern effectiveness.
type LabeledPattern struct {
	Label string
	Rx    *regexp.Regexp
}

var contextualEOLPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b(has\s+(reached|entered)|reaches|entered|is|are|becomes|becoming|will\s+(be|reach|enter)|was|were|has\s+been|have\s+been)\s+(end[\s-]?of[\s-]?life|eol)\b`),
	regexp.MustCompile(`\b(end[\s-]?of[\s-]?life|eol)\s+(for|of|on)\s+(this|the)\s+(project|artifact|library|module|package|component|release|version)\b`),
	regexp.MustCompile(`\b(end[\s-]?of[\s-]?life|eol)\b(?:\W+\w+){0,4}\s+\b(project|artifact|library|module|package|component|release|version)\b`),
	regexp.MustCompile(`(?i)moved\s+into\s+read-only\s+mode`),
	// Date-based EOL patterns: indicate scheduled or past EOL with a specific date.
	// DATE captures: YYYY-MM-DD, YYYY/MM/DD, or "Month DD, YYYY" / "Month DD YYYY".
	regexp.MustCompile(`(?i)\bwill\s+be\s+(removed|retired|sunset|sunsetted)\s+(on|by)\s+(\d{4}[-/]\d{2}[-/]\d{2}|\w+\s+\d{1,2},?\s+\d{4})`),
	regexp.MustCompile(`(?i)\bsupport\s+(continues|provided)\s+until\s+(\d{4}[-/]\d{2}[-/]\d{2}|\w+\s+\d{1,2},?\s+\d{4})`),
	regexp.MustCompile(`(?i)\bsecurity\s+fixes\s+until\s+(\d{4}[-/]\d{2}[-/]\d{2}|\w+\s+\d{1,2},?\s+\d{4})`),
	regexp.MustCompile(`(?i)\bafter\s+(\d{4}[-/]\d{2}[-/]\d{2}|\w+\s+\d{1,2},?\s+\d{4})\s+(no|without)\s+(support|updates)`),
}

// ContextualPatternsForStats returns labeled contextual EOL regex patterns
// for keyword effectiveness analysis. Each pattern is paired with a descriptive label.
func ContextualPatternsForStats() []LabeledPattern {
	labels := []string{
		"verb + end-of-life/eol",
		"end-of-life for/of this project",
		"end-of-life near project/package",
		"moved into read-only mode",
		"will be retired/removed on DATE",
		"support continues/provided until DATE",
		"security fixes until DATE",
		"after DATE no support/updates",
	}
	out := make([]LabeledPattern, len(contextualEOLPatterns))
	for i, rx := range contextualEOLPatterns {
		out[i] = LabeledPattern{Label: labels[i], Rx: rx}
	}
	return out
}

// rxReadmeExplicit is the broadest explicit EOL regex (Readme variant).
// Promoted to package level so it can be shared between DetectLifecycle and stats.
var rxReadmeExplicit = regexp.MustCompile(`(?i)\b(this (project|package|repository|repo) (is )?(now )?(deprecated|unmaintained|abandoned|dead|sunset|sunsetted|decommissioned|obsoleted)|sunsetting this (project|package|repository)|consider this (project|package|repository|repo) (obsolete|archived|deprecated)|no longer (maintained|supported)|reached end of life|final release)\b`)

// ExplicitPatternsForStats returns the broadest explicit EOL regex (Readme variant)
// for keyword effectiveness analysis.
func ExplicitPatternsForStats() []LabeledPattern {
	return []LabeledPattern{
		{Label: "this project/package is deprecated/...", Rx: rxReadmeExplicit},
	}
}

// componentAtStart detects lines that begin with a component-level deprecation notice
// (e.g., "Deprecated function foo() ...") which should not be escalated to a
// project/package-level explicit deprecation even if the text later contains
// "this project" or "this package". This reduces false positives where only
// an API element is deprecated.
var componentAtStart = regexp.MustCompile(`(?i)^\s*deprecated\s+(argument|parameter|function|method|class|module)\b`)

// PyPI specific regex sets (migrated from successor helpers)
var (
	rxPyPIExplicit  = regexp.MustCompile(`(?i)\b(this (project|package) (is )?(now )?(deprecated|unmaintained|abandoned|dead|sunset|sunsetted|decommissioned|obsoleted)|no longer (maintained|supported)|reached end of life|final release)\b`)
	rxPyPISuccessor = []*regexp.Regexp{
		regexp.MustCompile(`(?i)deprecated[^\.\n]*?use\s+([A-Za-z0-9][A-Za-z0-9._-]+)\s+instead`),
		regexp.MustCompile(`(?i)use\s+([A-Za-z0-9][A-Za-z0-9._-]+)\s+instead`),
		regexp.MustCompile(`(?i)replaced\s+(by|with)\s+([A-Za-z0-9][A-Za-z0-9._-]+)`),
		regexp.MustCompile(`(?i)superseded\s+by\s+([A-Za-z0-9][A-Za-z0-9._-]+)`),
		regexp.MustCompile(`(?i)moved\s+to\s+([A-Za-z0-9][A-Za-z0-9._-]+)`),
		regexp.MustCompile(`(?i)successor\s+is\s+([A-Za-z0-9][A-Za-z0-9._-]+)`),
		regexp.MustCompile(`(?i)consolidated\s+into\s+([A-Za-z0-9][A-Za-z0-9._-]+)`),
		regexp.MustCompile(`(?i)integrated\s+into\s+([A-Za-z0-9][A-Za-z0-9._-]+)`),
	}
	rxPyPIComponent   = regexp.MustCompile(`(?i)deprecated (argument|parameter|function|method|class|module)`)
	pyPINegativeExtra = []string{"will be deprecated"}
)

// DetectLifecycle is a unified detector replacing DetectPyPI and DetectReadme.
func DetectLifecycle(opts LifecycleDetectOpts) DetectionResult {
	var (
		text          string
		projectTokens []string
		explicitRx    *regexp.Regexp
		successorPats []*regexp.Regexp
		negatives     []string
		maxBytes      int
	)
	switch opts.Source {
	case SourcePyPI:
		text = strings.TrimSpace(opts.Text)
		projectTokens = []string{"this project", "this package", strings.ToLower(opts.PackageName)}
		explicitRx = rxPyPIExplicit
		successorPats = rxPyPISuccessor
		negatives = append(append([]string{}, negativeContexts...), pyPINegativeExtra...)
		maxBytes = 200_000
	case SourceReadme:
		text = strings.TrimSpace(opts.Text)
		projectTokens = []string{"this project", "this package", "this repository", "this repo", strings.ToLower(opts.RepoName)}
		explicitRx = rxReadmeExplicit
		negatives = negativeContexts
		maxBytes = 400_000
		successorPats = []*regexp.Regexp{
			regexp.MustCompile(`(?i)deprecated[^\.\n]*?use\s+(@?[A-Za-z0-9][A-Za-z0-9._@/\-]*)\s+instead`),
			regexp.MustCompile(`(?i)use\s+(@?[A-Za-z0-9][A-Za-z0-9._@/\-]*)\s+instead`),
			regexp.MustCompile(`(?i)replaced\s+(by|with)\s+(@?[A-Za-z0-9][A-Za-z0-9._@/\-]*)`),
			regexp.MustCompile(`(?i)superseded\s+by\s+(@?[A-Za-z0-9][A-Za-z0-9._@/\-]*)`),
			regexp.MustCompile(`(?i)moved\s+to\s+(@?[A-Za-z0-9][A-Za-z0-9._@/\-]*)`),
			regexp.MustCompile(`(?i)renamed\s+to\s+(@?[A-Za-z0-9][A-Za-z0-9._@/\-]*)`),
			regexp.MustCompile(`(?i)successor\s+is\s+(@?[A-Za-z0-9][A-Za-z0-9._@/\-]*)`),
			regexp.MustCompile(`(?i)consolidated\s+into\s+(@?[A-Za-z0-9][A-Za-z0-9._@/\-]*)`),
			regexp.MustCompile(`(?i)integrated\s+into\s+(@?[A-Za-z0-9][A-Za-z0-9._@/\-]*)`),
			regexp.MustCompile(`(?i)(\w[\w._@/-]*)\s+supersedes`),
		}
	case SourceShortMessage:
		text = strings.TrimSpace(opts.Text)
		// Include generic project tokens so ambiguous strong phrases like "no longer maintained"
		// are recognized when preceded by "This project ..." in short messages (release notes, registry messages).
		projectTokens = []string{"this project", "this package", strings.ToLower(opts.PackageName)}
		// For short messages treat only explicit self-references ("this project/package ...") as explicit.
		// Generic "reached end of life" is handled by contextual patterns to keep KindContextual expectations.
		explicitRx = regexp.MustCompile(`(?i)\b(this (project|package) (is )?(now )?(deprecated|unmaintained|abandoned|dead|sunset|sunsetted|decommissioned|obsoleted)|final release)\b`)
		negatives = negativeContexts
		maxBytes = 20_000
		successorPats = []*regexp.Regexp{
			regexp.MustCompile(`(?i)replaced\s+(by|with)\s+(@?[A-Za-z0-9][A-Za-z0-9._@/\-]*)`),
			regexp.MustCompile(`(?i)superseded\s+by\s+(@?[A-Za-z0-9][A-Za-z0-9._@/\-]*)`),
			regexp.MustCompile(`(?i)moved\s+to\s+(@?[A-Za-z0-9][A-Za-z0-9._@/\-]*)`),
			regexp.MustCompile(`(?i)use\s+(@?[A-Za-z0-9][A-Za-z0-9._@/\-]*)\s+instead`),
			regexp.MustCompile(`(?i)successor\s+is\s+(@?[A-Za-z0-9][A-Za-z0-9._@/\-]*)`),
			regexp.MustCompile(`(?i)consolidated\s+into\s+(@?[A-Za-z0-9][A-Za-z0-9._@/\-]*)`),
			regexp.MustCompile(`(?i)integrated\s+into\s+(@?[A-Za-z0-9][A-Za-z0-9._@/\-]*)`),
		}
	default:
		return DetectionResult{}
	}
	cfg := detectorConfig{
		strongPhrases:       strongPhrasesFromCatalog(),
		negativeSubs:        negatives,
		contextualPatterns:  contextualEOLPatterns,
		explicitPattern:     explicitRx,
		componentPattern:    rxPyPIComponent,
		requireProjectToken: true,
		successorPatterns:   successorPats,
		maxBytes:            maxBytes,
		ambiguousStrong:     []string{"no longer supported", "no longer maintained", "no further development", "no further updates", "will not be updated"},
		projectTokens:       projectTokens,
		sentenceNegatives:   []string{"actively maintained", "actively developed", "under active development", "still active"},
	}
	// For PyPI we optionally restrict strong/contextual scanning to the pre-changelog
	// section to avoid false positives from historical release notes mentioning
	// phrases like "no longer maintained" for other components or old branches.
	// Policy
	//   * Explicit phrases (rxPyPIExplicit) are allowed anywhere in the full text.
	//   * Strong/contextual phrases are only considered before the first changelog/history heading.
	var res DetectionResult
	if opts.Source == SourcePyPI {
		fullText := text
		// First attempt explicit-only detection across the full text.
		if r := detectExplicitOnly(fullText, cfg); r.Matched {
			res = r
		} else {
			prelude := trimAtChangelog(fullText)
			res = detect(prelude, cfg)
		}
	} else {
		res = detect(text, cfg)
	}
	if !res.Matched {
		return res
	}
	// Sentence extraction helper
	findSentence := func(full, phrase string) string {
		lf := strings.ToLower(full)
		lp := strings.ToLower(phrase)
		pi := strings.Index(lf, lp)
		if pi < 0 {
			return ""
		}
		start := pi
		for start > 0 {
			c := lf[start-1]
			if c == '.' || c == '!' || c == '?' || c == '\n' {
				break
			}
			start--
		}
		end := pi + len(lp)
		for end < len(lf) {
			c := lf[end]
			if c == '.' || c == '!' || c == '?' || c == '\n' {
				break
			}
			end++
		}
		return strings.ToLower(strings.TrimSpace(lf[start:end]))
	}
	sentence := findSentence(text, res.Phrase)
	// Sentence-level negative suppression (applies to all detection kinds including explicit).
	// Unlike negativeSubs which abort all detection, this only suppresses when the negative
	// phrase appears in the same sentence as the matched phrase.
	for _, neg := range cfg.sentenceNegatives {
		if neg != "" && strings.Contains(sentence, neg) {
			return DetectionResult{}
		}
	}
	// Ambiguous explicit requires project token
	if res.Kind == KindExplicit && containsCI(cfg.ambiguousStrong, strings.ToLower(res.Phrase)) {
		hasToken := false
		for _, tk := range cfg.projectTokens {
			if tk != "" && strings.Contains(sentence, tk) {
				hasToken = true
				break
			}
		}
		if !hasToken || sentenceLooksLikePlatformVersionNotice(sentence, cfg.projectTokens) {
			return DetectionResult{}
		}
	}
	// Platform version suppression for ambiguous strong/explicit
	if (res.Kind == KindExplicit || res.Kind == KindStrong) && containsCI(cfg.ambiguousStrong, strings.ToLower(res.Phrase)) {
		if sentenceLooksLikePlatformVersionNotice(sentence, cfg.projectTokens) {
			return DetectionResult{}
		}
	}
	// Successor proximity
	const maxChars = 400
	const maxNewlines = 3
	res.Successor = extractSuccessorNearPhrase(text, res.Phrase, cfg.successorPatterns, maxChars, maxNewlines)
	// Self reference suppression
	selfName := ""
	if opts.Source == SourcePyPI {
		selfName = opts.PackageName
	} else if opts.Source == SourceReadme {
		selfName = opts.RepoName
	}
	if res.Successor != "" && selfName != "" && strings.EqualFold(res.Successor, selfName) {
		res.Successor = ""
	}
	// Post-detection date extraction: if core detection (strong/explicit) matched
	// but no date was captured, try date-anchored contextual patterns against original text.
	if res.Matched && res.Date == "" {
		res.Date = extractEOLDate(text)
	}
	return res
}

// extractEOLDate scans text for date-anchored contextual EOL patterns (indices 4-7)
// and returns the first date found. Used as a post-detection pass to populate Date
// when the primary match came from strong/explicit detection.
func extractEOLDate(text string) string {
	for _, rx := range contextualEOLPatterns[4:] {
		if m := rx.FindStringSubmatch(text); m != nil {
			if d := extractDateFromSubmatch(m[1:]); d != "" {
				return d
			}
		}
	}
	return ""
}

// detect core engine.
func detect(text string, cfg detectorConfig) DetectionResult {
	if strings.TrimSpace(text) == "" {
		return DetectionResult{}
	}
	if cfg.maxBytes <= 0 {
		cfg.maxBytes = 200_000
	}
	if len(text) > cfg.maxBytes {
		text = text[:cfg.maxBytes]
	}
	lower := strings.ToLower(text)

	for _, n := range cfg.negativeSubs {
		if n != "" && strings.Contains(lower, n) {
			return DetectionResult{}
		}
	}

	// Component-level first sentence suppression with possible later promotion.
	if componentAtStart.MatchString(text) {
		// Find boundary of first sentence (rudimentary split on '.', '!' '?' or newline)
		boundary := -1
		for i, r := range text {
			if r == '.' || r == '!' || r == '?' || r == '\n' {
				boundary = i
				break
			}
		}
		if boundary >= 0 && boundary+1 < len(text) {
			rest := strings.TrimSpace(text[boundary+1:])
			if rest != "" {
				// Re-run detection on the remainder. This recursion will again honor suppression rules
				// for any chained component-level sentences, and only promote if a later sentence is
				// genuinely project-level.
				res := detect(rest, cfg)
				if res.Matched {
					return res
				}
			}
		}
		// No promotable later sentence -> suppress entirely.
		return DetectionResult{}
	}

	if cfg.explicitPattern != nil && cfg.explicitPattern.MatchString(text) { // explicit first
		if cfg.componentPattern != nil && cfg.componentPattern.MatchString(text) && cfg.requireProjectToken {
			// If no project token, skip (fallback to strong/contextual). If project token present but
			// the sentence starts with a component-level deprecation, treat as non-match (KindNone).
			if !strings.Contains(lower, "this project") && !strings.Contains(lower, "this package") {
				// Skip explicit classification but allow fallback evaluation.
			} else if componentAtStart.MatchString(text) {
				// Suppress escalation: return empty detection so strong/contextual may still trigger if applicable.
			} else {
				phrase := cfg.explicitPattern.FindString(text)
				return DetectionResult{Matched: true, Kind: KindExplicit, Phrase: phrase, Successor: extractSuccessor(text, cfg.successorPatterns)}
			}
		} else {
			phrase := cfg.explicitPattern.FindString(text)
			return DetectionResult{Matched: true, Kind: KindExplicit, Phrase: phrase, Successor: extractSuccessor(text, cfg.successorPatterns)}
		}
	}

	// Sentence-level evaluation for strong/contextual to reduce accidental cross-sentence coalescing.
	sentences := splitSentences(text)
	for _, sent := range sentences {
		ls := strings.ToLower(sent)
		// Sentence-level negative: if the sentence contains a negative phrase, skip it entirely.
		// Unlike negativeSubs (which abort all detection), this only skips the current sentence.
		sentenceSkip := false
		for _, neg := range cfg.sentenceNegatives {
			if neg != "" && strings.Contains(ls, neg) {
				sentenceSkip = true
				break
			}
		}
		if sentenceSkip {
			continue
		}
		for _, p := range cfg.strongPhrases {
			if p != "" && strings.Contains(ls, p) {
				// If phrase is ambiguous, require at least one project token present in the same sentence.
				if containsCI(cfg.ambiguousStrong, p) {
					hasProjectToken := false
					for _, tk := range cfg.projectTokens {
						if tk != "" && strings.Contains(ls, tk) {
							hasProjectToken = true
							break
						}
					}
					if !hasProjectToken {
						continue // skip ambiguous match without context
					}
				}
				// Successor may be declared in another sentence; keep legacy behavior by scanning whole text (later proximity filter may refine).
				succ := extractSuccessor(text, cfg.successorPatterns)
				return DetectionResult{Matched: true, Kind: KindStrong, Phrase: p, Successor: succ}
			}
		}
		for _, rx := range cfg.contextualPatterns {
			if rx == nil {
				continue
			}
			if rx.FindStringIndex(ls) == nil {
				continue
			}
			// Extract date from original-case sentence to preserve casing.
			var date string
			if m := rx.FindStringSubmatch(sent); m != nil {
				date = extractDateFromSubmatch(m[1:])
			}
			return DetectionResult{Matched: true, Kind: KindContextual, Phrase: "end of life", Date: date}
		}
	}
	return DetectionResult{}
}

// splitSentences performs a lightweight sentence split on ., !, ?, or newline.
// It preserves order and discards empty segments. This is intentionally simple
// to avoid heavy NLP dependencies while giving us isolation for phrase
// detection, reducing false positives that span multiple sentences.
func splitSentences(text string) []string {
	var out []string
	start := 0
	runes := []rune(text)
	flush := func(i int) {
		if i <= start {
			return
		}
		seg := strings.TrimSpace(string(runes[start:i]))
		if seg != "" {
			out = append(out, seg)
		}
		start = i
	}
	for i, r := range runes {
		if r == '.' || r == '!' || r == '?' || r == '\n' {
			flush(i + 1)
		}
	}
	// tail
	if start < len(runes) {
		seg := strings.TrimSpace(string(runes[start:]))
		if seg != "" {
			out = append(out, seg)
		}
	}
	if len(out) == 0 { // fallback single segment
		trim := strings.TrimSpace(text)
		if trim != "" {
			return []string{trim}
		}
	}
	return out
}

func extractSuccessor(text string, pats []*regexp.Regexp) string {
	for _, rx := range pats {
		if rx == nil {
			continue
		}
		if m := rx.FindStringSubmatch(text); len(m) >= 2 {
			// Use the last capture group (some patterns have intermediate groups like "(by|with)").
			name := strings.Trim(m[len(m)-1], ".,;:()[]{} ")
			if name != "" {
				return name
			}
		}
	}
	return ""
}

// extractSuccessorNearPhrase restricts successor extraction to a window following the first
// occurrence of the matched phrase. This reduces false positives where an unrelated
// "use X instead" appears far away (e.g. API usage guidance) from a generic phrase like
// "no longer supported" that was interpreted as EOL. The search window ends at the earlier
// of (phraseEnd+maxChars) or the point where newlineCount exceeds maxNewlines.
// If phrase cannot be located case-insensitively, the function returns empty string.
func extractSuccessorNearPhrase(fullText, phrase string, pats []*regexp.Regexp, maxChars, maxNewlines int) string {
	if phrase == "" {
		return ""
	}
	lowerFull := strings.ToLower(fullText)
	lowerPhrase := strings.ToLower(phrase)
	idx := strings.Index(lowerFull, lowerPhrase)
	if idx < 0 {
		return ""
	}
	// Include the phrase itself in the window so patterns like "superseded by X" that begin within the phrase substring can match.
	remain := fullText[idx:]
	if maxChars > 0 && len(remain) > maxChars {
		remain = remain[:maxChars]
	}
	if maxNewlines >= 0 { // -1 disables newline limit
		nlCount := 0
		for i, r := range remain {
			if r == '\n' {
				nlCount++
				if nlCount > maxNewlines { // cut before this newline
					remain = remain[:i]
					break
				}
			}
		}
	}
	return extractSuccessor(remain, pats)
}

// =============================
// PyPI changelog trimming helpers
// =============================

// rxChangelogHeading matches a heading signalling the start of historical
// release notes / changelog content. We treat everything from the first match
// onward as the "changelog" section which is excluded from strong/contextual
// scanning to reduce false positives. Explicit phrases are still allowed there.
var rxChangelogHeading = regexp.MustCompile(`(?im)^\s{0,3}(?:#{1,6}\s*)?(?:change\s*log|changelog|release\s+notes|history|version\s+history)\s*:?\s*$`)

// trimAtChangelog returns the text up to (but excluding) the first changelog
// heading. If no heading is found or the heading appears very late (heuristic
// minimum 200 characters of prelude), the original text is returned.
func trimAtChangelog(text string) string {
	if text == "" {
		return text
	}
	loc := rxChangelogHeading.FindStringIndex(text)
	if loc == nil {
		return text
	}
	// Require a reasonable prelude length so that a top-of-file "Changelog" heading
	// (rare for PyPI descriptions) does not remove all content.
	if loc[0] < 200 { // keep original if heading too early
		return text
	}
	return strings.TrimSpace(text[:loc[0]])
}

// detectExplicitOnly performs a minimal detection pass evaluating only explicit
// phrases (and negatives) across the full text. It mirrors the explicit branch
// from detect() so that we can later run the common post-processing pipeline.
func detectExplicitOnly(text string, cfg detectorConfig) DetectionResult {
	if strings.TrimSpace(text) == "" {
		return DetectionResult{}
	}
	lower := strings.ToLower(text)
	for _, n := range cfg.negativeSubs {
		if n != "" && strings.Contains(lower, n) {
			return DetectionResult{}
		}
	}
	if cfg.explicitPattern != nil && cfg.explicitPattern.MatchString(text) {
		// Component-level suppression: ensure not just an API element unless project token present.
		if cfg.componentPattern != nil && cfg.componentPattern.MatchString(text) && cfg.requireProjectToken {
			if !strings.Contains(lower, "this project") && !strings.Contains(lower, "this package") {
				return DetectionResult{}
			}
			if componentAtStart.MatchString(text) {
				return DetectionResult{}
			}
		}
		phrase := cfg.explicitPattern.FindString(text)
		return DetectionResult{Matched: true, Kind: KindExplicit, Phrase: phrase, Successor: extractSuccessor(text, cfg.successorPatterns)}
	}
	return DetectionResult{}
}
