// Package depparser provides infrastructure-layer utilities for dependency parser selection.
//
// DDD Layer: Infrastructure (file I/O and format detection)
package depparser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depparser/cyclonedx"
)

// DetectFileParser inspects filePath and returns the matching parser and file data.
// Returns (nil, nil, nil) when the file is not a recognized structured format.
//
// Detection order:
//  1. go.mod (by filename) → parsers["gomod"]
//  2. .json with CycloneDX bomFormat header → parsers["sbom"]
//  3. Unrecognized → (nil, nil, nil)
func DetectFileParser(filePath string, parsers map[string]depparser.DependencyParser) (depparser.DependencyParser, []byte, error) {
	if filepath.Base(filePath) == "go.mod" {
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

	if strings.HasSuffix(filePath, ".json") {
		// Read only a small prefix to sniff format, avoiding a full-file
		// allocation when the file is not CycloneDX.
		f, err := os.Open(filePath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to open file '%s': %w", filePath, err)
		}
		defer f.Close() //nolint:errcheck // best-effort cleanup

		prefix := make([]byte, cyclonedx.SniffPrefixLen)
		n, err := f.Read(prefix)
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
