# uzomuzo-diet Findings

Accumulated insights from diet-trial and diet-fuzz runs.

## 2026-04-08 — diet-fuzz run (20 projects × syft, 4 languages)

### SBOM Tool Limitations

- **syft cannot resolve Python projects using only `pyproject.toml`**: Projects without `requirements.txt`, `Pipfile.lock`, or `poetry.lock` produce SBOMs with 0 dependency graph edges. Diet fails with `SBOM has no dependency graph`. Affected: psf/black, encode/httpx, pypa/pip (3/5 Python projects). Workaround: use trivy or cdxgen, or ensure lockfiles exist.

### Call Site Detection — Resolved Patterns

The following patterns were fixed by #238 and #239, confirmed by before/after comparison (TypeScript IBNC: 58 → 25, -57%):

- **Default import + constructor** (`import Foo from 'foo'; new Foo()`) — fixed by #239. Evidence: dockerode in drizzle-orm (31 files, was 0 calls → now detected).
- **JSX component usage** (`<Component />`) — fixed by #238. Evidence: @heroicons/react (5 files), react-icons (4 files) in trpc now detected.
- **Named/destructured imports** — fixed by #239. Evidence: @opentelemetry/context-async-hooks (15 files) in prisma now detected.

### Call Site Detection — Remaining Known Limitations

These IMPORTS-BUT-NO-CALLS patterns are **expected behavior**, not bugs. They should be excluded from anomaly counts or documented as known limitations:

- **Config-driven tools** (14 cases): eslint plugins (`eslint-plugin-markdown`, `eslint-plugin-unicorn`, etc.), tailwindcss plugins, babel plugins. These are referenced in config files (`.eslintrc`, `tailwind.config.js`), not imported in source code.
- **Side-effect imports** (6 cases): `@testing-library/jest-dom`, `jest-extended`, `isomorphic-fetch`, `jest-serializer-ansi-escapes`. The `import 'foo'` pattern augments globals; there are no explicit API calls to detect.
- **CLI tools used as binaries** (8 cases): `wrangler` (×4 versions), `jest`, `ava`, `zx`, `esbuild-register`. These are invoked as CLI commands, not imported in application code.
- **Python conditional/try-except imports** (1 case): `cryptography` in flask — `try: import cryptography` used for feature detection. The import itself is the usage. Filed as #243.

### Cross-Language Observations

- **Go is the most stable language for diet analysis**: 0 anomalies across 5 projects (cli/cli, fzf, cobra, bubbletea, lazygit). go.mod-based resolution is mature and reliable.
- **TypeScript monorepos produce the most IBNC**: Average 5 IBNC per project vs 0 for Go. Monorepo patterns (re-exports, workspace dependencies, multi-package imports) create more edge cases for static analysis.
- **JavaScript projects have fewer direct deps in syft SBOMs**: express (1 direct), axios (1 direct). syft's CycloneDX output treats the root package as the only "direct" dependency when the lockfile doesn't distinguish dev vs prod well.
- **EOL/Archived deps are most common in TypeScript/JavaScript**: 9 EOL deps across 5 TS projects, 3 across 5 JS projects, vs 1 for Go and 0 for Python. The npm ecosystem's rapid package churn and frequent deprecations contribute.

### Tool Comparison Notes

- **syft vs trivy component counts differ**: syft finds more GitHub Actions components. For the same project, syft may report more total components but similar dependency graph depth.
- **syft includes devDependencies by default**: Unlike trivy which excludes them. This means syft SBOMs have more components and potentially more IBNC (dev-only deps like test utilities are included).
