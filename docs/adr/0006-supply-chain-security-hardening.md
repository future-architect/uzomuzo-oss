# ADR-0006: Supply Chain Security Hardening for CI/CD and Releases

## Status

Accepted (2026-03-29)

## Context

The project aimed to improve its OpenSSF Scorecard rating and overall supply chain security posture. Several areas were identified as gaps (ref #63):

1. **Release artifact integrity**: GoReleaser produced unsigned binaries. Users had no way to verify that release artifacts were built by the project's CI and not tampered with.
2. **Workflow token permissions**: Multiple GitHub Actions workflows used default token permissions (read+write on all scopes), violating the principle of least privilege. A compromised action could access resources beyond what was needed.
3. **Fork PR code execution**: The `claude.yml` workflow (AI-assisted code review) would check out and potentially execute code from fork PRs, enabling secret exfiltration via untrusted code.
4. **Unpinned dependencies in CI**: `curl | sh` style installations (e.g., golangci-lint) allowed supply chain attacks via compromised install scripts.

These gaps were addressed across three PRs (#68, #70, #72) as a coordinated security hardening initiative.

## Decision

### 1. Cosign/Sigstore keyless signing for releases (PR #70)

- Add `sigstore/cosign-installer` (SHA-pinned) to the release workflow
- After GoReleaser builds artifacts, `cosign sign-blob` signs `checksums.txt` using GitHub Actions OIDC identity (keyless — no secret management needed)
- Signature (`.sig`) and certificate (`.pem`) are uploaded alongside release artifacts
- Add `id-token: write` permission for Sigstore OIDC token exchange

**Why keyless**: Traditional signing requires managing and rotating secret keys. Sigstore's keyless flow uses the CI's OIDC identity as the signing credential, tying artifact provenance to the specific GitHub Actions run. No secrets to leak, no keys to rotate.

### 2. Least-privilege workflow token permissions (PR #68)

- Add `permissions: {}` (deny-all) at the top level of `claude.yml` and `copilot-review-fix.yml`, with scoped permissions at the job level
- Restrict `release.yml` top-level to `contents: read`, moving `contents: write` to the goreleaser job only
- Each job declares only the permissions it actually needs

### 3. Fork PR rejection and input hardening (PR #72)

- Add `isCrossRepository` check in `claude.yml` before code checkout — fork PRs are rejected with a clear message
- Restrict trigger to PR comments only (not issue comments) to prevent unintended execution
- Replace `curl | sh` golangci-lint installation with SHA-pinned `golangci-lint-action@v7.0.1`

## Consequences

### Positive

- **Verifiable releases**: Users can run `cosign verify-blob` to cryptographically verify artifact provenance back to a specific GitHub Actions run.
- **Blast radius reduction**: A compromised GitHub Action in any workflow can only access the permissions explicitly granted to that job, not the default read+write scope.
- **Fork safety**: Untrusted code from fork PRs cannot access repository secrets via the AI review workflow.
- **Scorecard improvement**: Token-Permissions, Signed-Releases, and Pinned-Dependencies checks all improve.

### Negative

- **Verification friction**: Users must install cosign and run a verification command to check signatures. Mitigated by documenting the exact command in release notes.
- **Maintenance overhead**: SHA-pinned dependencies require manual updates (or Renovate/Dependabot PRs) when new versions are released. This is an intentional trade-off — pinning prevents supply chain attacks at the cost of update friction.
- **Fork contributor experience**: Fork PR authors cannot trigger AI review via `@claude` comments. They must wait for a maintainer to trigger it. This is the intended security boundary.

### Neutral

- These changes are CI-only — no impact on application code, domain logic, or user-facing CLI behavior.
- Renovate is configured to propose updates for SHA-pinned Actions (PR #65), so the maintenance overhead is partially automated.
