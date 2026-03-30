# Language Policy

## Source Code — English Only

All executable / compilable / testable assets MUST be written in **English**:

- Source code (functions, types, variables, comments)
- Error messages & log messages
- User-facing CLI output text
- Test code & fixtures (except binary/golden files)

## Documentation — English Only

All project documentation is written in **English**:

- `README.md` is the canonical project overview (root)
- `docs/*.md` contains detailed reference documentation

Constraints:

1. `README.md` and `docs/*.md` are the canonical documentation.
2. When updating documentation, update the English files directly.

## Prohibited Content

- Embedding Japanese text inside source code comments or identifiers.

## Rationale

Source code and documentation are both in English for tooling compatibility and global contributor accessibility.

## Copilot Response Language Policy

- **PR review comments**: Always in **English**. Automated PR reviews are public-facing project content and must follow the English-only documentation policy.
- **Chat / Inline Chat**: Respond in the same language as the user's question (Japanese → Japanese, English → English).
