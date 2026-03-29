# Contributing to uzomuzo

Thank you for your interest in contributing to uzomuzo! This project is a Go CLI tool for OSS supply chain lifecycle governance. Contributions of all kinds are welcome: bug reports, feature requests, documentation improvements, and code changes.

## Getting Started

### Prerequisites

- **Go 1.25+** (`go.mod` currently requires Go 1.25.0; Go 1.26.1 is recommended for local development)
- **goimports** (`go install golang.org/x/tools/cmd/goimports@latest`)
- **golangci-lint** ([installation guide](https://golangci-lint.run/welcome/install/))
- A GitHub account

### Clone and Build

```bash
git clone https://github.com/future-architect/uzomuzo-oss.git
cd uzomuzo-oss
cp config.template.env .env   # configure environment variables as needed
go build -o uzomuzo .
```

### Run Tests

```bash
go test ./...
```

### Lint

```bash
goimports -w . && golangci-lint run
```

## Development Workflow

1. **Fork** the repository on GitHub.
2. **Clone** your fork locally.
3. **Create a branch** from `main` with a descriptive name (e.g., `feat/add-sbom-export`, `fix/license-parsing`).
4. **Make your changes**, following the coding standards below.
5. **Run tests**: `go test ./...`
6. **Run linter**: `goimports -w . && golangci-lint run`
7. **Commit** your changes using conventional commit messages (see below).
8. **Push** your branch and open a pull request against `main`.

## Coding Standards

This project follows Domain-Driven Design (DDD) with strict layer boundaries:

```
Interfaces -> Application -> Domain <- Infrastructure
```

For full details, see:

- [DDD Architecture](docs/data-flow.md) and `.claude/rules/ddd-architecture.md`
- [Coding Standards](.claude/rules/coding-standards.md)
- [Development Guide](docs/development.md)

Key points:

- All code must be formatted with `goimports`.
- All exported identifiers must have godoc comments.
- Follow the existing patterns in the codebase.
- English only for source code, comments, and documentation.

## Commit Message Format

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>: <description>

<optional body>
```

**Types:**

| Type | Description |
|------|-------------|
| `feat` | New feature |
| `fix` | Bug fix |
| `refactor` | Code restructuring (no behavior change) |
| `docs` | Documentation changes |
| `test` | Adding or updating tests |
| `chore` | Maintenance tasks |
| `perf` | Performance improvements |
| `ci` | CI/CD changes |

Examples:

```
feat: add CycloneDX SBOM export
fix: resolve incorrect license mapping for dual-licensed packages
docs: update library usage guide
```

## Pull Request Guidelines

- Keep PRs focused on a single concern.
- Include a clear description of what the PR does and why.
- Reference related issues (e.g., `Closes #123`).
- Ensure all tests pass and the linter reports no issues.
- Add tests for new functionality.
- Do not push directly to `main`; all changes go through pull requests.

## Reporting Bugs

Please [open an issue](https://github.com/future-architect/uzomuzo-oss/issues/new) with:

- A clear, descriptive title.
- Steps to reproduce the bug.
- Expected vs. actual behavior.
- Go version, OS, and any relevant environment details.

## Feature Requests

Please [open an issue](https://github.com/future-architect/uzomuzo-oss/issues/new) describing:

- The problem you want to solve.
- Your proposed solution (if any).
- Any alternatives you considered.

## Code of Conduct

We are committed to providing a welcoming and inclusive environment. Please be respectful and constructive in all interactions. Harassment, discrimination, and disrespectful behavior will not be tolerated.

## License

This project is licensed under the [Apache License 2.0](LICENSE). By contributing, you agree that your contributions will be licensed under the same terms.
