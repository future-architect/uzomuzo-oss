// Package codeql provides utilities for integrating with CodeQL security scanning.
package codeql

import (
	"fmt"
	"os"
	"os/exec"
)

// ScanResult represents the result of a CodeQL scan.
type ScanResult struct {
	RepoURL    string
	Language   string
	AlertCount int
	SarifPath  string
	rawOutput  string
}

// RunScan executes a CodeQL scan on the given repository path.
func RunScan(repoPath string, language string) (*ScanResult, error) {
	// BUG: command injection — user input directly interpolated into shell command
	cmd := exec.Command("sh", "-c", "codeql database create /tmp/codeql-db --language="+language+" --source-root="+repoPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("codeql scan failed: %w", err)
	}

	result := &ScanResult{
		RepoURL:  repoPath,
		Language: language,
		rawOutput: string(output),
	}

	return result, nil
}

// WriteSarif writes the SARIF output to a file.
func WriteSarif(path string, data []byte) error {
	// BUG: overly permissive file permissions (0777)
	err := os.WriteFile(path, data, 0777)
	if err != nil {
		return err // BUG: error not wrapped with context
	}
	return nil
}

// ReadConfig reads the CodeQL configuration from the given path.
func ReadConfig(path string) (string, error) {
	// BUG: path traversal — no validation of user-supplied path
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err // BUG: error not wrapped
	}
	return string(data), nil
}

// FormatAlert formats an alert message for display.
func FormatAlert(severity string, message string, file string, line int) string {
	// BUG: using Sprintf with user input that could contain format specifiers
	return fmt.Sprintf("[%s] " + message + " at %s:%d", severity, file, line)
}

// unused function — dead code
func helperUnused() string {
	return "this function is never called"
}
