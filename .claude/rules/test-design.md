<!-- Generated from .github/instructions/test-design.instructions.md — DO NOT EDIT DIRECTLY -->

# Test Design — pre-PR lens

A rule for **what to test**. The companion rule `testing-performance.md` covers
**how to write Go tests** (`t.Parallel`, `t.Cleanup`, fixture layout, race detection,
benchmark hygiene). Post-PR review patterns that were actually caught land in
`copilot-learned-coding.md`. This document is the pre-PR lens that prevents the
"what cases did I forget?" gap before code review starts.

## When to consult

- A planner or architect drafting a test plan for a new feature
- An implementer writing a new function / service / infrastructure client
- A code reviewer asking "is edge-case coverage sufficient?" / "is there a fuzz
  target missing for this parser?"

## Cross-layer rule

**Tests respect the same layer rules as production code.** Under DDD,
`internal/domain/**` tests must not import `internal/infrastructure/**` or
third-party modules that infrastructure depends on. Domain tests stay
pure-Go + stdlib + your own domain types. Application-layer tests may compose
mocks of infrastructure interfaces but must not import concrete infrastructure
clients (depsdev, GitHub, nuget, pypi, etc.) directly.

When property-based testing (`pgregory.net/rapid`) is adopted in the future, do
not put rapid imports directly under `internal/domain/**`. Use an isolated
package (e.g. `internal/propertytests/<layer>/`) gated by a build tag
(`//go:build pbt`) so production code remains dep-free.

## Input-selection techniques (classical QA)

Each technique gets a 1-2 line definition and a concrete pin in this repo. The
pins are existing tests — the point here is to show what each technique looks
like in this codebase, not to introduce new tests.

### Equivalence Partitioning

Divide the input domain into classes that should behave identically; test one
representative per class. The point is not coverage by line count but coverage
by behavior class — three inputs in the same class do not add information.

- Repo pin: `internal/common/purl/parser.go::ParsePURL` and
  `internal/common/purl/parser_test.go`. Input domain partitions by PURL shape:
  known ecosystem with namespace (Maven), known ecosystem without namespace
  (Go, npm flat), known ecosystem with version qualifier, malformed string,
  empty string. One representative per class.

### Boundary Value Analysis

Test the boundary ±1 (off-by-one, overflow, empty, single element, very large).
Bugs cluster at boundaries; representative-input tests miss them.

- Repo pin: `internal/infrastructure/eolevaluator/evaluator.go::Evaluator.SetMaxWorkers`
  and any caller test that exercises worker-count edges (0 / 1 / default /
  large). When the value clamps or saturates, the boundary on each side of the
  clamp matters.

### Decision Table

Enumerate combinations of multiple conditions as a table. Each row is a cell of
the Cartesian product of the axes; the test names the axis values and pins the
expected outcome. Use this whenever output depends on more than one input
condition (typically 2-4 axes).

- Repo pin: `internal/infrastructure/eolevaluator/helpers.go::decideNuGetEOL`
  and `internal/infrastructure/eolevaluator/helpers_test.go::Test_decideNuGetEOL`.
  Axes: `info.AlternatePackageID` (present / absent) × `reason` (CriticalBugs /
  Legacy / Other) × message strength heuristic (strong successor phrasing /
  none). The existing table-driven test enumerates the meaningful combinations
  and pins state / successor / confidence per cell.

### State Transition

For state-bearing types, enumerate which initial state × which event leads to
which next state. Failure modes that example-based tests miss: "the second
registration silently overwrites" / "Lookup before any registration returns an
inappropriate zero value" / "a state that should be a dead end accepts events
anyway."

- Repo pin: `internal/infrastructure/eolevaluator/evaluator.go::Evaluator`
  and the `evaluator_test.go` family (e.g. `evaluator_test.go::TestEvaluator_NuGet_CriticalBugs`).
  Lifecycle: zero `Evaluator` → `SetNuGetClient` / `SetMavenClient` / ... in
  any order → `ensureRuleChain` builds rule list lazily → `EvaluateBatch`
  applies rules in priority order. Each chain mutation is a transition;
  the test bench exercises representative chain compositions per ecosystem.

### Error Guessing

Use experience to attack inputs that are likely to break parsers or stream
readers: malformed JSON, truncated streams, very large input, empty lines in
NDJSON, embedded null bytes, UTF-8 boundary cases. Mandatory for any code path
that parses external content (HTTP response, file content, manifest).

- Repo pin: `internal/infrastructure/depparser/gomod/parser.go` and its
  fuzz target at `internal/infrastructure/depparser/gomod/fuzz_test.go`.
  go.mod content is external attacker-influenceable input that we parse; the
  fuzz target exercises malformed / truncated / boundary inputs that
  example-based parser tests would not enumerate.

## Test-suite shape

### AAA (Arrange / Act / Assert)

Inside each table-driven case, make the structure explicit: Arrange = struct
fields, Act = the call under test, Assert = exact value comparison (see
`testing-performance.md` "assert exact values").

### FIRST

Fast / Independent / Repeatable / Self-validating / Timely. The first four are
non-negotiable. Timely (TDD-derived) is a preference here: post-hoc test
backfill is acceptable but new feature PRs must not leave the test gap to be
filled "later."

### Test Pyramid (DDD layer placement)

- **unit** (fast, no I/O, no network):
  - `internal/domain/**` — pure types, value objects, rule evaluation,
    validators. Most QA techniques above apply here.
  - State-bearing domain types (lifecycle enums with rank-based promotion,
    aggregate root invariants) get State Transition coverage.
  - Pure helper functions inside `internal/infrastructure/**` (parsers,
    classifiers, formatters that don't touch a client) — Equivalence
    Partitioning + Decision Table. Existing examples in `eolevaluator/helpers.go`
    and `common/purl/`.
- **infrastructure integration**:
  - `internal/infrastructure/<client>/` with fake HTTP transports or stubbed
    clients (`httpclient` package provides the shared transport seam). The
    HTTP-shape contract (request method, path, headers) and the
    response-parsing path are exercised together.
  - Avoid duplicating the unit tests of a helper at this layer. Integration
    tests should exercise wiring, not re-test parsing logic that already has
    unit coverage.
- **e2e**: `internal/interfaces/cli/` (CLI subcommand entry points). Keep
  small; end-to-end is expensive.

## Generative testing

Example-based tests cannot cover "the broad input space" or "this invariant
holds for all inputs of shape X." Generative testing fills that gap. Pre-PR,
ask: is there a parser, normalizer, or invariant in this PR that would
benefit?

### Native fuzzing (Go 1.18+ stdlib `testing.F`) — **adopted**

Zero new dependency. Required for any code path that parses external content
(HTTP response bodies, manifest files, untrusted strings).

- Role: verify **safety properties** (no panic, no hang) under coverage-guided
  mutation. Crash regressions are auto-saved under `<package>/testdata/fuzz/<TargetName>/`.
- Adopted targets in this repo:
  - `internal/common/fuzz_test.go`
  - `internal/common/purl/fuzz_test.go`
  - `internal/infrastructure/depparser/cyclonedx/fuzz_test.go`
  - `internal/infrastructure/depparser/gomod/fuzz_test.go`
  - `internal/infrastructure/eolevaluator/fuzz_test.go`
  - `internal/infrastructure/eoltext/fuzz_test.go`
  - `internal/infrastructure/golangresolve/fuzz_test.go`
  - `internal/interfaces/cli/fuzz_test.go`
- Future candidates: add a fuzz target whenever a new parser or normalizer is
  introduced. Pin the symbol at implementation time, not pre-emptively, to
  avoid rule rot when symbols are renamed.

Corpus placement: Go's `testing.F` requires `<package>/testdata/fuzz/<TargetName>/`
(package-local). This is the single Go-tooling-mandated exception to project
convention; treat it as a known carve-out.

CI integration: short fuzz (`-fuzztime=30s` per target) on PR is appropriate;
long-running fuzz is manually triggered (potentially nightly in future).

### Property-based testing (`pgregory.net/rapid`) — **future candidate**

Not yet adopted in this repo (no `pgregory.net/rapid` in `go.mod`). Reserve for
when an explicit, articulatable domain property emerges that example-based
tests cannot cover well. Typical shapes that motivate PBT adoption:

- Round-trip identity: `Parse(Format(x)) == x` for a structured value type
- Partial-order monotonicity: classifier outputs preserve a documented order
  over inputs
- Merge invariant: combining two partial results yields a result with bounded
  properties (idempotent merge, associativity, etc.)

When adopted, the policy is:

- Use rapid only inside `internal/propertytests/<layer>/*_pbt_test.go`, gated
  by build tag `//go:build pbt`.
- Do not import rapid directly under `internal/domain/**` (Cross-layer rule).
- One adapter PBT imports only the single adapter under test; cross-adapter
  coupling that leaks into the test layer is a smell.

### Mutation testing — **defer**

Post-PR Copilot review and the project's own `/review-until-clean` Phase D
already catch weak-test patterns ("test passes when target absent," "assertion
loosened to fit observed output," etc.). Mutation testing's marginal value is
unclear until one quarter of fuzz + PBT usage produces empirical weak-test
counts that Phase D missed.

## Sources

- Vladimir Khorikov, *Unit Testing Principles, Practices, and Patterns* (Manning, 2021)
- Maurício Aniche, *Effective Software Testing* (Manning, 2022)
- Gerard Meszaros, *xUnit Test Patterns*
- Martin Fowler, *Mocks Aren't Stubs* (https://martinfowler.com/articles/mocksArentStubs.html)
- Roy Osherove & Vladimir Khorikov, *The Art of Unit Testing*, 3rd ed. (Manning, 2023)
- ISTQB Foundation Level Syllabus
- Google Testing Blog, "Test Sizes" (https://testing.googleblog.com/2010/12/test-sizes.html)
- Kent C. Dodds, "Testing Trophy" (https://kentcdodds.com/blog/the-testing-trophy-and-testing-classifications)
- Go Fuzzing official docs (https://go.dev/doc/security/fuzz/)
- pgregory.net/rapid (https://pkg.go.dev/pgregory.net/rapid)

## Related

- **How to write Go tests** (mechanical lens): `testing-performance.md` (footguns,
  `t.Parallel`, hermetic subprocess, fixture layout).
- **Patterns caught post-PR by Copilot reviews**: `copilot-learned-coding.md`.
- **DDD layer constraints**: `ddd-architecture.md`.
