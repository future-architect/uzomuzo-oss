// Package depparser provides infrastructure-layer utilities for dependency parser selection.
//
// DDD Layer: Infrastructure (file I/O and format detection)
package depparser

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depparser/cyclonedx"
)

// goModSniffPrefixLen is the number of bytes to read for content-based go.mod detection.
// A go.mod file typically starts with a "module" directive within the first few hundred bytes,
// preceded only by comments and blank lines.
const goModSniffPrefixLen = 512

// DetectFileParser inspects filePath and returns the matching parser and file data.
// Returns (nil, nil, nil) when the file is not a recognized structured format.
//
// Detection order:
//  1. go.mod — by exact filename, OR by content sniffing ("module " directive in first 512 bytes)
//  2. .json with CycloneDX bomFormat header → parsers["sbom"]
//  3. Unrecognized → (nil, nil, nil)
func DetectFileParser(filePath string, parsers map[string]depparser.DependencyParser) (depparser.DependencyParser, []byte, error) {
	// Fast path: exact filename match.
	if filepath.Base(filePath) == "go.mod" {
		return readGoMod(filePath, parsers)
	}

	// Content-based go.mod detection: read a small prefix and look for "module " directive.
	// This catches renamed files (e.g., "vuls-go.mod", "my-project-go.mod").
	if looksLikeGoMod(filePath) {
		return readGoMod(filePath, parsers)
	}

	if strings.HasSuffix(filePath, ".json") {
		// Read only a small prefix to sniff format, avoiding a full-file
		// allocation when the file is not CycloneDX.
		f, err := os.Open(filePath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to open file '%s': %w", filePath, err)
		}
		defer f.Close() //nolint:errcheck // best-effort cleanup

		prefix := make([]byte, cyclonedx.SniffPrefixLen)
		n, err := io.ReadFull(f, prefix)
		if err != nil && n == 0 {
			return nil, nil, fmt.Errorf("failed to read file '%s': %w", filePath, err)
		}
		prefix = prefix[:n]

		if cyclonedx.IsCycloneDXJSON(prefix) {
			// Confirmed CycloneDX — now read the full file for parsing.
			data, err := os.ReadFile(filePath)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to read file '%s': %w", filePath, err)
			}
			parser, ok := parsers["sbom"]
			if !ok {
				return nil, nil, fmt.Errorf("SBOM parser not available")
			}
			return parser, data, nil
		}
	}

	return nil, nil, nil
}

// readGoMod reads the file and returns the gomod parser.
func readGoMod(filePath string, parsers map[string]depparser.DependencyParser) (depparser.DependencyParser, []byte, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read file '%s': %w", filePath, err)
	}
	parser, ok := parsers["gomod"]
	if !ok {
		return nil, nil, fmt.Errorf("go.mod parser not available")
	}
	return parser, data, nil
}

// looksLikeGoMod reads a small prefix of the file and checks for a go.mod "module" directive.
// Returns false on any I/O error (caller will fall through to other detectors).
func looksLikeGoMod(filePath string) bool {
	f, err := os.Open(filePath)
	if err != nil {
		return false
	}
	defer f.Close() //nolint:errcheck // best-effort cleanup

	buf := make([]byte, goModSniffPrefixLen)
	n, err := io.ReadFull(f, buf)
	if err != nil && n == 0 {
		return false
	}
	buf = buf[:n]

	// A valid go.mod starts with optional comments/whitespace then "module <path>".
	// Check for "module " at the start of any line.
	for _, line := range bytes.Split(buf, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] == '/' {
			continue // skip empty lines and comments
		}
		return bytes.HasPrefix(line, []byte("module "))
	}
	return false
}
