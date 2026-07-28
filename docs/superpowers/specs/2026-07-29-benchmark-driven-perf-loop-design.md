# Benchmark-Driven Performance Loop — Design

Date: 2026-07-29
Status: Approved (design), pending implementation plan

## 1. Problem

`uzomuzo` has zero benchmarks. `grep "func Benchmark" internal/ pkg/` returns nothing, while
19 fuzz targets exist. Correctness has a safety net; speed has none. There is no way to answer
"is this change faster?" or "where does `diet` actually spend its time?" with evidence rather
than intuition.

The repository rule `testing-performance.md` already states the policy — *"Write Benchmarks for
Hot Spots: Do not optimize prematurely. Use the `testing` package to benchmark
performance-critical functions and prove an optimization is needed."* — but nothing implements it.

## 2. Goal

Stand up a benchmark baseline over the CPU-hot paths of the two binaries, then run a self-paced
`/loop` that repeatedly does: **measure → profile → optimize exactly one hotspot → verify with
`benchstat` → commit or revert**. The loop stops when it converges and the accumulated commits
ship as a single PR.

Explicitly *not* a goal (YAGNI): CI benchmark gating, a long-lived performance-regression
tracking service, property-based testing adoption, or any architectural rewrite. Those are
separate decisions and this design does not pre-commit to them.

## 3. Benchmark Targets

Targets are chosen because they sit on the real CPU path of `uzomuzo scan` / `uzomuzo-diet`, not
because they are convenient to benchmark. Each must be **hermetic** — no network, no live API —
so `go test ./...` stays runnable in CI and restricted-network environments
(`testing-performance.md`, "Keep Tests Network-Independent by Default").

| # | Package | Benchmark | Why it is on the hot path |
|---|---------|-----------|---------------------------|
| 1 | `internal/infrastructure/treesitter` | `BenchmarkAnalyzeCoupling` | `AnalyzeCoupling` walks the entire source tree and tree-sitter-parses **every** matching file through a single `*sitter.Parser` in one sequential `filepath.WalkDir`. This is the dominant cost of `diet` coupling analysis on any real repository. |
| 2 | `internal/infrastructure/depparser/cyclonedx` | `BenchmarkParse` | Parses SBOMs with thousands of components; runs once per invocation but over large input. |
| 3 | `internal/common/purl` | `BenchmarkParse`, `BenchmarkNormalizeMavenCollapsedCoordinates` | Called once per component in every code path; small per-call cost multiplied by component count. |
| 4 | `internal/infrastructure/eoltext` | `BenchmarkDetectLifecycleReadme`, `BenchmarkDetectLifecyclePyPI` | Regex battery over README / PyPI description text. This is the pure-CPU core behind the EOL evaluator's text rules, and it is reachable with no client at all. |
| 5 | `internal/interfaces/cli` | `BenchmarkRenderScan` | Allocation-heavy string/box rendering over a large result set. |

Target 4 deliberately benchmarks `eoltext` rather than `eolevaluator.EvaluateBatch`. The
evaluator's clients are concrete types (`*nuget.Client`, `*maven.Client`, `*pypi.Client`, …), not
interfaces, so they cannot be replaced with hermetic stubs; a benchmark built on nil clients would
measure worker-channel overhead and short-circuit returns rather than real evaluation work.
`eoltext.DetectLifecycle` is the CPU-heavy part that batch actually spends time in, and it takes
plain values.

Constraints that fall out of the repo's rules:

- Target 1 needs `//go:build cgo` on its benchmark file, matching the package
  (`testing-performance.md`, "Propagate Build Tags to Test Files").
- Fixtures live under each package's `testdata/` (`project-conventions.md`), with one reasoned
  exception: target 1's multi-language source corpus is produced at run time by a **committed
  deterministic generator** into `b.TempDir()`. `skipDirs` in `analyzer.go:34` contains
  `"testdata"`, so a committed corpus risks being skipped by the very walker under test, and
  committing hundreds of synthetic source files into a dependency-analysis tool's own repository
  invites its scanners to treat them as real dependencies. The generator uses no randomness, so
  runs stay byte-identical and benchmark numbers stay comparable across commits.
- Benchmarks must not `t.Fatal` from non-test goroutines and must `Close()` tree-sitter handles
  (`testing-performance.md`).

## 4. Loop Mechanics

`/loop` is invoked **without an interval**, i.e. dynamic mode: the model paces itself with
`ScheduleWakeup` and one wake-up performs exactly one iteration. Iterations are sequential by
construction — each one measures the tree the previous one produced — so no parallel fan-out.

State lives in `.perf-loop/journal.md` inside the worktree, registered in `.git/info/exclude` so
it never enters a commit. It is the loop's memory across wake-ups and is re-read at the start of
every iteration. Per iteration it records: target benchmark, hypothesis, the change made,
`benchstat` delta, accept/revert verdict, commit SHA, and the running no-win counter.

### One iteration

1. **Measure.** `go test -run='^$' -bench=. -benchmem -count=10 ./...` → `bench-<n>.txt`.
2. **Profile.** For the slowest / most-allocating target, re-run with `-cpuprofile` and
   `-memprofile`, then `go tool pprof -top`.
3. **Choose one.** Exactly one hotspot per iteration, with a written hypothesis in the journal.
   One unknown per change (`project-conventions.md`, "One unknown per change").
4. **Implement.** The minimal change that tests the hypothesis. DDD layer rules hold: parallel
   processing belongs in Infrastructure, never in Interfaces or Domain
   (`ddd-architecture.md`). Any change that introduces concurrency must preserve deterministic
   output ordering (`copilot-learned-coding.md`, "Deterministic Output from Non-Deterministic
   Sources").
5. **Gate on correctness.** `go test ./... -count=1`, `go vet ./...`, `golangci-lint run` must all
   pass. Concurrency changes additionally require `go test -race` on the touched packages.
6. **Verify.** Re-run the benchmark at `-count=10` and compare with
   `benchstat before.txt after.txt`.
7. **Accept or revert.** Accept only if the target metric improves by **≥10%** with
   `benchstat` p < 0.05, **and** no other benchmark regresses by more than 5%. Otherwise
   `git checkout` the change away and record the negative result in the journal — a refuted
   hypothesis is a real output, not a failure.
8. **Commit.** One accepted optimization = one commit, with the `benchstat` delta in the message
   body.
9. **Decide.** Update the journal, evaluate the termination condition, and either
   `ScheduleWakeup` for the next iteration or stop.

## 5. Termination

The loop stops when **two consecutive iterations produce no accepted win**, which is read as
convergence: the cheap wins are gone and further iterations would burn tokens on noise. A hard
cap of **8 iterations** is a backstop in case the accept criterion keeps being marginally met.

Iteration 0 is the bootstrap and is exempt from the win requirement — it only establishes the
baseline.

`ScheduleWakeup` delays: a benchmark iteration is compute-bound local work with nothing external
to poll, so the loop schedules its next wake-up in the idle-tick range rather than polling
rapidly.

## 6. Deliverable

A single PR from branch `perf/benchmark-driven-optimization`, containing the benchmark
infrastructure (iteration 0) plus one commit per accepted optimization.

The PR body must carry a **Runtime verification** section with real evidence
(`git-workflow.md`), not just green tests:

- the `benchstat` before/after table for every target, and
- a real end-to-end run of the actual binary — `uzomuzo-diet` against a checked-out OSS
  repository — with wall-clock timings before and after, to confirm the microbenchmark win shows
  up in the user-visible path.

Negative results from reverted iterations are summarised in the PR body too. They are the
evidence that the remaining code paths were examined and found already adequate.

## 7. Risks and Mitigations

| Risk | Mitigation |
|------|-----------|
| `benchstat` is not installed (confirmed absent from `$GOPATH/bin`) | Bootstrap step runs `go install golang.org/x/perf/cmd/benchstat@latest`. If the network blocks it, fall back to `-count=10` with median comparison and state the weaker evidence explicitly in the journal and PR. |
| Noisy measurements on WSL2 | `-count=10` plus `benchstat` p-values rather than single runs; no other heavy work scheduled concurrently during a measurement. |
| Optimizing something that is not really hot | Target selection is gated on `pprof` output from a realistic fixture, never on intuition about which code "looks slow". |
| Concurrency change breaks deterministic output | Sort before emit; `-race` required; the existing test suite (which asserts exact rendered output) is the guard. |
| Loop loses context across wake-ups | `.perf-loop/journal.md` is the single source of truth and is re-read at the start of each iteration. |
| A "win" is really a benchmark artifact | The end-to-end binary timing in the PR is the cross-check; a microbenchmark win that does not move the real run is reported as such. |

## 8. Fixture Sizes

Fixed by the implementation plan: 2000 components for the CycloneDX SBOM, 50 files per language
(200 files total) for the tree-sitter corpus, 1000 entries for the CLI render benchmark. Each is
sized so a single benchmark run takes seconds.
