# Security Policy

## Supported Versions

uzomuzo is currently in pre-1.0 development. Security updates are applied only to the latest commit on the `main` branch; older tagged releases are not maintained.

| Version                     | Supported |
| --------------------------- | --------- |
| Main branch (latest commit) | Yes       |
| Tagged releases (pre-1.0)  | No        |

## Reporting a Vulnerability

**Do NOT open a public GitHub issue for security vulnerabilities.**

Please use GitHub's private vulnerability reporting feature:

1. Navigate to the [Security tab](https://github.com/future-architect/uzomuzo-oss/security) of this repository.
2. Click **"Report a vulnerability"**.
3. Fill in the details as described below.

### What to Include

- A clear description of the vulnerability.
- Steps to reproduce the issue.
- Impact assessment (e.g., severity, affected functionality).
- Affected versions (or commit SHAs).
- Any suggested fixes or mitigations, if applicable.

### Response Timeline

- **Acknowledgment**: Within 48 hours of the report.
- **Initial assessment and timeline**: Within 1 week.
- **Fix and disclosure**: Determined on a case-by-case basis depending on severity.

## Disclosure Policy

We follow a **coordinated disclosure** approach:

1. The reporter submits a vulnerability via GitHub's private reporting.
2. We acknowledge and assess the report.
3. We develop and test a fix.
4. We release the fix and publish a security advisory.
5. The reporter is credited in the advisory (unless they prefer to remain anonymous).

We ask reporters to refrain from public disclosure until a fix is available, and we commit to resolving confirmed vulnerabilities promptly.

## Scope

### In Scope

- The `uzomuzo` CLI tool.
- The public library API (`pkg/uzomuzo`).
- CI/CD workflows defined in this repository (`.github/workflows/`).
- Configuration handling and secret management.

### Out of Scope

- Vulnerabilities in third-party dependencies (please report these to the respective upstream projects).
- Infrastructure or hosting environments not managed by this repository.
- Social engineering attacks against maintainers or contributors.

## License

This project is licensed under the [Apache License 2.0](LICENSE).
