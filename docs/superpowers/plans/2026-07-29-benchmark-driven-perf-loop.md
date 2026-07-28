# Benchmark-Driven Performance Loop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give `uzomuzo` a hermetic benchmark baseline over its CPU-hot paths, then run a self-paced `/loop` that profiles, optimizes one hotspot per iteration, and verifies each change with `benchstat` before committing.

**Architecture:** Tasks 1–6 build the measurement infrastructure: a `make bench` target plus one benchmark file per hot package, each with a hermetic fixture and a correctness assertion that prevents the benchmark from silently measuring a no-op early-return. Task 7 captures the baseline and writes the loop's iteration prompt. Task 8 launches the loop. Nothing in Tasks 1–6 changes production code — that is exclusively the loop's job.

**Tech Stack:** Go 1.26.1 (toolchain) / `go 1.25.0` (module floor), stdlib `testing` benchmarks, `golang.org/x/perf/cmd/benchstat`, `go tool pprof`, tree-sitter via CGO.

## Global Constraints

- Go toolchain is **1.26.1**; `go.mod` says `go 1.25.0` and must **not** be downgraded (`CLAUDE.md`).
- All source, comments, test names, and docs are **English only** (`language-policy.md`).
- Benchmarks must be **hermetic** — no network, no live API calls — so `go test ./...` runs in CI and restricted-network environments (`testing-performance.md`).
- Test data lives under each package's `testdata/` (`project-conventions.md`). Corpora too large or too numerous to commit are produced by a committed **deterministic generator** instead; see Task 6's note.
- The `treesitter` package is `//go:build cgo`. Its benchmark file must carry the same tag (`testing-performance.md`, "Propagate Build Tags to Test Files").
- Tree-sitter handles (`Analyzer`, `Parser`, `Tree`, `QueryCursor`) must be explicitly closed; leaking one per iteration bloats memory across a benchmark run (`testing-performance.md`).
- Never call `b.Fatal` from a non-test goroutine (`testing-performance.md`).
- Every benchmark must assert its result is **non-degenerate** before the timed loop. A benchmark that measures a nil-guard early-return is worse than no benchmark: it makes a dead path look fast and invites "optimizations" that change nothing.
- DDD layer rules hold for any optimization the loop makes: parallel processing belongs in **Infrastructure**, never Interfaces or Domain (`ddd-architecture.md`).
- Commit messages follow `<type>: <description>` with types feat/fix/refactor/docs/test/chore/perf/ci (`git-workflow.md`). Benchmark-adding commits use `test:`; optimization commits use `perf:`.

---

## File Structure

| Path | Status | Responsibility |
|------|--------|----------------|
| `Makefile` | Modify | Add a `bench` target so every iteration measures identically. |
| `.git/info/exclude` | Modify | Keep the loop journal out of commits without touching the shared `.gitignore`. |
| `internal/common/purl/bench_test.go` | Create | Benchmarks `Parser.Parse` and `IsStableVersion` — per-component cost multiplied by component count. |
| `internal/infrastructure/eoltext/bench_test.go` | Create | Benchmarks `DetectLifecycle` — regex-heavy pure CPU over README/PyPI text. |
| `internal/infrastructure/depparser/cyclonedx/bench_test.go` | Create | Benchmarks `Parser.Parse` over a large generated SBOM. |
| `internal/infrastructure/depparser/cyclonedx/testdata/large_sbom.json` | Create (generated, committed) | ~2000-component CycloneDX SBOM, produced once by a committed generator. |
| `internal/interfaces/cli/bench_test.go` | Create | Benchmarks `renderScanOutput` for table/json/csv over a large entry set. |
| `internal/infrastructure/treesitter/bench_test.go` | Create | Benchmarks `Analyzer.AnalyzeCoupling` over a generated multi-language source corpus. `//go:build cgo`. |
| `internal/infrastructure/treesitter/corpus_test.go` | Create | Deterministic corpus generator shared by the benchmark. `//go:build cgo`. |
| `.perf-loop/journal.md` | Create (uncommitted) | The loop's memory across wake-ups. |
| `.perf-loop/ITERATION.md` | Create (uncommitted) | The exact prompt the loop re-executes each wake-up. |

Each benchmark file is package-local and self-contained: it owns its fixture and its correctness assertion, so a reviewer can approve or reject one target without reading the others.

---

### Task 1: Benchmark tooling and loop scaffolding

**Files:**
- Modify: `Makefile` (append after the `lint:` target, around line 20)
- Modify: `.git/info/exclude`

**Interfaces:**
- Consumes: nothing.
- Produces: `make bench` (runs every benchmark, 10 counts, writes nothing — stdout is redirected by the caller); `make bench-save FILE=<path>` (same run, tee'd to `<path>`). The `.perf-loop/` directory, git-excluded.

- [ ] **Step 1: Install benchstat and verify it runs**

```bash
go install golang.org/x/perf/cmd/benchstat@latest
"$(go env GOPATH)/bin/benchstat" -h
```

Expected: usage text on stderr and a non-crash exit. If the install fails because the network is unavailable, stop and record that in `.perf-loop/journal.md` (Step 5 creates it) — the loop can still run with median comparison, but every later claim of a "significant" win must then be stated as weaker evidence. Do not silently continue as if benchstat were present.

- [ ] **Step 2: Add the bench targets to the Makefile**

Append immediately after the existing `lint:` target:

```makefile
# ── Benchmark ──────────────────────────────────────────────

# bench: run every benchmark at 10 counts for benchstat comparison.
# CGO_ENABLED=1 is required because the treesitter package is //go:build cgo;
# without it those benchmarks are silently excluded from the run.
bench:
	CGO_ENABLED=1 go test ./... -run='^$$' -bench=. -benchmem -count=10

# bench-save: same run, captured to FILE for benchstat before/after comparison.
# Usage: make bench-save FILE=.perf-loop/bench-0.txt
bench-save:
	@test -n "$(FILE)" || (echo "FILE is required: make bench-save FILE=path" >&2; exit 1)
	CGO_ENABLED=1 go test ./... -run='^$$' -bench=. -benchmem -count=10 | tee $(FILE)
```

Add `bench bench-save` to the `.PHONY:` line at the top of the file.

- [ ] **Step 3: Verify the target runs and finds no benchmarks yet**

```bash
make bench 2>&1 | tail -20
```

Expected: packages report `ok` / `no test files` and **no** `Benchmark...` result lines, because none exist yet. This confirms the target is wired before any benchmark can mask a wiring bug.

- [ ] **Step 4: Exclude the loop journal from git**

```bash
mkdir -p .perf-loop
printf '\n# Local perf-loop state (see docs/superpowers/specs/2026-07-29-benchmark-driven-perf-loop-design.md)\n.perf-loop/\n' >> .git/info/exclude
git status --porcelain
```

Expected: `.perf-loop/` does **not** appear in the output. Only `Makefile` shows as modified.

- [ ] **Step 5: Seed the journal**

Write `.perf-loop/journal.md`:

```markdown
# Perf Loop Journal

Branch: perf/benchmark-driven-optimization
Design: docs/superpowers/specs/2026-07-29-benchmark-driven-perf-loop-design.md

## Tooling

- benchstat: INSTALLED | UNAVAILABLE (record which, and the reason if unavailable)

## Iterations

(none yet — iteration 0 is the baseline capture in Task 7)

## Consecutive iterations with no accepted win: 0
```

- [ ] **Step 6: Commit**

```bash
git add Makefile
git commit -m "chore: add make bench targets for benchstat comparison"
```

---

### Task 2: PURL parsing benchmarks

**Files:**
- Create: `internal/common/purl/bench_test.go`
- Read for reference: `internal/common/purl/parser.go:16-110`

**Interfaces:**
- Consumes: `make bench` from Task 1.
- Produces: `BenchmarkParserParse`, `BenchmarkIsStableVersion`. Package `purl` (internal test package — `parser_test.go` conventions apply; this file uses the internal package so it can be extended to unexported helpers later).

- [ ] **Step 1: Write the benchmark**

Create `internal/common/purl/bench_test.go`:

```go
package purl

import "testing"

// benchPURLs covers the shapes ParsePURL partitions on: namespaced Maven,
// flat Go, scoped npm, and a versionless entry. Parsing cost differs per
// shape, so a single representative would hide regressions in the others.
var benchPURLs = []string{
	"pkg:maven/org.slf4j/slf4j-api@2.0.16",
	"pkg:golang/github.com/gin-gonic/gin@v1.10.0",
	"pkg:npm/@babel/core@7.24.0",
	"pkg:npm/express@4.18.2",
	"pkg:pypi/requests",
}

func BenchmarkParserParse(b *testing.B) {
	p := NewParser()

	// Guard: a parser that errored on every input would benchmark the error
	// path, not the parse path, and would look misleadingly fast.
	for _, s := range benchPURLs {
		parsed, err := p.Parse(s)
		if err != nil {
			b.Fatalf("Parse(%q) failed during setup: %v", s, err)
		}
		if parsed == nil {
			b.Fatalf("Parse(%q) returned nil during setup", s)
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, s := range benchPURLs {
			if _, err := p.Parse(s); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkIsStableVersion(b *testing.B) {
	versions := []string{"1.2.3", "v1.10.0", "2.0.0-rc.1", "0.0.0-20240101120000-abcdef123456", "7.24.0"}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, v := range versions {
			_ = IsStableVersion(v)
		}
	}
}
```

- [ ] **Step 2: Run and confirm real numbers**

```bash
go test ./internal/common/purl/ -run='^$' -bench=. -benchmem -count=1
```

Expected: two `Benchmark...` lines with non-zero ns/op and B/op. If `BenchmarkParserParse` reports suspiciously few ns/op (single digits), the setup guard did not catch a degenerate path — investigate before continuing.

- [ ] **Step 3: Confirm the existing suite still passes**

```bash
go test ./internal/common/purl/ -count=1
```

Expected: `ok`.

- [ ] **Step 4: Commit**

```bash
git add internal/common/purl/bench_test.go
git commit -m "test: add PURL parsing benchmarks"
```

---

### Task 3: EOL text detection benchmarks

**Files:**
- Create: `internal/infrastructure/eoltext/bench_test.go`
- Read for reference: `internal/infrastructure/eoltext/detector.go:99-128` (`SourceKind`, `LifecycleDetectOpts`), `:410` (`DetectLifecycle`)

**Interfaces:**
- Consumes: `make bench` from Task 1.
- Produces: `BenchmarkDetectLifecycleReadme`, `BenchmarkDetectLifecyclePyPI`. Package `eoltext`.

`DetectLifecycle(opts LifecycleDetectOpts) DetectionResult` runs a battery of compiled regexes over README/description text. It is the pure-CPU core behind the evaluator's text rules and is reachable without any client, which is why it is benchmarked here rather than through `Evaluator.EvaluateBatch` — most of the evaluator's clients are concrete types (`*nuget.Client`, `*maven.Client`, …) that cannot be replaced with hermetic stubs, so an `EvaluateBatch` benchmark with nil clients would measure channel overhead and short-circuits rather than real work.

The `SourceKind` constants are `SourcePyPI`, `SourceReadme`, `SourceShortMessage` (`detector.go:102-106`), and `LifecycleDetectOpts` has fields `Source`, `PackageName`, `RepoName`, `Text` (`detector.go:123-128`). The code below uses those names directly.

- [ ] **Step 1: Write the benchmark**

Create `internal/infrastructure/eoltext/bench_test.go`:

```go
package eoltext

import (
	"strings"
	"testing"
)

// benchReadme is a realistic deprecation README: long enough that regex
// scanning dominates, with the EOL phrase late in the text so early-exit
// paths do not make the benchmark trivially cheap.
var benchReadme = strings.Repeat(
	"# example-lib\n\nA utility library for parsing configuration files.\n\n"+
		"## Installation\n\n    npm install example-lib\n\n"+
		"## Usage\n\nSee the documentation for details on the API surface.\n\n",
	40,
) + "\n## Notice\n\nThis project is deprecated and no longer maintained. Please use example-lib-ng instead.\n"

func BenchmarkDetectLifecycleReadme(b *testing.B) {
	opts := LifecycleDetectOpts{
		Source:   SourceReadme,
		RepoName: "example-lib",
		Text:     benchReadme,
	}

	// Guard: if detection finds nothing, this benchmark measures a full-scan
	// miss rather than the match path. Both are worth measuring, but we must
	// know which one we have.
	got := DetectLifecycle(opts)
	b.Logf("setup detection result: %+v", got)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DetectLifecycle(opts)
	}
}

func BenchmarkDetectLifecyclePyPI(b *testing.B) {
	opts := LifecycleDetectOpts{
		Source:      SourcePyPI,
		PackageName: "example-lib",
		Text: "Deprecated utility library\n" + strings.Repeat(
			"This package provides helpers for reading configuration files. ", 60,
		) + "\nThis package is deprecated; use example-lib-ng.",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = DetectLifecycle(opts)
	}
}
```

- [ ] **Step 2: Run and read the setup log**

```bash
go test ./internal/infrastructure/eoltext/ -run='^$' -bench=. -benchmem -count=1 -v 2>&1 | head -20
```

Expected: both benchmarks report non-zero ns/op, and the `setup detection result` line shows a populated `DetectionResult` (a detected lifecycle state, not the zero value). If it is the zero value, adjust `benchReadme` so the phrasing matches the detector's patterns — check `ExplicitPatternsForStats()` in `detector.go:207` for the phrasings it recognizes — and re-run.

- [ ] **Step 3: Confirm the existing suite still passes**

```bash
go test ./internal/infrastructure/eoltext/ -count=1
```

Expected: `ok`.

- [ ] **Step 4: Commit**

```bash
git add internal/infrastructure/eoltext/bench_test.go
git commit -m "test: add EOL text detection benchmarks"
```

---

### Task 4: CycloneDX SBOM parsing benchmark

**Files:**
- Create: `internal/infrastructure/depparser/cyclonedx/bench_test.go`
- Create: `internal/infrastructure/depparser/cyclonedx/testdata/large_sbom.json` (generated in Step 1, committed)
- Read for reference: `internal/infrastructure/depparser/cyclonedx/parser.go:62-76`

**Interfaces:**
- Consumes: `make bench` from Task 1.
- Produces: `BenchmarkParseLargeSBOM`. Package `cyclonedx_test` (external), matching `parser_test.go`.

- [ ] **Step 1: Generate the fixture**

Unlike the source corpus in Task 6, this fixture is a single ~1 MB JSON file, so committing it is cheaper than regenerating it and guarantees byte-identical input across machines. Generate it once with this throwaway script:

```bash
python3 - <<'PY'
import json
comps = []
deps = []
for i in range(2000):
    ref = f"pkg:npm/pkg-{i}@1.{i % 50}.{i % 7}"
    comps.append({
        "type": "library",
        "name": f"pkg-{i}",
        "version": f"1.{i % 50}.{i % 7}",
        "purl": ref,
        "bom-ref": ref,
        "licenses": [{"license": {"id": "MIT"}}],
    })
    # Every component depends on the next 3, giving the dependency-graph
    # walk real work instead of a flat list.
    deps.append({"ref": ref, "dependsOn": [f"pkg:npm/pkg-{j}@1.{j % 50}.{j % 7}"
                                            for j in range(i + 1, min(i + 4, 2000))]})
sbom = {
    "bomFormat": "CycloneDX",
    "specVersion": "1.5",
    "version": 1,
    "components": comps,
    "dependencies": deps,
}
with open("internal/infrastructure/depparser/cyclonedx/testdata/large_sbom.json", "w") as f:
    json.dump(sbom, f)
PY
ls -la internal/infrastructure/depparser/cyclonedx/testdata/large_sbom.json
```

Expected: a file of roughly 700 KB–1 MB. (Python is the project's mandated tool for JSON manipulation — `project-conventions.md` forbids PowerShell's `ConvertTo-Json` for this.)

- [ ] **Step 2: Write the benchmark**

Create `internal/infrastructure/depparser/cyclonedx/bench_test.go`:

```go
package cyclonedx_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/infrastructure/depparser/cyclonedx"
)

func BenchmarkParseLargeSBOM(b *testing.B) {
	data, err := os.ReadFile(filepath.Join("testdata", "large_sbom.json"))
	if err != nil {
		b.Fatalf("reading fixture: %v", err)
	}

	p := &cyclonedx.Parser{}
	ctx := context.Background()

	// Guard: assert the fixture actually parses into the expected component
	// count. A fixture that silently yields zero dependencies would make the
	// benchmark measure an early error return.
	deps, err := p.Parse(ctx, data)
	if err != nil {
		b.Fatalf("parsing fixture: %v", err)
	}
	if len(deps) != 2000 {
		b.Fatalf("fixture produced %d dependencies, want 2000", len(deps))
	}

	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := p.Parse(ctx, data); err != nil {
			b.Fatal(err)
		}
	}
}
```

- [ ] **Step 3: Run and verify the guard passes**

```bash
go test ./internal/infrastructure/depparser/cyclonedx/ -run='^$' -bench=. -benchmem -count=1
```

Expected: one benchmark line with a large ns/op (this parses ~1 MB per iteration) and a MB/s figure from `SetBytes`. If it fails with "fixture produced N dependencies, want 2000", the parser deduplicates or filters — read the actual count from the error, confirm it is the correct expected value by reading `parser.go`, and update the constant rather than deleting the guard.

- [ ] **Step 4: Confirm the existing suite still passes**

```bash
go test ./internal/infrastructure/depparser/... -count=1
```

Expected: `ok` for each package.

- [ ] **Step 5: Commit**

```bash
git add internal/infrastructure/depparser/cyclonedx/bench_test.go \
        internal/infrastructure/depparser/cyclonedx/testdata/large_sbom.json
git commit -m "test: add large CycloneDX SBOM parsing benchmark"
```

---

### Task 5: CLI scan rendering benchmarks

**Files:**
- Create: `internal/interfaces/cli/bench_test.go`
- Read for reference: `internal/interfaces/cli/scan_render_test.go:15-39` (`makeTestEntries` shape), `scan_render.go:158` (`renderScanOutput`)

**Interfaces:**
- Consumes: `make bench` from Task 1.
- Produces: `BenchmarkRenderScanOutput` (sub-benchmarks: `table`, `json`, `csv`). Package `cli` (internal — `renderScanOutput` is unexported).

- [ ] **Step 1: Write the benchmark**

Create `internal/interfaces/cli/bench_test.go`:

```go
package cli

import (
	"fmt"
	"io"
	"testing"

	"github.com/future-architect/uzomuzo-oss/internal/domain/analysis"
	domainaudit "github.com/future-architect/uzomuzo-oss/internal/domain/audit"
)

// makeBenchEntries builds a large, realistic entry set. Verdicts and axis
// results are varied so conditional-column logic (hasMultipleSources,
// hasRelationInfo) takes its real branches rather than the all-empty path.
func makeBenchEntries(n int) []domainaudit.AuditEntry {
	entries := make([]domainaudit.AuditEntry, 0, n)
	verdicts := []domainaudit.Verdict{
		domainaudit.VerdictOK,
		domainaudit.VerdictReplace,
		domainaudit.VerdictReview,
	}
	for i := 0; i < n; i++ {
		e := domainaudit.AuditEntry{
			PURL:    fmt.Sprintf("pkg:npm/pkg-%d@1.%d.0", i, i%50),
			Verdict: verdicts[i%len(verdicts)],
		}
		switch i % 3 {
		case 0:
			e.Analysis = &analysis.Analysis{
				AxisResults: map[analysis.AssessmentAxis]*analysis.AssessmentResult{
					analysis.LifecycleAxis: {Label: string(analysis.LabelActive)},
				},
			}
		case 1:
			e.Analysis = &analysis.Analysis{
				EOL: analysis.EOLStatus{State: analysis.EOLEndOfLife},
			}
		default:
			e.Analysis = nil // exercises the nil-Analysis renderer branch
		}
		entries = append(entries, e)
	}
	return entries
}

func BenchmarkRenderScanOutput(b *testing.B) {
	entries := makeBenchEntries(1000)

	for _, format := range []string{"table", "json", "csv"} {
		b.Run(format, func(b *testing.B) {
			// Guard: confirm the renderer succeeds and emits a non-trivial
			// amount of output before timing it.
			cw := &countingWriter{}
			if err := renderScanOutput(cw, entries, entries, format, false); err != nil {
				b.Fatalf("renderScanOutput(%s) failed during setup: %v", format, err)
			}
			if cw.n < 1000 {
				b.Fatalf("renderScanOutput(%s) wrote only %d bytes, expected substantial output", format, cw.n)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := renderScanOutput(io.Discard, entries, entries, format, false); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// countingWriter counts bytes written without retaining them, so the setup
// guard can assert on output size without allocating a megabyte buffer.
type countingWriter struct{ n int }

func (w *countingWriter) Write(p []byte) (int, error) {
	w.n += len(p)
	return len(p), nil
}
```

- [ ] **Step 2: Run and verify all three sub-benchmarks report**

```bash
go test ./internal/interfaces/cli/ -run='^$' -bench=BenchmarkRenderScanOutput -benchmem -count=1
```

Expected: three lines — `BenchmarkRenderScanOutput/table`, `/json`, `/csv` — each with non-zero ns/op and B/op. If compilation fails on `analysis.LabelActive` or `analysis.LifecycleAxis`, read the exact identifiers from `scan_render_test.go:20-32` and match them; that file is known to compile against the current API.

- [ ] **Step 3: Confirm the existing suite still passes**

```bash
go test ./internal/interfaces/cli/ -count=1
```

Expected: `ok`.

- [ ] **Step 4: Commit**

```bash
git add internal/interfaces/cli/bench_test.go
git commit -m "test: add CLI scan rendering benchmarks"
```

---

### Task 6: Tree-sitter coupling analysis benchmark

**Files:**
- Create: `internal/infrastructure/treesitter/corpus_test.go`
- Create: `internal/infrastructure/treesitter/bench_test.go`
- Read for reference: `internal/infrastructure/treesitter/analyzer.go:29-39` (`skipDirs`), `:133` (`NewAnalyzer`), `:169-217` (`AnalyzeCoupling`)

**Interfaces:**
- Consumes: `make bench` from Task 1.
- Produces: `writeBenchCorpus(tb testing.TB, filesPerLang int) (root string, importPaths map[string][]string)` in `corpus_test.go`; `BenchmarkAnalyzeCoupling` in `bench_test.go`. Package `treesitter`, both files `//go:build cgo`.

**Note on fixture placement:** the corpus is generated into `b.TempDir()` rather than committed under `testdata/`. Two reasons, both specific to this target. First, `skipDirs` in `analyzer.go:34` contains `"testdata"` — a committed corpus risks being skipped by the very walker under test if any nested directory is named that. Second, committing several hundred synthetic source files into a dependency-analysis tool's own repository invites its CI dependency scanners to pick them up as real dependencies. Determinism is preserved because the generator itself is committed and uses no randomness. This is a deliberate, reasoned departure from the "test data lives in `testdata/`" convention, limited to this one fixture.

- [ ] **Step 1: Write the corpus generator**

Create `internal/infrastructure/treesitter/corpus_test.go`:

```go
//go:build cgo

package treesitter

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// writeBenchCorpus generates a deterministic multi-language source tree and
// returns its root plus the importPaths map that maps PURLs to import paths.
//
// The corpus is generated rather than committed: analyzer.go's skipDirs
// contains "testdata", so a committed corpus could be skipped by the walker
// under test. No randomness is used, so successive runs produce byte-identical
// trees and benchmark numbers stay comparable across commits.
func writeBenchCorpus(tb testing.TB, filesPerLang int) (string, map[string][]string) {
	tb.Helper()
	root := tb.TempDir()

	importPaths := map[string][]string{
		"pkg:golang/github.com/foo/bar@v1.0.0": {"github.com/foo/bar"},
		"pkg:npm/lodash@4.17.21":               {"lodash"},
		"pkg:pypi/requests@2.31.0":             {"requests"},
		"pkg:maven/com.google.code.gson/gson@2.10.1": {"com.google.gson"},
	}

	// Nested directories exercise the WalkDir recursion, not just a flat scan.
	for i := 0; i < filesPerLang; i++ {
		dir := filepath.Join(root, "src", fmt.Sprintf("mod%d", i%10))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			tb.Fatalf("creating corpus dir: %v", err)
		}

		writeCorpusFile(tb, filepath.Join(dir, fmt.Sprintf("file%d.go", i)), fmt.Sprintf(`package mod%d

import (
	"fmt"
	"github.com/foo/bar"
)

func Run%d() {
	bar.Do()
	bar.Also()
	fmt.Println(bar.Value())
}
`, i%10, i))

		writeCorpusFile(tb, filepath.Join(dir, fmt.Sprintf("file%d.js", i)), fmt.Sprintf(`const _ = require('lodash');

function run%d() {
  _.map([1, 2, 3], (x) => x * 2);
  return _.uniq([1, 1, 2]);
}

module.exports = { run%d };
`, i, i))

		writeCorpusFile(tb, filepath.Join(dir, fmt.Sprintf("file%d.py", i)), fmt.Sprintf(`import requests


def run_%d():
    resp = requests.get("https://example.com")
    return requests.utils.default_headers(), resp
`, i))

		writeCorpusFile(tb, filepath.Join(dir, fmt.Sprintf("File%d.java", i)), fmt.Sprintf(`package mod%d;

import com.google.gson.Gson;

public class File%d {
    public String run() {
        Gson gson = new Gson();
        return gson.toJson(new Object());
    }
}
`, i%10, i))
	}

	return root, importPaths
}

func writeCorpusFile(tb testing.TB, path, content string) {
	tb.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		tb.Fatalf("writing corpus file %s: %v", path, err)
	}
}
```

- [ ] **Step 2: Write the benchmark**

Create `internal/infrastructure/treesitter/bench_test.go`:

```go
//go:build cgo

package treesitter

import (
	"context"
	"testing"
)

// BenchmarkAnalyzeCoupling measures the dominant CPU cost of `uzomuzo-diet`:
// walking a source tree and tree-sitter-parsing every matching file.
func BenchmarkAnalyzeCoupling(b *testing.B) {
	root, importPaths := writeBenchCorpus(b, 50) // 50 files x 4 languages = 200 files
	ctx := context.Background()

	analyzer := NewAnalyzer()
	b.Cleanup(analyzer.Close)

	// Guard: AnalyzeCoupling returns (nil, nil) when no coupling data is
	// collected. Benchmarking that path would measure a walk that parses
	// nothing and would make any "optimization" look free.
	result, err := analyzer.AnalyzeCoupling(ctx, root, importPaths)
	if err != nil {
		b.Fatalf("AnalyzeCoupling failed during setup: %v", err)
	}
	if len(result) == 0 {
		b.Fatal("AnalyzeCoupling returned no coupling data; the benchmark would measure a no-op")
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := analyzer.AnalyzeCoupling(ctx, root, importPaths); err != nil {
			b.Fatal(err)
		}
	}
}
```

- [ ] **Step 3: Run and confirm the guard passes**

```bash
CGO_ENABLED=1 go test ./internal/infrastructure/treesitter/ -run='^$' -bench=BenchmarkAnalyzeCoupling -benchmem -count=1
```

Expected: one benchmark line, likely in the tens of milliseconds per iteration. If it fails with "returned no coupling data", the import paths in the corpus do not match what the extractors produce — run the existing `TestAnalyzer_SkipsDirs` and the `lang_*_test.go` suites to see the exact import-path forms each language extractor emits, then align the `importPaths` map.

- [ ] **Step 4: Verify the corpus is not skipped**

```bash
CGO_ENABLED=1 go test ./internal/infrastructure/treesitter/ -run='^$' -bench=BenchmarkAnalyzeCoupling -benchtime=1x -v 2>&1 | head -20
```

Expected: the benchmark completes. This run exists to confirm the corpus root name is not one of `skipDirs` (`vendor`, `node_modules`, `.git`, `testdata`, `__pycache__`, `build`, `dist`, `target`) — `b.TempDir()` generates a name derived from the benchmark name, so it is not, but confirm rather than assume.

- [ ] **Step 5: Confirm the existing suite still passes, with race detection**

```bash
CGO_ENABLED=1 go test ./internal/infrastructure/treesitter/ -count=1
CGO_ENABLED=1 go test ./internal/infrastructure/treesitter/ -race -count=1
```

Expected: `ok` for both. The race run matters here specifically: parallelizing `AnalyzeCoupling` is the single most likely optimization the loop will attempt, so a clean race baseline must exist before that change lands.

- [ ] **Step 6: Commit**

```bash
git add internal/infrastructure/treesitter/corpus_test.go \
        internal/infrastructure/treesitter/bench_test.go
git commit -m "test: add tree-sitter coupling analysis benchmark"
```

---

### Task 7: Capture the baseline and write the iteration prompt

**Files:**
- Create: `.perf-loop/bench-0.txt` (uncommitted)
- Create: `.perf-loop/ITERATION.md` (uncommitted)
- Modify: `.perf-loop/journal.md` (uncommitted)

**Interfaces:**
- Consumes: `make bench-save` from Task 1; all benchmarks from Tasks 2–6.
- Produces: the baseline measurement file and the exact prompt Task 8's loop re-executes each wake-up.

- [ ] **Step 1: Verify every benchmark is present in one run**

```bash
make bench 2>&1 | grep -c '^Benchmark'
```

Expected: at least **8** benchmark result lines at `-count=10`… note that `-count=10` emits 10 lines per benchmark, so expect roughly 80. The point of this step is to confirm **no target is missing**. Cross-check the distinct names:

```bash
make bench 2>&1 | grep '^Benchmark' | sed 's/-[0-9]*  *.*//' | sort -u
```

Expected exactly these names: `BenchmarkAnalyzeCoupling`, `BenchmarkDetectLifecyclePyPI`, `BenchmarkDetectLifecycleReadme`, `BenchmarkIsStableVersion`, `BenchmarkParseLargeSBOM`, `BenchmarkParserParse`, `BenchmarkRenderScanOutput/csv`, `BenchmarkRenderScanOutput/json`, `BenchmarkRenderScanOutput/table`. If `BenchmarkAnalyzeCoupling` is absent, `CGO_ENABLED=1` did not take effect — fix that before capturing a baseline, or the loop will be blind to its most important target.

- [ ] **Step 2: Capture the baseline**

```bash
make bench-save FILE=.perf-loop/bench-0.txt
```

Expected: the file exists and contains the full result set. This is iteration 0 and is exempt from the win requirement — it only establishes the reference point.

- [ ] **Step 3: Write the iteration prompt**

Write `.perf-loop/ITERATION.md`. This file is the loop's instruction set; it is written once and re-read at every wake-up:

```markdown
# Perf Loop — One Iteration

Working directory: the `perf-bench-loop` worktree, branch `perf/benchmark-driven-optimization`.
Design: `docs/superpowers/specs/2026-07-29-benchmark-driven-perf-loop-design.md`

## Before anything else

Read `.perf-loop/journal.md` in full. It is the only memory that survives between
wake-ups. Note the current "consecutive iterations with no accepted win" counter.

## Stop conditions — check these FIRST

Stop the loop (call ScheduleWakeup with `stop: true`) and go to "On stop" if either holds:

- the no-win counter is **>= 2**, or
- the journal records **8** completed iterations.

## The iteration

1. **Profile.** Take the slowest or most-allocating target from the most recent
   `bench-<n>.txt` that you have not already optimized to exhaustion. Re-run just
   that benchmark with profiling:

       CGO_ENABLED=1 go test ./<pkg>/ -run='^$' -bench=<Name> -benchmem \
         -cpuprofile=.perf-loop/cpu-<n>.prof -memprofile=.perf-loop/mem-<n>.prof -count=5
       go tool pprof -top -nodecount=25 .perf-loop/cpu-<n>.prof

2. **Choose exactly one hotspot** and write the hypothesis into the journal before
   changing any code. One unknown per change.

3. **Implement** the minimal change that tests the hypothesis. Layer rules hold:
   parallel processing belongs in Infrastructure only. Any concurrency change must
   preserve deterministic output ordering — sort before emit.

4. **Gate on correctness.** All three must pass:

       go test ./... -count=1
       go vet ./...
       golangci-lint run

   If the change introduced concurrency, additionally:

       CGO_ENABLED=1 go test ./<pkg>/ -race -count=1

5. **Verify the win.**

       make bench-save FILE=.perf-loop/bench-<n>.txt
       benchstat .perf-loop/bench-<n-1>.txt .perf-loop/bench-<n>.txt

6. **Accept or revert.** Accept only if ALL hold:
   - the target metric improves by >= 10%,
   - benchstat reports p < 0.05 for that improvement,
   - no other benchmark regresses by more than 5%.

   Otherwise `git checkout -- <changed files>` and record the refuted hypothesis.
   A refuted hypothesis is a real result — record what you learned, not just "failed".

7. **Commit** an accepted change as one commit:

       git commit -m "perf: <what changed>

       benchstat: <metric> <before> -> <after> (<pct>%, p=<p>)"

8. **Update the journal**: iteration number, target, hypothesis, verdict, benchstat
   delta, commit SHA, and the new no-win counter (reset to 0 on accept, +1 on revert).

9. **Schedule the next wake-up** with ScheduleWakeup, or stop if a stop condition now holds.

## On stop

1. Write a summary section at the end of the journal: accepted optimizations with
   their deltas, and refuted hypotheses with what they ruled out.
2. Run the end-to-end verification required by `git-workflow.md`. Build the real
   binary and time it against a real repository, before and after:

       make build-all
       # time uzomuzo-diet against a checked-out OSS repo, and compare against
       # the same command run from the merge-base commit

3. Report back to the user with the benchstat table and the end-to-end timings.
   **Do not open the PR without asking** — `git-workflow.md` requires user
   confirmation before merging, and the user should see the evidence first.
```

- [ ] **Step 4: Record the baseline in the journal**

Append to `.perf-loop/journal.md`:

```markdown
### Iteration 0 — baseline

- Captured: .perf-loop/bench-0.txt
- Targets present: (paste the sorted unique benchmark names from Step 1)
- Verdict: baseline only, exempt from the win requirement
- No-win counter: 0
```

- [ ] **Step 5: Confirm nothing leaked into git**

```bash
git status --porcelain
```

Expected: empty. All of Task 7's artifacts are under the git-excluded `.perf-loop/`.

---

### Task 8: Launch the loop

**Files:** none — this task runs the loop built by Tasks 1–7.

**Interfaces:**
- Consumes: `.perf-loop/ITERATION.md`, `.perf-loop/journal.md`, `.perf-loop/bench-0.txt`.
- Produces: `perf:` commits on `perf/benchmark-driven-optimization`, and a final report to the user.

- [ ] **Step 1: Confirm the working tree is clean and the baseline exists**

```bash
git status --porcelain && ls -la .perf-loop/
```

Expected: no output from `git status`; `.perf-loop/` contains `journal.md`, `ITERATION.md`, and `bench-0.txt`.

- [ ] **Step 2: Start the loop**

Invoke:

```
/loop Execute one iteration of .perf-loop/ITERATION.md
```

No interval is given, so the loop runs in dynamic mode and paces itself with
`ScheduleWakeup`. Because each iteration is compute-bound local work with no
external state to poll, wake-ups belong in the idle-tick range rather than
rapid polling.

- [ ] **Step 3: Let it converge, then review**

The loop stops itself on two consecutive no-win iterations or at 8 iterations.
When it reports back, review the benchstat table and the end-to-end timings
before deciding whether to open the PR.

---

## Notes for the Reviewer

Tasks 1–6 add **no production code** — only benchmarks, a Makefile target, and
local scaffolding. If any of those tasks finds itself editing a non-test `.go`
file, something has gone wrong: optimization is the loop's job in Task 8, and
mixing it into the measurement setup destroys the baseline the loop depends on.

The one place this plan knowingly departs from a repo convention is Task 6's
generated corpus, for the two reasons stated in that task's note.
