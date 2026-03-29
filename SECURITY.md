# Security Policy

## Supported Versions

| Version | Supported |
| ------- | --------- |
| latest  | Yes       |

## Reporting a Vulnerability

If you discover a security vulnerability in this project, please report it responsibly.

**Please do NOT open a public GitHub issue for security vulnerabilities.**

Instead, please report them via email:

- Email: security@future.co.jp
- Subject: [uzomuzo-oss] Security Vulnerability Report

### What to include

- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

### Response Timeline

- We will acknowledge receipt within 3 business days
- We aim to provide a fix within 30 days for critical issues
- We will coordinate disclosure with you

## Security Best Practices

When using uzomuzo in your projects:

1. Always use the latest version
2. Set `GITHUB_TOKEN` with minimal required permissions (read-only for public repos)
3. Do not expose API tokens in CI logs — use masked secrets
4. Review the SBOM/dependency list regularly using uzomuzo's audit command
