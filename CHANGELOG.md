# Changelog

All notable changes to Forge are documented here. The format follows Keep a Changelog, and releases use semantic versioning.

## [1.2.0] - 2026-11-16

### Removed

- Embedded JavaScript `cli` and `log-aggregator` starter templates.
- `--template cli` and `--template log-aggregator`. `--template none` remains accepted and is the default.

### Changed

- `forge init` now writes control files only (config, goal, schema, `AGENTS.md`, `.gitignore`) and never scaffolds application code. Use your own project tooling to create the project.
- `forge init` defaults to `none`; omitting `--template` no longer generates a JavaScript project.
- A new workspace requires `--goal`, since there is no template goal to fall back on. `--force` without `--goal` preserves the existing goal.
- Init output no longer reports template files copied.
- README, CLI help, and self-hosting docs describe bring-your-own-project usage.
- Version bumped to 1.2.0.

### Validated

- Go unit and integration suite passes across all packages.
- Self-gating run passes lint, build, tests, coverage, benchmarks, and secret/scanner checks.

## [1.1.0] - 2026-11-16

### Added

- Language- and project-agnostic command detection: Node.js (npm, pnpm, Yarn, Bun), Deno, Go, Rust, Python (pip, uv, Poetry, Pipenv, tox), Java/Kotlin (Maven, Gradle), .NET, Ruby, PHP, C/C++ (CMake, Meson, Autotools, vcpkg), Swift, Dart/Flutter, Elixir, Scala, Haskell (Cabal, Stack), Zig, Lua, R, and Julia.
- Task-runner and script fallbacks for `Makefile`, `justfile`, `Taskfile.yml`, Bazel, Nix, and executable `test.sh`/`build.sh`/`coverage.sh`-style scripts, including Windows wrappers.
- Multi-ecosystem projects: Forge chooses the primary ecosystem from source-file counts and fills missing gates from other detected ecosystems, task runners, and scripts. Explicit `commands` remain authoritative.
- Source-only projects get a language profile without invented commands.
- Coverage parsing for Cobertura XML, JaCoCo XML, LCOV, Go coverage profiles, and additional text summaries (PHPUnit, SimpleCov, Elixir, tarpaulin, and others).
- Auto-detected security scanners for pnpm, Yarn, Composer, Bundler, .NET, and OSV, with parsers for each output format. Explicit scanners with the same name replace auto-detected ones.
- Generic project facts in run context: manifests with previews, languages, package manager, task runners, and detected commands, replacing Node/Go/Rust/Python-specific fields.
- Release tagging reads the project version from package manifests across ecosystems, not only `package.json`.
- Ecosystem-neutral prompts, write scopes, generated agent instructions, `forge init` output, and `forge doctor` project profile.
- Extensive detection, coverage-format, security-parser, context, prompt, and template tests.

### Changed

- `forge init` and `forge doctor` report a combined language and package-manager profile instead of assuming Node.js.
- Generated `AGENTS.md` records the detected project profile and resolved commands, while the bundled `cli` and `log-aggregator` starters remain JavaScript.
- `forge.schema.json` documents auto-detection, the expanded scanner formats, and explicit-command precedence.

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
