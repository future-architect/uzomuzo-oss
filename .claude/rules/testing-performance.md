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
