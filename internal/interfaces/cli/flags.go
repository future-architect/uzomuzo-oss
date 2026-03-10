package cli

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
)

// FlagType represents a supported primitive flag kind.
type FlagType int

const (
	FlagBool FlagType = iota
	FlagString
	FlagInt
)

// FlagSpec declares a CLI flag binding into ProcessingOptions.
// Adding a new flag should only require appending one entry to specs in buildFlagSet().
type FlagSpec struct {
	Name string
	Type FlagType
	Help string
	// Set performs assignment into *ProcessingOptions after parsing (for custom logic) OR binds variable address.
	// Exactly one of BoolTarget/StringTarget/IntTarget or Set should be used.
	BoolTarget   *bool
	StringTarget *string
	IntTarget    *int
	// Optional alias names (e.g. deprecated or shorthand) - simple binding without extra help duplication.
	Aliases []string
}

func (s FlagSpec) bind(fs *flag.FlagSet) {
	switch s.Type {
	case FlagBool:
		if s.BoolTarget != nil {
			fs.BoolVar(s.BoolTarget, s.Name, false, s.Help)
		}
		for _, a := range s.Aliases {
			if s.BoolTarget != nil {
				fs.BoolVar(s.BoolTarget, a, false, "(alias of --"+s.Name+")")
			}
		}
	case FlagString:
		if s.StringTarget != nil {
			fs.StringVar(s.StringTarget, s.Name, "", s.Help)
		}
		for _, a := range s.Aliases {
			if s.StringTarget != nil {
				fs.StringVar(s.StringTarget, a, "", "(alias of --"+s.Name+")")
			}
		}
	case FlagInt:
		if s.IntTarget != nil {
			fs.IntVar(s.IntTarget, s.Name, 0, s.Help)
		}
		for _, a := range s.Aliases {
			if s.IntTarget != nil {
				fs.IntVar(s.IntTarget, a, 0, "(alias of --"+s.Name+")")
			}
		}
	}
}

// buildFlagSet constructs a FlagSet and wires ProcessingOptions fields.
// Mode specific differences (like ignoring sample in direct mode) are handled by caller.
func buildFlagSet(opts *ProcessingOptions, mode string, cfgSampleDefault int) *flag.FlagSet {
	fs := flag.NewFlagSet(mode, flag.ContinueOnError)
	// Suppress default usage noise (callers print their own errors)
	fs.SetOutput(new(strings.Builder))

	var lineRangeRaw string

	specs := []FlagSpec{
		{Name: "only-review-needed", Type: FlagBool, Help: "Show only 'Review Needed' analysis results in output", BoolTarget: &opts.OnlyReviewNeeded},
		{Name: "only-eol", Type: FlagBool, Help: "Show only 'EOL-*' analysis results (Confirmed/Effective/Planned) in output", BoolTarget: &opts.OnlyEOL},
		{Name: "ecosystem", Type: FlagString, Help: "Filter PURLs to a single ecosystem (e.g., npm, pypi, maven, nuget, cargo, golang, gem, composer)", StringTarget: &opts.Ecosystem},
		{Name: "sample", Type: FlagInt, Help: "Randomly sample up to N inputs (file mode only; direct mode ignores)", IntTarget: &opts.SampleSize},
		{Name: "export-license-csv", Type: FlagString, Help: "Write extended license analysis CSV to specified path (optional)", StringTarget: &opts.LicenseCSVPath},
		// line-range is a string (START:END) parsed after flag parsing to populate opts.LineStart/LineEnd.
		{Name: "line-range", Type: FlagString, Help: "Limit processing to 1-based inclusive line range START:END (file mode only; END optional, e.g. 100:200 or 250:)", StringTarget: &lineRangeRaw},
	}
	for _, s := range specs {
		s.bind(fs)
	}
	if mode == "file" && opts.SampleSize == 0 { // default from config for file mode
		opts.SampleSize = cfgSampleDefault
	}

	// Store a post-parse hook in unused fields (using closure) by leveraging FlagSet's error output suppression.
	// Callers must invoke applyPostParseLineRange(fs, &opts, lineRangeRaw) after fs.Parse.
	if lineRangeRaw != "" {
		// Placeholder; actual parsing is triggered by caller after Parse
	}
	// Attach the raw string into an internal field via closure side effect (not exported) -- simpler: use fs.Usage side note.
	// We'll rely on a helper.
	lineRangeRawGlobal[fs] = &lineRangeRaw
	return fs
}

// lineRangeRawGlobal maps a FlagSet pointer identity to a captured line-range raw pointer.
var lineRangeRawGlobal = make(map[*flag.FlagSet]*string)

// applyPostParseLineRange parses the stored line-range raw value and populates opts.
func applyPostParseLineRange(fs *flag.FlagSet, opts *ProcessingOptions) error {
	ptr, ok := lineRangeRawGlobal[fs]
	if !ok || ptr == nil || *ptr == "" {
		return nil
	}
	ls, le, err := parseLineRange(*ptr)
	if err != nil {
		return err
	}
	opts.LineStart = ls
	opts.LineEnd = le
	return nil
}

// parseLineRange parses START:END or START: format. START must be >=1. END optional or >= START.
func parseLineRange(raw string) (int, int, error) {
	if !strings.Contains(raw, ":") { // enforce colon presence
		return 0, 0, fmt.Errorf("invalid --line-range format (expected START:END)")
	}
	parts := strings.Split(raw, ":")
	if len(parts) == 0 || len(parts) > 2 {
		return 0, 0, fmt.Errorf("invalid --line-range format (expected START:END)")
	}
	startStr := strings.TrimSpace(parts[0])
	if startStr == "" {
		return 0, 0, fmt.Errorf("invalid --line-range: start missing")
	}
	start, err := strconv.Atoi(startStr)
	if err != nil || start < 1 {
		return 0, 0, fmt.Errorf("invalid --line-range: start must be integer >=1")
	}
	end := 0
	if len(parts) == 2 {
		endStr := strings.TrimSpace(parts[1])
		if endStr != "" {
			v, err := strconv.Atoi(endStr)
			if err != nil || v < start {
				return 0, 0, fmt.Errorf("invalid --line-range: end must be integer >= start")
			}
			end = v
		}
	}
	return start, end, nil
}

// parseFlags splits raw args into flag tokens & positional arguments.
func parseFlags(args []string) (flagTokens []string, positional []string) {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flagTokens = append(flagTokens, a)
		} else {
			positional = append(positional, a)
		}
	}
	return
}
