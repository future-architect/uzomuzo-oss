package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/application"
	scanapp "github.com/future-architect/uzomuzo-oss/internal/application/scan"
	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	"github.com/future-architect/uzomuzo-oss/internal/domain/audit"
	"github.com/future-architect/uzomuzo-oss/internal/domain/depparser"
	"github.com/future-architect/uzomuzo-oss/internal/domain/scan"
)

type sarifInputSource struct{ inputs []string }

func (s *sarifInputSource) AnalyzeFromPURLs(_ context.Context, keys []string) (map[string]*analysis.Analysis, error) {
	s.inputs = append(s.inputs, keys...)
	result := make(map[string]*analysis.Analysis, len(keys))
	for _, key := range keys {
		result[key] = &analysis.Analysis{OriginalPURL: key, Error: errors.New("fixture lookup failure")}
	}
	return result, nil
}
func (s *sarifInputSource) AnalyzeFromGitHubURLs(ctx context.Context, keys []string) (map[string]*analysis.Analysis, error) {
	return s.AnalyzeFromPURLs(ctx, keys)
}

type sarifInputParser struct{ name string }

func (p sarifInputParser) FormatName() string { return p.name }
func (p sarifInputParser) Parse(context.Context, []byte) ([]depparser.ParsedDependency, error) {
	return []depparser.ParsedDependency{{PURL: "pkg:npm/a@1", Locations: []depparser.SourceLocation{{Line: 7}}}}, nil
}

func captureSARIF(t *testing.T, run func() error) (sarifLog, error) {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = file
	t.Cleanup(func() { os.Stdout = previous; _ = file.Close() })
	runErr := run()
	os.Stdout = previous
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	var log sarifLog
	if err := json.Unmarshal(data, &log); err != nil {
		t.Fatalf("invalid SARIF: %v, output=%s", err, data)
	}
	return log, runErr
}

func TestSARIFInputRouting(t *testing.T) {
	source := &sarifInputSource{}
	svc, err := scanapp.NewService(application.NewAnalysisService(source))
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "input")
	if err := os.WriteFile(file, []byte("# comment\npkg:npm/a@1\n\npkg:npm/a@1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	opts := ScanOptions{Format: FormatSARIF, ProcessingOptions: ProcessingOptions{Filename: file}, renderOptions: scanRenderOptions{ToolVersion: "test-version"}}
	policy := scan.FailPolicy{}
	tests := []struct {
		name      string
		run       func() error
		wantURI   string
		wantLines []int
	}{
		{"list", func() error {
			return runScanFile(context.Background(), svc, opts, nil, policy, func(string, map[string]depparser.DependencyParser) (depparser.DependencyParser, []byte, error) {
				return nil, nil, nil
			}, nil, nil, scanapp.ActionsConfig{})
		}, file, []int{2, 4}},
		{"go.mod", func() error {
			return runScanFile(context.Background(), svc, opts, nil, policy, func(string, map[string]depparser.DependencyParser) (depparser.DependencyParser, []byte, error) {
				return sarifInputParser{"go.mod"}, nil, nil
			}, nil, nil, scanapp.ActionsConfig{})
		}, file, []int{7}},
		{"SBOM via file", func() error {
			return runScanFile(context.Background(), svc, opts, nil, policy, func(string, map[string]depparser.DependencyParser) (depparser.DependencyParser, []byte, error) {
				return sarifInputParser{"CycloneDX SBOM"}, nil, nil
			}, nil, nil, scanapp.ActionsConfig{})
		}, file, []int{0}},
		{"SBOM", func() error {
			opts.SBOMPath = file
			return runScanSBOM(context.Background(), svc, opts, map[string]depparser.DependencyParser{"sbom": sarifInputParser{"CycloneDX SBOM"}}, policy)
		}, file, []int{0}},
		{"direct", func() error {
			return runScanDirect(context.Background(), svc, []string{"pkg:npm/a@1"}, opts, policy, scanapp.ActionsConfig{})
		}, "", nil},
		{"workflow", func() error {
			return runScanFile(context.Background(), svc, opts, nil, policy, func(string, map[string]depparser.DependencyParser) (depparser.DependencyParser, []byte, error) {
				return nil, nil, nil
			}, func(string) ([]byte, bool, error) { return nil, true, nil }, func([]byte) ([]string, map[string][]depparser.SourceLocation, error) {
				return []string{"https://github.com/a/b"}, map[string][]depparser.SourceLocation{"https://github.com/a/b": {{Line: 3}, {Line: 9}}}, nil
			}, scanapp.ActionsConfig{})
		}, file, []int{3, 9}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source.inputs = nil
			log, err := captureSARIF(t, tt.run)
			if err != nil {
				t.Fatal(err)
			}
			if len(source.inputs) != 1 {
				t.Fatalf("analysis inputs=%v", source.inputs)
			}
			if len(log.Runs) != 1 || len(log.Runs[0].Results) != 1 {
				t.Fatalf("runs=%+v", log.Runs)
			}
			if log.Runs[0].Tool.Driver.Version != "test-version" {
				t.Fatal("version lost")
			}
			var lines []int
			for _, loc := range log.Runs[0].Results[0].Locations {
				if loc.PhysicalLocation.ArtifactLocation.URI != filepath.ToSlash(tt.wantURI) {
					t.Fatalf("artifact=%+v", loc)
				}
				line := 0
				if loc.PhysicalLocation.Region != nil {
					line = loc.PhysicalLocation.Region.StartLine
				}
				lines = append(lines, line)
			}
			if !reflect.DeepEqual(lines, tt.wantLines) {
				t.Fatalf("lines=%v, want %v", lines, tt.wantLines)
			}
		})
	}
}

func TestSARIFWrittenBeforePolicyFailure(t *testing.T) {
	svc, err := scanapp.NewService(application.NewAnalysisService(nil))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := scan.ParseFailPolicy("legacy-safe")
	if err != nil {
		t.Fatal(err)
	}
	entries := []audit.AuditEntry{sarifTestEntry("pkg:npm/a@1", analysis.LabelLegacySafe, audit.VerdictOK)}
	result := &scanapp.Result{Entries: entries, HasFailure: policy.Evaluate(entries)}
	for _, filter := range []string{"", "replace", "ok"} {
		t.Run("filter="+filter, func(t *testing.T) {
			log, err := captureSARIF(t, func() error {
				return finalizeScanOutput(svc, result, ScanOptions{Format: FormatSARIF, ShowOnlyRaw: filter, renderOptions: scanRenderOptions{Policy: policy}}, 1)
			})
			if !errors.Is(err, ErrScanFailPolicy) {
				t.Fatalf("error=%v", err)
			}
			want := 1
			if filter == "replace" {
				want = 0
			}
			if len(log.Runs[0].Results) != want {
				t.Fatalf("results=%+v", log.Runs[0].Results)
			}
			if want == 1 && log.Runs[0].Results[0].Level != "error" {
				t.Fatal("policy match must be error")
			}
		})
	}
}

func TestSARIFStdinAndAutoDetectLocations(t *testing.T) {
	svc, err := scanapp.NewService(application.NewAnalysisService(&sarifInputSource{}))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	input := filepath.Join(dir, "go.mod")
	if err := os.WriteFile(input, []byte("pkg:npm/a@1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	for _, mode := range []string{"stdin", "sbom-stdin", "auto"} {
		t.Run(mode, func(t *testing.T) {
			file, err := os.Open(input)
			if err != nil {
				t.Fatal(err)
			}
			previous := os.Stdin
			os.Stdin = file
			t.Cleanup(func() { os.Stdin = previous; _ = file.Close() })
			opts := ScanOptions{Format: FormatSARIF, SBOMPath: "-"}
			policy := scan.FailPolicy{}
			log, err := captureSARIF(t, func() error {
				switch mode {
				case "stdin":
					return runScanStdin(context.Background(), svc, opts, policy, scanapp.ActionsConfig{})
				case "sbom-stdin":
					return runScanSBOM(context.Background(), svc, opts, map[string]depparser.DependencyParser{"sbom": sarifInputParser{"CycloneDX SBOM"}}, policy)
				default:
					return runScanAutoDetect(context.Background(), svc, opts, map[string]depparser.DependencyParser{"gomod": sarifInputParser{"go.mod"}}, policy)
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(log.Runs[0].Results) != 1 {
				t.Fatalf("results=%+v", log.Runs[0].Results)
			}
			locs := log.Runs[0].Results[0].Locations
			if mode == "auto" {
				if len(locs) != 1 || locs[0].PhysicalLocation.Region == nil {
					t.Fatalf("locations=%+v", locs)
				}
				if locs[0].PhysicalLocation.ArtifactLocation.URI != "go.mod" || locs[0].PhysicalLocation.Region.StartLine != 7 {
					t.Fatalf("location=%+v", locs[0])
				}
			} else if len(locs) != 0 {
				t.Fatalf("stdin locations=%+v", locs)
			}
		})
	}
}
