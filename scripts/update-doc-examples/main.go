// update-doc-examples builds the uzomuzo binary and runs predefined CLI
// commands, then replaces the corresponding output blocks in Markdown
// documentation files. Blocks are identified by HTML comment markers:
//
//	<!-- begin:output:BLOCK_ID -->
//	```lang
//	...
//	```
//	<!-- end:output:BLOCK_ID -->
//
// Usage:
//
//	go run ./scripts/update-doc-examples [flags]
//
// Flags:
//
//	--dry-run          Report changed blocks, exit 1 if any differ
//	--check-markers    Validate marker presence and structure only (no binary needed; for CI)
//	--skip-build       Use existing binary instead of rebuilding
//	--skip-juice-shop  Skip commands that require trivy
package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
)

//go:embed commands.json
var commandsJSON []byte

// Config is the top-level structure of commands.json.
type Config struct {
	Binary   string    `json:"binary"`
	Commands []Command `json:"commands"`
	RawFiles []RawFile `json:"raw_files"`
}

// Command defines a CLI invocation whose output replaces a marker block.
type Command struct {
	ID             string   `json:"id"`
	Args           []string `json:"args"`
	Files          []string `json:"files"`
	FenceLang      string   `json:"fence_lang"`
	Prepend        string   `json:"prepend"`
	Append         string   `json:"append"`
	IgnoreExitCode bool     `json:"ignore_exit_code"`
}

// RawFile defines a shell command whose stdout is written to a file directly.
// The Shell field is safe because commands.json is embedded at compile time.
type RawFile struct {
	ID             string   `json:"id"`
	Shell          string   `json:"shell"`
	OutputFile     string   `json:"output_file"`
	Requires       []string `json:"requires"`
	IgnoreExitCode bool     `json:"ignore_exit_code"`
}

func main() {
	dryRun := flag.Bool("dry-run", false, "report changed blocks and exit 1 if any differ")
	checkMarkers := flag.Bool("check-markers", false, "validate marker presence and structure only (no binary needed)")
	skipBuild := flag.Bool("skip-build", false, "use existing binary")
	skipJuiceShop := flag.Bool("skip-juice-shop", false, "skip commands requiring trivy")
	flag.Parse()

	var cfg Config
	if err := json.Unmarshal(commandsJSON, &cfg); err != nil {
		fatalf("parse commands.json: %v", err)
	}

	if *checkMarkers {
		runCheckMarkers(cfg)
		return
	}

	if !*skipBuild {
		fmt.Println("Building uzomuzo...")
		if err := runShell("go", "build", "-o", cfg.Binary, "./cmd/uzomuzo"); err != nil {
			fatalf("build failed: %v", err)
		}
	}

	// Read all target files into memory.
	fileContents := make(map[string]string)
	for _, cmd := range cfg.Commands {
		for _, f := range cmd.Files {
			if _, ok := fileContents[f]; !ok {
				data, err := os.ReadFile(f)
				if err != nil {
					fatalf("read %s: %v", f, err)
				}
				fileContents[f] = string(data)
			}
		}
	}

	// Cache command output by args to avoid redundant API calls.
	outputCache := make(map[string]string)

	changed := 0

	// Process each command.
	for _, cmd := range cfg.Commands {
		cacheKey := strings.Join(cmd.Args, "\x00")
		output, cached := outputCache[cacheKey]
		if !cached {
			fmt.Printf("Running: %s %s\n", cfg.Binary, strings.Join(cmd.Args, " "))
			var err error
			output, err = runCommand(cfg.Binary, cmd.Args)
			if err != nil && !cmd.IgnoreExitCode {
				fatalf("command %q failed: %v\nOutput:\n%s", cmd.ID, err, output)
			}
			outputCache[cacheKey] = output
		} else {
			fmt.Printf("Cached:  %s [%s]\n", cmd.ID, strings.Join(cmd.Args[:min(3, len(cmd.Args))], " "))
		}

		// Build the replacement block content.
		var block strings.Builder
		if cmd.Prepend != "" {
			block.WriteString(cmd.Prepend)
		}
		block.WriteString(strings.TrimRight(output, "\n"))
		if cmd.Append != "" {
			block.WriteString("\n")
			block.WriteString(cmd.Append)
		}

		for _, f := range cmd.Files {
			content := fileContents[f]
			replaced, err := replaceBlock(content, cmd.ID, block.String(), cmd.FenceLang)
			if err != nil {
				fatalf("%s: %v", f, err)
			}
			if replaced != content {
				changed++
				fmt.Printf("  Updated: %s [%s]\n", f, cmd.ID)
			}
			fileContents[f] = replaced
		}
	}

	// Process raw file outputs (requires shell; input is compile-time embedded).
	if !*skipJuiceShop {
		for _, rf := range cfg.RawFiles {
			if !hasRequiredTools(rf.Requires) {
				fmt.Printf("Skipping %s: required tool(s) not found: %v\n", rf.ID, rf.Requires)
				continue
			}
			cleaned := path.Clean(rf.OutputFile)
			if cleaned == "." || strings.HasPrefix(cleaned, "..") || strings.ContainsRune(rf.OutputFile, '\\') {
				fatalf("raw command %q: output_file %q escapes repo root", rf.ID, rf.OutputFile)
			}

			fmt.Printf("Running shell: %s\n", rf.Shell)
			output, err := runShellCommand(rf.Shell)
			if err != nil && !rf.IgnoreExitCode {
				fatalf("raw command %q failed: %v", rf.ID, err)
			}
			existing, err := os.ReadFile(rf.OutputFile)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				fatalf("read %s: %v", rf.OutputFile, err)
			}
			if string(existing) != output {
				changed++
				fmt.Printf("  Updated: %s\n", rf.OutputFile)
				if !*dryRun {
					if err := os.WriteFile(rf.OutputFile, []byte(output), 0644); err != nil {
						fatalf("write %s: %v", rf.OutputFile, err)
					}
				}
			}
		}
	}

	// In dry-run mode, report changes without writing files.
	if *dryRun {
		if changed > 0 {
			fmt.Printf("\nDry run: %d block(s) would be updated. Run without --dry-run to apply.\n", changed)
			os.Exit(1)
		}
		fmt.Println("Dry run: all blocks are up to date.")
		return
	}

	for f, content := range fileContents {
		if err := os.WriteFile(f, []byte(content), 0644); err != nil {
			fatalf("write %s: %v", f, err)
		}
	}
	fmt.Printf("\nDone: %d block(s) updated.\n", changed)
}

// runCheckMarkers validates that every command in commands.json has matching
// begin/end markers in the target files and that marker blocks are non-empty.
// This mode does not require the uzomuzo binary or any API calls.
func runCheckMarkers(cfg Config) {
	fileContents := make(map[string]string)
	errs := 0

	for _, cmd := range cfg.Commands {
		for _, f := range cmd.Files {
			if _, ok := fileContents[f]; !ok {
				data, err := os.ReadFile(f)
				if err != nil {
					fmt.Fprintf(os.Stderr, "ERROR: read %s: %v\n", f, err)
					errs++
					continue
				}
				fileContents[f] = string(data)
			}

			content := fileContents[f]
			beginMarker := "<!-- begin:output:" + cmd.ID + " -->"
			endMarker := "<!-- end:output:" + cmd.ID + " -->"

			beginIdx := strings.Index(content, beginMarker)
			if beginIdx < 0 {
				fmt.Fprintf(os.Stderr, "ERROR: %s: missing begin marker for %q\n", f, cmd.ID)
				errs++
				continue
			}

			afterBegin := beginIdx + len(beginMarker)
			if strings.Contains(content[afterBegin:], beginMarker) {
				fmt.Fprintf(os.Stderr, "ERROR: %s: duplicate begin marker for %q\n", f, cmd.ID)
				errs++
			}

			endIdx := strings.Index(content[afterBegin:], endMarker)
			if endIdx < 0 {
				fmt.Fprintf(os.Stderr, "ERROR: %s: missing end marker for %q\n", f, cmd.ID)
				errs++
				continue
			}

			blockContent := strings.TrimSpace(content[afterBegin : afterBegin+endIdx])
			if blockContent == "" {
				fmt.Fprintf(os.Stderr, "WARN:  %s: empty block for %q\n", f, cmd.ID)
			} else {
				fmt.Printf("OK: %s [%s]\n", f, cmd.ID)
			}
		}
	}

	if errs > 0 {
		fmt.Fprintf(os.Stderr, "\n%d marker error(s) found.\n", errs)
		os.Exit(1)
	}
	fmt.Printf("\nAll %d marker(s) validated successfully.\n", len(cfg.Commands))
}

// replaceBlock replaces the content between begin/end markers for the given
// block ID using index-based string splicing (avoids regex $ expansion and
// detects duplicate markers). Returns the modified string or an error.
func replaceBlock(content, id, newBlock, fenceLang string) (string, error) {
	beginMarker := "<!-- begin:output:" + id + " -->\n"
	endMarker := "<!-- end:output:" + id + " -->"

	beginIdx := strings.Index(content, beginMarker)
	if beginIdx < 0 {
		return "", fmt.Errorf("marker %q not found", beginMarker)
	}

	// Check for duplicate begin markers.
	if strings.Contains(content[beginIdx+len(beginMarker):], beginMarker) {
		return "", fmt.Errorf("duplicate marker %q found", beginMarker)
	}

	afterBegin := beginIdx + len(beginMarker)
	endIdx := strings.Index(content[afterBegin:], endMarker)
	if endIdx < 0 {
		return "", fmt.Errorf("end marker %q not found (begin at offset %d)", endMarker, beginIdx)
	}
	endIdx += afterBegin

	// Check for duplicate end markers.
	if strings.Contains(content[endIdx+len(endMarker):], endMarker) {
		return "", fmt.Errorf("duplicate marker %q found", endMarker)
	}

	fence := fenceDelimiter(newBlock)
	replacement := fmt.Sprintf("%s%s%s\n%s\n%s\n%s",
		beginMarker, fence, fenceLang, newBlock, fence, endMarker)

	var sb strings.Builder
	sb.Grow(len(content))
	sb.WriteString(content[:beginIdx])
	sb.WriteString(replacement)
	sb.WriteString(content[endIdx+len(endMarker):])
	return sb.String(), nil
}

// fenceDelimiter returns a backtick fence string long enough to avoid
// collisions with backtick runs inside content. It uses at least 3 backticks
// and increases the count if the content contains a run of 3+ backticks.
func fenceDelimiter(content string) string {
	maxRun := 0
	cur := 0
	for _, ch := range content {
		if ch == '`' {
			cur++
			if cur > maxRun {
				maxRun = cur
			}
		} else {
			cur = 0
		}
	}
	n := 3
	if maxRun >= 3 {
		n = maxRun + 1
	}
	return strings.Repeat("`", n)
}

// runCommand executes the binary with args and returns stdout.
func runCommand(binary string, args []string) (string, error) {
	cmd := exec.Command(binary, args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	return string(out), err
}

// runShell executes a command directly (no shell interpretation).
func runShell(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runShellCommand executes a command via sh -c and returns stdout.
// The input is trusted (compile-time embedded via go:embed).
func runShellCommand(shell string) (string, error) {
	cmd := exec.Command("sh", "-c", shell)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	return string(out), err
}

// hasRequiredTools checks if all required tools are available in PATH.
func hasRequiredTools(tools []string) bool {
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			return false
		}
	}
	return true
}

// fatalf prints an error message to stderr and exits with code 1.
func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
