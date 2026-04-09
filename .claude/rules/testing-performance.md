<!-- Generated from .github/instructions/testing-performance.instructions.md — DO NOT EDIT DIRECTLY -->

# Testing & Performance

## Testing

- **Table-Driven Tests**: Use table-driven tests for testing multiple scenarios of a function.
- **Sub-tests**: Use `t.Run()` to create sub-tests for better isolation and clearer output.
- **Test Coverage**: Strive for high test coverage, especially for business-critical logic.

## Performance Considerations

While correctness and clarity come first, performance is critical in many parts of our application.

- **Pre-allocate Slices and Maps**: When the size is known, pre-allocate capacity with `make` to avoid repeated allocations.
- **Be Mindful of Pointer vs. Value Semantics**: Consider using pointers for large structs to avoid expensive copies, but don't default to them unnecessarily.
- **Write Benchmarks for Hot Spots**: Do not optimize prematurely. Use the `testing` package to benchmark performance-critical functions and prove an optimization is needed.

## Concurrency

- **Context Propagation**: Functions that may block (I/O, etc.) MUST accept a `ctx context.Context` as their first argument.
- **Goroutine Lifetime**: Ensure every goroutine has a clear exit condition to avoid leaks. Use `sync.WaitGroup` to wait for goroutines to finish.
- **Race Conditions**: Protect shared memory with mutexes. Be mindful of data races and test with the `-race` flag.

## Learned from Copilot Reviews

- **Port Tests When Replacing Services**: When replacing or refactoring an application service, port all existing unit tests to the new service. Untested replacement code silently loses coverage that the old tests provided.
- **No Permanently Skipped Tests**: Do not commit tests with `t.Skip()` that have no plan for implementation. Skipped tests create a false sense of coverage and accumulate as dead code. Either implement the test (e.g., by introducing a test seam or mock) or remove it entirely.
- **Propagate Build Tags to Test Files**: When a package uses build tags (e.g., `//go:build cgo`), test files that import it must carry the same tag — otherwise `CGO_ENABLED=0` or other constrained builds fail to compile the test.
- **Capture and Assert Mock/Fake Arguments**: When using test fakes or mocks, capture the arguments passed to them and assert correctness — unconditional return values let tests pass even when input parsing or encoding is wrong.
- **Accept Interfaces in Test-Setter Methods**: When providing `SetXxxClient`-style methods for test injection, accept the interface type (not the concrete type) so test fakes can be injected via the public API without accessing unexported fields.
- **Exercise Non-Nil Return Paths in Tests**: When a function returns `nil` to signal "unavailable" vs an empty collection to signal "no matches", ensure tests include at least one matching item so the result is non-nil and the test exercises actual behavior — not a vacuous early return.
- **Match Test Fixture IDs to Their Mapped Values**: When test fixtures map one identifier to another (e.g., PURL to import path), ensure each pair is consistent — mismatched pairs make tests confusing and hide incorrect mappings.
- **Cover New Control Flow Branches with Tests**: When adding a new conditional branch (especially fallback paths or classification logic), add a targeted test case to the existing test suite that exercises the new path. New branches without test coverage are easy to regress silently.
- **Explicit Subtest Names for Empty Inputs**: When table-driven test inputs include empty strings or values that produce empty `t.Run` names, add a separate `name` field to the test struct and use it for `t.Run`. Empty subtest names make failures harder to identify and debug.
- **Scope Test Assertions to Specific Output Regions**: When testing output that contains multiple sections (e.g., summary box + detail table), scope assertions to the specific region under test — broad `strings.Contains` checks can match unrelated sections and mask bugs.
- **Avoid Duplicate Test Coverage Across Packages**: Do not duplicate unit tests in a consumer package when the function under test already has comprehensive coverage in its own package. Cross-package duplication increases maintenance cost and can drift out of sync.
- **Use `filepath.Join` for Temp File Paths in Tests**: When constructing temporary file paths in tests, use `filepath.Join(t.TempDir(), "filename")` instead of string concatenation with `"/"`. String concatenation is not portable across OS/path conventions and is inconsistent with `filepath`-based path construction used elsewhere in the codebase.
- **Use Bounded Waits in Test Poll Loops**: When polling for a condition in tests (e.g., waiting for a server to respond), use `time.After` with a deadline and `time.Sleep` for backoff between attempts — never spin in a tight loop. Unbounded polling burns CPU and can cause CI hangs.
- **Never Call `t.Fatal` from Non-Test Goroutines**: `t.Fatal` and `t.Fatalf` must only be called from the test goroutine. In `httptest` handlers or other goroutines, precompute test data outside the handler or use channels to signal errors back to the test goroutine.
- **Read `os.Pipe` Concurrently When Capturing Output**: When redirecting `os.Stdout` to an `os.Pipe` to capture output in tests, start a goroutine to read from the pipe before the function under test runs. Reading only after completion can deadlock if output exceeds the OS pipe buffer size.
- **Mirror Production JSON Tags in Test Validation Structs**: When defining test structs to unmarshal command output (JSON, CSV), ensure the struct's field tags exactly match the production output schema. Mismatched JSON tags silently leave fields at their zero value, masking regressions that would otherwise be caught by assertions.
- **Use `t.Cleanup` When Replacing Process-Global State**: When a test replaces process-global state (`os.Stdin`, `os.Stdout`, `os.Stderr`), register `t.Cleanup` immediately after the replacement to guarantee restoration — even if a later `t.Fatalf` exits early. Also close pipe readers/writers in the cleanup to prevent file descriptor leaks.
- **Assert Exact Computed Values, Not Just Thresholds**: When testing functions that produce computed numeric results (scores, percentages, ratios), assert the exact expected value (with a small tolerance for floats) rather than only checking threshold boundaries. Threshold-only assertions miss formula regressions that produce different-but-still-passing values.
- **Omit Unused Struct Fields in Test Fixtures**: When constructing struct literals for test fixtures, only populate fields that the test actually exercises. Including unused fields (especially those with nondeterministic values like timestamps or random IDs) can introduce flaky tests and unnecessary import dependencies.
- **Assert All Output Fields When Extending Structs**: When adding new fields to an output struct (JSON, CSV, domain model), add corresponding assertions in existing tests for those fields — including ordering guarantees for slices. Untested pass-through fields can silently regress without detection.
