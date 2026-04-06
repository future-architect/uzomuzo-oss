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

### 5. Never Silently Discard Errors

When intentionally ignoring an error, use a comment explaining why:

- **GOOD:** `_ = f.Close() // best-effort cleanup, original error preserved`
- **BAD:** `_, _ = sqlDB.Exec(migrationSQL)`

If the error matters only in some cases, handle the expected case and propagate the rest:

```go
if _, err := db.Exec(alterSQL); err != nil {
    if !strings.Contains(err.Error(), "duplicate column name") {
        return fmt.Errorf("migration failed: %w", err)
    }
    // Column already exists — expected on subsequent runs.
}
```

### 6. Integrate with Structured Logging (`slog`)

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

## Learned from Copilot Reviews

- **Preserve Typed Error Identity When Wrapping**: When a function returns a domain-specific error type (e.g., `AuthenticationError`), wrap it using the matching constructor — not a generic one (e.g., `NewFetchError`). Mismatched wrappers break `errors.As` checks downstream.
- **Check All Error Returns in Tests**: Do not discard errors from stdlib functions (e.g., `os.Pipe()`, `os.CreateTemp()`) in test helpers with `_`. If the call fails, subsequent code will panic with a confusing nil-pointer error. Always check and `t.Fatalf` on failure.
- **Avoid Redundant Context in Error Wrapping Chains**: When wrapping an error that already contains context (e.g., `"invalid --fail-on label ..."`), use a different wrapper phrase (e.g., `"parse fail policy: %w"`) instead of repeating the same prefix. Redundant wrapping produces confusing messages like `"invalid --fail-on: invalid --fail-on label ..."`.
- **Distinguish "Not Found" from Other I/O Errors**: When reading a file that may not exist (e.g., auto-detecting `go.mod`), check `errors.Is(err, os.ErrNotExist)` for the "not found" case and return a wrapped error for other failures (permission denied, transient I/O). Treating all read errors as "not found" hides the real cause from users.
- **Surface Initialization Errors Instead of Silent Degradation**: When constructing objects with fallible initialization steps (e.g., compiling queries, loading configs), capture and log errors immediately rather than discarding them with `_`. Silent failures cause difficult-to-diagnose degradation at runtime. Use a shared initialization function that logs warnings, or return an error from the constructor.
