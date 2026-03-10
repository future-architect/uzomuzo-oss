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

**GitHub Copilot Chat and Inline Chat responses must be in the same language as the user's question.**

- **If the user asks in Japanese**: Respond in Japanese
- **If the user asks in English**: Respond in English
- **Match the user's communication language**: Always adapt to the language used in the user's question or comment

This ensures natural communication and avoids language confusion during development.
