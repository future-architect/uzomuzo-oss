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
//	--dry-run          Print diffs, exit 1 if any block changed (for CI)
//	--skip-build       Use existing binary instead of rebuilding
//	--skip-juice-shop  Skip commands that require trivy
package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
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
type RawFile struct {
	ID             string   `json:"id"`
	Shell          string   `json:"shell"`
	OutputFile     string   `json:"output_file"`
	Requires       []string `json:"requires"`
	IgnoreExitCode bool     `json:"ignore_exit_code"`
}

func main() {
	dryRun := flag.Bool("dry-run", false, "print diffs and exit 1 if any block changed")
	skipBuild := flag.Bool("skip-build", false, "use existing binary")
	skipJuiceShop := flag.Bool("skip-juice-shop", false, "skip commands requiring trivy")
	flag.Parse()

	var cfg Config
	if err := json.Unmarshal(commandsJSON, &cfg); err != nil {
		fatalf("parse commands.json: %v", err)
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

	changed := 0

	// Process each command.
	for _, cmd := range cfg.Commands {
		fmt.Printf("Running: %s %s\n", cfg.Binary, strings.Join(cmd.Args, " "))
		output, err := runCommand(cfg.Binary, cmd.Args)
		if err != nil && !cmd.IgnoreExitCode {
			fatalf("command %q failed: %v\nOutput:\n%s", cmd.ID, err, output)
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
			replaced, ok := replaceBlock(content, cmd.ID, block.String(), cmd.FenceLang)
			if !ok {
				fatalf("marker <!-- begin:output:%s --> not found in %s", cmd.ID, f)
			}
			if replaced != content {
				changed++
				fmt.Printf("  Updated: %s [%s]\n", f, cmd.ID)
			}
			fileContents[f] = replaced
		}
	}

	// Process raw file outputs.
	if !*skipJuiceShop {
		for _, rf := range cfg.RawFiles {
			if !hasRequiredTools(rf.Requires) {
				fmt.Printf("Skipping %s: required tool(s) not found: %v\n", rf.ID, rf.Requires)
				continue
			}
			fmt.Printf("Running shell: %s\n", rf.Shell)
			output, err := runShellCommand(rf.Shell)
			if err != nil && !rf.IgnoreExitCode {
				fatalf("raw command %q failed: %v", rf.ID, err)
			}
			existing, _ := os.ReadFile(rf.OutputFile)
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

	// Write updated files.
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

// replaceBlock replaces the content between begin/end markers for the given
// block ID. Returns the modified string and whether the marker was found.
func replaceBlock(content, id, newBlock, fenceLang string) (string, bool) {
	pattern := regexp.MustCompile(
		`(?s)(<!-- begin:output:` + regexp.QuoteMeta(id) + ` -->\n).*?(<!-- end:output:` + regexp.QuoteMeta(id) + ` -->)`,
	)
	if !pattern.MatchString(content) {
		return content, false
	}
	replacement := fmt.Sprintf("<!-- begin:output:%s -->\n```%s\n%s\n```\n<!-- end:output:%s -->",
		id, fenceLang, newBlock, id)
	return pattern.ReplaceAllString(content, replacement), true
}

// runCommand executes the binary with args and returns combined stdout.
func runCommand(binary string, args []string) (string, error) {
	cmd := exec.Command(binary, args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	return string(out), err
}

// runShell executes a command directly (no shell).
func runShell(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runShellCommand executes a command via sh -c and returns stdout.
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

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
