# Changelog

All notable changes to Forge are documented here. The format follows Keep a Changelog, and releases use semantic versioning.

## [1.0.0] - 2026-11-14

### Added

- Pure-Go multi-model coding harness with no third-party dependencies.
- Goal-driven loop with architect, designer, implementer, tester, benchmarker, fixer, triage, security, reviewer, and release stages.
- Deterministic machine gates for install, lint, typecheck, build, tests, coverage, benchmarks, secret scanning, and external security scanners.
- Independent review and security report contracts under `.forge/`.
- Anti-tamper fingerprinting for acceptance criteria, commands, models, budget, loop, and release settings.
- Model routing with per-stage fallbacks, availability detection, pricing, token accounting, and budgets.
- Embedded JavaScript `cli` and `log-aggregator` starter templates.
- `init`, `run`, `gates`, `status`, `doctor`, `models`, `version`, and `help` commands.
- File-based run artifacts, prompt archives, event logs, command logs, and markdown/JSON reports.
- Self-hosting gate configuration with a 70% coverage floor and parser benchmark threshold.
- Detailed installation, configuration, usage, troubleshooting, and development documentation.
- Line-scoped `forge:allow-secret` marker for intentional secret-scan test fixtures.
- Go coverage parsing that prefers the `total: (statements)` summary line.

### Validated

- Go unit and integration suite passes across all packages.
- Self-gating run passes lint, build, tests, coverage, benchmarks, and secret/scanner checks.
- Generated JavaScript workspaces initialize with the expected dotfiles and pass their own gates.
