<!-- Generated from .github/instructions/error-handling.instructions.md — DO NOT EDIT DIRECTLY -->

# Error Handling

Prioritize robust error handling. Use early returns (guard clauses).

- **NEVER** hardcode sensitive information (passwords, API keys, etc.). Manage them via environment variables or a secure secret management system.
- Continuously verify that changes do not compromise data or introduce new vulnerabilities.

## Go Coding Conventions: Error Handling

Strictly adhere to the following modern best practices for error handling.

### 1. Always Add Context to Errors with `%w`

When returning an error, **always** wrap it with `fmt.Errorf` and `%w` to add context.

- **GOOD:** `return fmt.Errorf("failed to read config '%s': %w", path, err)`
- **BAD:** `return err`

### 2. Use `errors.Is` and `errors.As` for Error Inspection

- **`errors.Is`**: To check for a specific sentinel error (e.g., `errors.Is(err, io.EOF)`).
- **`errors.As`**: To check for a specific error type (e.g., `errors.As(err, &pathErr)`).

### 3. Use `errors.Join` to Combine Multiple Errors

Use `errors.Join()` (Go 1.20+) to combine errors from multiple independent operations.

### 4. Use `panic` Only for Unrecoverable Situations

Only use `panic` for truly exceptional, unrecoverable situations (e.g., programmer errors like nil pointer dereference, or failed startup conditions). Do not use it for normal error control flow.

### 5. Integrate with Structured Logging (`slog`)

When logging errors (Go 1.21+), use `log/slog`.

- Log the error object itself under an "error" key.
- Include relevant context as structured key-value pairs.

```go
slog.Error(
    "failed to update user profile",
    "error", err,
    "user_id", userID,
)
```
