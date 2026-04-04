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
- **Cover New Control Flow Branches with Tests**: When adding a new conditional branch (especially fallback paths or classification logic), add a targeted test case to the existing test suite that exercises the new path. New branches without test coverage are easy to regress silently.
- **Do Not Duplicate Tests Across Packages**: When a helper function is already tested in its own package (e.g., `common/links/depsdev_test.go`), do not add identical test cases in consumer packages (e.g., CLI). Test the helper once at the source; consumers should test their own integration logic.
- **Scope Test Assertions to the Region Under Test**: When asserting on CLI output or rendered text, use assertions specific to the output section being tested (e.g., check a specific table row, not the entire output). Broad substring checks (e.g., `Contains(output, emoji)`) can match unrelated sections and produce false-positive passes.
- **Populate All Semantically Implied Fields in Test Fixtures**: When constructing test data for a domain state (e.g., EOLScheduled), populate all fields that the state semantically implies (e.g., `ScheduledAt`). Missing fields in fixtures can mask bugs where code depends on the field being set.
