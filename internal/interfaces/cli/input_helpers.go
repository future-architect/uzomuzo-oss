package cli

import (
	"bufio"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"strings"

	"github.com/future-architect/uzomuzo-oss/internal/common"
)

// maxDescriptionLen limits the length of repository/project descriptions in CLI output.
const maxDescriptionLen = 150

// truncateDescription normalizes to a single line and truncates with an ellipsis when too long.
func truncateDescription(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	// single line normalization
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	// Truncate by rune, not byte, so multi-byte UTF-8 characters are not split.
	if r := []rune(s); len(r) > maxDescriptionLen {
		return string(r[:maxDescriptionLen-1]) + "…"
	}
	return s
}

// randomSample randomly selects a subset of strings (works for PURLs, GitHub URLs, etc.)
func randomSample(items []string, sampleSize int) []string {
	if sampleSize <= 0 || sampleSize >= len(items) {
		return items // return all if sample size is invalid or >= total
	}

	// Create a copy to avoid modifying the original slice
	itemsCopy := make([]string, len(items))
	copy(itemsCopy, items)

	// Shuffle using Go 1.20+ auto-seeded random generation
	rand.Shuffle(len(itemsCopy), func(i, j int) {
		itemsCopy[i], itemsCopy[j] = itemsCopy[j], itemsCopy[i]
	})

	return itemsCopy[:sampleSize]
}

// validateLineRange validates line range options and returns an error if invalid.
func validateLineRange(opts *ProcessingOptions) error {
	if opts.LineStart < 0 || opts.LineEnd < 0 {
		return fmt.Errorf("--line-range values must be non-negative (0 disables range filtering)")
	}
	if opts.LineStart > 0 && opts.LineEnd > 0 && opts.LineEnd < opts.LineStart {
		return fmt.Errorf("--line-range end must be >= start (start=%d, end=%d)", opts.LineStart, opts.LineEnd)
	}
	return nil
}

// categorizeInputs separates PURLs and GitHub URLs from mixed input.
func categorizeInputs(inputs []string) (purls []string, githubURLs []string) {
	for _, input := range inputs {
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		if strings.HasPrefix(input, "pkg:") {
			purls = append(purls, input)
		} else if common.IsValidGitHubURL(input) {
			githubURLs = append(githubURLs, input)
		} else {
			slog.Warn("Unsupported input format",
				"input", input,
				"suggestion", "Expected PURL (pkg:) or GitHub URL format")
		}
	}
	return purls, githubURLs
}

// unrecognizedLineThreshold is the fraction of non-blank, non-comment lines that must be
// unrecognized (neither PURL nor GitHub URL) before categorizeFileLines returns an error.
// This catches cases where a structured file (e.g., go.mod) bypasses format detection and
// is silently misinterpreted as a PURL list.
const unrecognizedLineThreshold = 0.5

// categorizeFileLines reads file and categorizes each line (unified function).
func categorizeFileLines(filename string, opts ProcessingOptions) (purls []string, githubURLs []string, err error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open file '%s': %w", filename, err)
	}
	defer func() {
		if cerr := file.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	scanner := bufio.NewScanner(file)
	lineNum := 0
	unrecognized := 0
	contentLines := 0

	for scanner.Scan() {
		lineNum++
		// Apply pre-filter: skip until start
		if opts.LineStart > 0 && lineNum < opts.LineStart {
			continue
		}
		if opts.LineEnd > 0 && lineNum > opts.LineEnd {
			break // early stop once beyond end
		}

		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and comments (still count in line numbers above)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		contentLines++
		if strings.HasPrefix(line, "pkg:") {
			purls = append(purls, line)
		} else if common.IsValidGitHubURL(line) {
			githubURLs = append(githubURLs, line)
		} else {
			unrecognized++
			slog.Warn("Unsupported line format",
				"file", filename,
				"line", lineNum,
				"content", line,
				"suggestion", "Expected PURL (pkg:) or GitHub URL format")
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("error reading file '%s': %w", filename, err)
	}

	// Failsafe: if the majority of content lines are unrecognized, the file is likely
	// a structured format (go.mod, requirements.txt, etc.) that was not detected.
	if contentLines > 0 && float64(unrecognized)/float64(contentLines) > unrecognizedLineThreshold {
		return nil, nil, fmt.Errorf(
			"file '%s' does not appear to be a PURL/URL list: %d/%d lines unrecognized. "+
				"If this is a go.mod or other dependency file, ensure the format is detected correctly or convert to CycloneDX SBOM first",
			filename, unrecognized, contentLines)
	}

	return purls, githubURLs, nil
}

// ProcessingOptions govern how scan input processing behaves for both direct
// arguments and file-based input.
type ProcessingOptions struct {
	SampleSize    int
	Filename      string
	IsDirectInput bool
	// LineStart and LineEnd define an optional 1-based inclusive line range filter for file mode.
	// If both are zero, no range filtering is applied. If LineEnd is zero, it means EOF.
	LineStart int
	LineEnd   int
}
