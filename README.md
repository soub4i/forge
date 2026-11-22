# Forge

![Forge gates live progress](docs/forge-gates.gif)

[![CI](https://github.com/soub4i/forge/actions/workflows/ci.yml/badge.svg)](https://github.com/soub4i/forge/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/soub4i/forge?sort=semver)](https://github.com/soub4i/forge/releases)
[![Go](https://img.shields.io/badge/Go-1.23%2B-00ADD8)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Forge is a gate-driven, language-agnostic coding harness. Give it a goal and a workspace; it plans, implements, tests, benchmarks, reviews, and releases in bounded loops until machine-checked gates and an independent reviewer agree.

Forge is a single Go binary with no third-party Go dependencies. It adds control files to an existing project, detects native commands, and never scaffolds application code or imposes a framework. Explicit configuration always wins; detection fills the gaps.

## Contents

- [Why Forge](#why-forge)
- [Install](#install)
- [Quick start](#quick-start)
- [How it works](#how-it-works)
- [Machine gates](#machine-gates)
- [Model routing](#model-routing)
- [Configuration](#configuration)
- [CLI reference](#cli-reference)
- [Artifacts](#artifacts)
- [Development](#development)
- [Contributing](#contributing)
- [Troubleshooting](#troubleshooting)
- [Limitations](#limitations)
- [License](#license)

## Why Forge

Most coding agents stop when the model says it is done. Forge stops when the contract passes:

- tests, coverage, lint, typecheck, and build commands succeed
- benchmark metrics meet configured thresholds
- the built-in secret scan is clean and external scanners report nothing above the allowed severity
- an independent reviewer, which did not write the code, approves with no blocking findings
- the acceptance configuration was not weakened while the loop was running

When a check fails, Forge writes a bounded feedback file, repairs, escalates to a stronger model when it is stuck, and tries again until the budget or iteration limit is exhausted. The model is an implementation detail; the gates are the contract.

Forge is designed for open, reproducible workflows:

- **Bring your own project.** Any language, framework, or build system is welcome.
- **Bring your own models.** Routing is per stage and configurable through OpenCode.
- **Keep the evidence.** Prompts, events, command logs, reports, and costs are written under `.forge/`.
- **Keep control.** Budgets, write scopes, config fingerprints, and checkpoints bound what an agent can do.

## Install

### Requirements

- **Go 1.23+** to build or install from source
- The [`opencode` CLI](https://opencode.ai/) on `PATH`, authenticated with at least one model
- **Git** (optional) for repository context, checkpoints, and release tags
- The native toolchain for the project you want to build

Forge builds and runs with the Go standard library only. It does not need a database, daemon, or API-key manager.

### From source

```sh
go install github.com/soub4i/forge/cmd/forge@latest
forge version
```

If `forge` is not found after installation, add Go's bin directory to `PATH`:

```sh
export PATH="$PATH:$(go env GOPATH)/bin"
```

From a checkout:

```sh
go build -o forge ./cmd/forge   # local ./forge binary
go install ./cmd/forge          # install into $GOBIN or $HOME/go/bin
go run ./cmd/forge help         # run without installing
```

Published binaries for Linux, macOS, and Windows are available on the [releases page](https://github.com/soub4i/forge/releases). Each release includes `checksums.txt`.

## Quick start

```sh
cd my-project

# 1. Add Forge control files to an existing project or an empty directory
forge init . --goal "Add structured logging without changing the public API"

# 2. Check OpenCode, model availability, commands, config, and git
forge doctor

# 3. Inspect the resolved plan without calling a model
forge run --dry-run

# 4. Run only the deterministic gates
forge gates

# 5. Run the full loop with a small budget for the first experiment
forge run --max-iterations 2 --budget 1

# 6. Inspect the result
forge status
forge status --report
```

`forge init` never scaffolds application code. It writes:

```text
my-project/
  forge.config.json        goal, acceptance criteria, commands, models, budget
  .forge/forge.schema.json JSON schema for the config
  docs/GOAL.md             machine-readable copy of the goal and acceptance
  AGENTS.md                project profile, resolved commands, and agent rules
  .gitignore               ignores .forge/, dependencies, coverage, and build output
```

Forge records detected commands in `forge.config.json`; edit or replace them at any time. If no command is detected, add a `Makefile` target, `just` recipe, `Taskfile` task, or conventional `test.sh`/`build.sh` script, or configure `commands` manually.

Existing `AGENTS.md` and `.gitignore` files are preserved. Forge only appends `.forge/` when a `.gitignore` already exists.

### Detected ecosystems

Detection is best-effort and additive. Forge scans manifests and source files, chooses a primary ecosystem from source-file counts when several are present, and fills missing gates from other ecosystems, task runners, and scripts.

| Project | Detected from |
| --- | --- |
| Node.js / Bun / Deno | `package.json`, `pnpm-lock.yaml`, `yarn.lock`, `bun.lockb`, `deno.json` |
| Go | `go.mod` (`go.work` files are recorded as manifests) |
| Rust | `Cargo.toml` |
| Python | `pyproject.toml`, `requirements.txt`, `setup.py`, `Pipfile`, `uv.lock`, `poetry.lock`, `tox.ini` |
| Java / Kotlin | `pom.xml`, `build.gradle`, `build.gradle.kts`, Maven/Gradle wrappers |
| .NET | `*.sln`, `*.csproj`, `*.fsproj`, `*.vbproj`, `global.json` |
| Ruby | `Gemfile`, `Rakefile`, `*.gemspec` |
| PHP | `composer.json`, PHPUnit/PHPStan/Psalm/phpcs configs |
| C / C++ | `CMakeLists.txt`, `meson.build`, `configure.ac`, `vcpkg.json` |
| Swift | `Package.swift` |
| Dart / Flutter | `pubspec.yaml` |
| Elixir | `mix.exs` |
| Scala | `build.sbt` |
| Haskell | `*.cabal`, `stack.yaml`, `cabal.project` |
| Zig | `build.zig` |
| Lua | `*.rockspec`, `.busted`, `.luacheckrc` |
| R | `DESCRIPTION` |
| Julia | `Project.toml` |
| Task runners / scripts | `Makefile`, `justfile`, `Taskfile.yml`, Bazel, Nix, executable `test.sh`/`build.sh`/etc. |

Projects with only source files still get a language profile, but Forge does not invent commands for them. Add a task runner, a conventional script, or explicit `commands`.

## How it works

One iteration runs these stages:

1. **Architect** writes `docs/ARCHITECTURE.md` and `docs/PLAN.md`.
2. **Designer** writes `docs/DESIGN.md` and refines the plan.
3. **Implementer** writes production code, tests, and scripts.
4. **Tester** strengthens tests and closes test gaps.
5. **Benchmarker** creates or repairs the benchmark when one is required.
6. **Machine gates** run install, lint, typecheck, build, test, coverage, benchmark, secret scan, and external scanners.
7. **Fixer** runs up to `loop.repairAttempts` times if deterministic gates fail.
8. **Security** performs an adversarial review and writes `.forge/security.json`.
9. **Reviewer** independently reviews the result and writes `.forge/review.json`.
10. **Release** writes `README.md`, `CHANGELOG.md`, and `docs/RELEASE.md`, and optionally creates a git tag.

Architecture and design are skipped on later iterations unless their documents are missing or a design change is requested. The benchmarker is skipped when benchmarks are not required or when a benchmark command already exists. This keeps repair iterations cheaper.

When a failure repeats `loop.escalateAfter` times, a **Triage** stage diagnoses the root cause and the implementer/fixer use the architect-tier model for that iteration. A reviewer can request a design change, in which case architecture and design are regenerated before implementation resumes.

Each stage runs in a fresh OpenCode session and hands work to the next stage through files. Write scopes are narrow: security, review, and triage are read-only for production code; release may only touch documentation and release metadata.

## Machine gates

`forge gates` makes no model calls and is the fastest way to check whether a workspace is releasable. It writes command logs and a summary under `.forge/gates/<timestamp>/`.

Commands come from `forge.config.json` first, then project detection, then task runners and scripts. A missing enabled command is a failure. `forge init` enables lint, typecheck, build, and coverage gates only when detection finds a matching command. Benchmarks are opt-in through acceptance thresholds. Tests are required by default.

The deterministic gates run in this order:

1. **Install** — when `commands.install` is configured or detected.
2. **Lint** — when `acceptance.lint` is true.
3. **Typecheck** — when `acceptance.typecheck` is true.
4. **Build** — when `acceptance.build` is true.
5. **Test** — when `acceptance.tests` is true.
6. **Coverage** — when `acceptance.coverage.min` is greater than zero.
7. **Benchmarks** — when `acceptance.benchmarks.required` is true.
8. **Secrets** — when `acceptance.security.enabled` is true.
9. **External scanners** — when configured or auto-detected.
10. **Independent review** — after the reviewer stage in a full run.

A command can be marked `optional: true` to turn a non-zero exit into a warning instead of a blocker.

### Coverage

Coverage is parsed from, in order:

- the configured `acceptance.coverage.file`
- `coverage/coverage-summary.json`, `coverage.json`, or `coverage/coverage-final.json`
- `coverage/lcov.info` or `lcov.info`
- `coverage.xml`, `coverage/cobertura-coverage.xml`, or `coverage/coverage.cobertura.xml`
- `target/site/jacoco/jacoco.xml`
- other discovered `coverage.xml`, `jacoco.xml`, `lcov.info`, and Cobertura files
- the coverage command's stdout
- `coverage.out` (Go profiles are computed from statement counts)

Supported formats include Istanbul JSON, Python coverage JSON, Cobertura XML, JaCoCo XML, LCOV, and text summaries from tools such as `go tool cover`, `nyc`, PHPUnit, SimpleCov, `mix test --cover`, and tarpaulin. For Go, the extractor prefers the `total: (statements) X%` line, so multi-package output is parsed correctly.

### Benchmarks

The benchmark command must exit successfully and report metrics in the configured results file (default `bench/results.json`) or as JSON on stdout. Metrics can be nested under `metrics` or supplied as objects with `name`/`metric`/`key` and `value`:

```json
{
  "metrics": {
    "lines_per_sec": 1250000,
    "p95_latency_ms": 3.2
  }
}
```

Thresholds support `min` and `max`. A missing metric fails the threshold. Units are labels only; Forge compares raw numbers.

### Security scanning

The built-in scanner checks source files for API keys, tokens, private keys, hard-coded credentials, and committed `.env` files, and redacts evidence in output. To exempt an intentional test fixture, put the marker on the same line as the value:

```go
fixture := `AKIAIOSFODNN7EXAMPLE` // forge:allow-secret
```

The marker is intentionally line-scoped. Prefer it over disabling a whole file.

External scanners are auto-detected when the tool and project marker exist:

- `npm audit`, `pnpm audit`, or `yarn audit` for Node.js
- `pip-audit` for Python
- `govulncheck` for Go
- `cargo audit` for Rust
- `composer audit` for PHP
- `bundle-audit` for Ruby
- `dotnet list package --vulnerable` for .NET
- `osv-scanner` when installed

Scanners can also be configured explicitly. An explicit scanner replaces an auto-detected scanner with the same name. Supported formats are `npm-audit`, `pnpm-audit`, `yarn-audit`, `pip-audit`, `composer-audit`, `bundler-audit`, `dotnet-vulnerable`, `osv`, `semgrep`, `trivy`, `gitleaks`, `govulncheck`, `json`, and `text`.

## Model routing

Models are routed by stage and capability tier. The shipped defaults are examples for the OpenCode CLI; every stage can point to any available provider and model. The tier is documentation and a routing hint, while `models.<stage>.model` is the actual choice.

| Stage | Tier | Default model |
| --- | --- | --- |
| Architect | reasoning | `opencode/big-pickle` |
| Designer | reasoning | `opencode/longcat-2.5-free` |
| Implementer | balanced | `opencode/deepseek-v4.1-flash-free` |
| Tester | balanced | `opencode/glm-5.3-flash-free` |
| Fixer | fast | `opencode/minimax-m3.1-flash-free` |
| Benchmarker | fast | `opencode/flint-40b-free` |
| Security | reasoning | `opencode/nemotron-3.5-super-vl-free` |
| Reviewer | reasoning | `opencode/big-pickle` |
| Release | fast | `opencode/inkling-2-small-free` |
| Triage | fast | `opencode/nemotron-3.5-lightning-free` |

Each stage has fallbacks. If the selected model is unavailable or a call fails, Forge tries the next fallback, bounded by `loop.modelRetries`. `forge models` shows routing, prices, and availability warnings; `forge doctor` flags stages whose primary model is unavailable.

Override a stage for one run:

```sh
forge run --model reviewer=provider/strong-model --model implementer=provider/cheap-model
```

Or set routing permanently in `forge.config.json`:

```json
{
  "models": {
    "architect": {
      "model": "anthropic/claude-opus-4",
      "tier": "reasoning",
      "fallbacks": ["anthropic/claude-sonnet-4"]
    },
    "implementer": {
      "model": "anthropic/claude-sonnet-4",
      "tier": "balanced",
      "fallbacks": ["opencode/deepseek-v4.1-flash-free"]
    }
  },
  "pricing": {
    "anthropic/claude-opus-4": { "input": 15, "output": 75 },
    "anthropic/claude-sonnet-4": { "input": 3, "output": 15 }
  }
}
```

Prices are USD per million tokens and feed the budget ledger and reports. Unpriced models are treated as zero cost; token usage is still recorded, but a dollar limit cannot be enforced precisely.

## Configuration

`forge init` writes `forge.config.json`. Forge searches for `forge.config.json`, `harness.config.json`, or `.forge/config.json`, walking upward from the workspace. A global config is merged first from `$XDG_CONFIG_HOME/forge/config.json` (or `~/.config/forge/config.json`).

A complete example:

```json
{
  "$schema": "./.forge/forge.schema.json",
  "version": 1,
  "goal": "Build a fast NDJSON log aggregator CLI",
  "acceptance": {
    "tests": true,
    "coverage": { "min": 80, "file": null },
    "lint": true,
    "typecheck": true,
    "build": true,
    "benchmarks": {
      "required": true,
      "resultsFile": "bench/results.json",
      "thresholds": [
        { "metric": "events_per_sec", "min": 50000, "unit": "events/sec" }
      ]
    },
    "security": {
      "enabled": true,
      "maxSeverity": "medium",
      "requireNoSecrets": true,
      "scanners": [],
      "ignore": []
    },
    "review": { "enabled": true, "maxBlockingFindings": 0 }
  },
  "commands": {
    "install": null,
    "test": "npm test",
    "coverage": "npm run test:coverage",
    "lint": "npm run lint",
    "typecheck": null,
    "build": null,
    "benchmark": "npm run bench"
  },
  "models": {},
  "pricing": {},
  "budget": { "maxUsd": 10, "maxMinutes": 180, "maxIterations": 8, "maxTokens": 0 },
  "loop": {
    "repairAttempts": 1,
    "escalateAfter": 2,
    "stageTimeoutMinutes": 20,
    "modelRetries": 1
  },
  "context": { "maxFiles": 250, "maxDocChars": 6000 },
  "git": { "checkpoint": false },
  "release": { "enabled": true, "tag": false }
}
```

Notes:

- `budget.maxUsd`, `maxMinutes`, `maxIterations`, and `maxTokens` of `0` mean unlimited.
- `commands` may be omitted; set a command to `null` to force detection for that gate.
- `acceptance.coverage` also accepts a number: `"coverage": 80`.
- `acceptance.benchmarks` also accepts an array of thresholds, or `false` to disable benchmarks.
- A command may be a string or `{ "command": "...", "timeoutMs": 600000, "optional": false }`.
- `acceptance.security.scanners` entries look like `{ "name": "semgrep", "command": "semgrep --json ...", "format": "semgrep", "required": false }`.
- `acceptance.security.ignore` accepts glob patterns. `vendor/**` matches a tree; `*.min.js` matches a basename.
- `git.checkpoint` commits after each iteration in a git repository. Forge excludes `.forge/` through `.git/info/exclude`.
- `release.enabled` requires the release stage to produce `README.md`, `CHANGELOG.md`, and `docs/RELEASE.md`; `release.tag` creates `v<version>` when a recognized manifest exposes a version.

Forge fingerprints acceptance, commands, models, budget, loop, and release settings at the start of a run and checks them after every model stage. If a model edits any of them, the iteration fails with a tamper message. Change the contract intentionally outside a run, then use `forge init --force` if you want to regenerate control files.

Environment overrides are applied after config files:

```sh
FORGE_MODEL_ARCHITECT=provider/model
FORGE_MODEL_IMPLEMENTER=provider/model
FORGE_MAX_ITERATIONS=4
FORGE_BUDGET_USD=2.50
FORGE_MAX_MINUTES=60
```

## CLI reference

```text
forge init [dir]        Create a workspace, goal, config, and schema
forge run [dir]         Run the goal-driven loop until gates + review pass
forge gates [dir]       Run only the machine gates (no model calls)
forge status [dir]      Show the latest run and recent history
forge doctor [dir]      Check OpenCode, models, commands, config, and git
forge models [dir]      Show model routing and prices
forge version           Show the harness version
forge help              Show CLI help
```

The workspace argument is optional and defaults to the current directory. Both `forge run /path/to/project` and `cd /path/to/project && forge run` work.

### Init flags

| Flag | Meaning |
| --- | --- |
| `--goal <text>` | Goal statement; required for a new workspace |
| `--force` | Overwrite generated control files and an existing `forge.config.json` |

### Run flags

| Flag | Meaning |
| --- | --- |
| `--max-iterations <n>` | Iteration limit; `0` means unlimited |
| `--budget <usd>` | Cost budget; `0` means unlimited |
| `--max-minutes <n>` | Wall-clock budget; `0` means unlimited |
| `--model <stage=model>` | Override one stage model; repeatable |
| `--config <file>` | Use a specific config file |
| `--checkpoint` | Commit after each iteration (requires git) |
| `--no-checkpoint` | Disable checkpoints for this run |
| `--dry-run` | Print the resolved plan without calling models |
| `--verbose` | More console detail |
| `--no-progress` | Disable the live status line (also `FORGE_NO_PROGRESS=1`) |

`status`, `doctor`, and `models` also accept `--json`. `status --report` prints the latest markdown report.

### Live progress

On an interactive terminal, `forge run` and `forge gates` draw a live status line while work is in progress:

```text
→ Implementer  ·  iter 1  ·  opencode/deepseek-v4.1-flash-free
  ⠹ 01:24 · editing internal/gates/gates.go · 12 tools · 4.3k tok
  ✓ Implementer  ·  1m 32s  ·  8,231 in / 1,204 out  ·  $0.00
```

The line reports elapsed time, the active tool or gate check, tool calls, tokens, and cost. Gate runs show a progress bar and completed checks, followed by a compact summary. Stage and gate results remain in the log after the live line is cleared.

Progress is disabled automatically when stdout is not a terminal. It can be disabled explicitly with `--no-progress` or `FORGE_NO_PROGRESS=1`; colors honor `NO_COLOR` and `TERM=dumb`.

## Artifacts

Everything lives under `.forge/` in the target workspace:

| Path | Purpose |
| --- | --- |
| `.forge/state.json` | Run history and latest status |
| `.forge/runs/<id>/run.json` | Full run record: stages, costs, gate outcomes |
| `.forge/runs/<id>/report.md` | Human-readable run report |
| `.forge/runs/<id>/prompts/` | Exact prompt sent to each stage |
| `.forge/runs/<id>/events/` | Raw OpenCode JSONL event streams |
| `.forge/runs/<id>/stages/` | Model text, token usage, and errors |
| `.forge/runs/<id>/commands/` | Command output for every gate |
| `.forge/runs/<id>/iterations/` | Per-iteration feedback records |
| `.forge/gates/<id>/` | Deterministic-only gate runs |
| `.forge/review.json` | Latest machine-readable independent review |
| `.forge/security.json` | Latest machine-readable security review |
| `.forge/triage.md` | Escalation diagnosis |

Review and security reports are JSON contracts; a missing or malformed report is a blocking failure rather than a silent pass.

Security report shape:

```json
{
  "verdict": "pass",
  "summary": "No exploitable issues found.",
  "findings": []
}
```

Review report shape:

```json
{
  "verdict": "approve",
  "summary": "Implementation matches the design and gates.",
  "design_change_needed": false,
  "findings": [
    {
      "severity": "low",
      "blocking": false,
      "title": "Optional naming cleanup",
      "file": "src/cli.js",
      "line": 12,
      "recommendation": "Consider renaming for clarity."
    }
  ]
}
```

A review passes only when `verdict` is `approve` and blocking findings do not exceed `acceptance.review.maxBlockingFindings`. Findings marked `blocking: true` are blocking; otherwise `high` and `critical` findings are treated as blocking.

## Development

```sh
go test ./...                          # unit and integration tests
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | tail -1
go vet ./...                           # static checks
go build ./...                         # build all packages
go run ./bench                         # write bench/results.json
gofmt -w cmd internal bench            # format
```

Run the harness against itself:

```sh
go run ./cmd/forge gates
```

The checked-in `forge.config.json` gates Forge itself with a 70% coverage floor, `go vet`, a successful build, and a parser benchmark threshold. Run the self-gates before opening a pull request.

To run the full model loop on Forge itself:

```sh
go run ./cmd/forge run --max-iterations 2 --budget 5
```

This consumes OpenCode model time. Provider-side usage limits may stop a run even when the harness is working correctly.

Testing guidance:

- Add tests next to the package they exercise.
- Prefer fake `opencode` scripts and temporary directories. Do not depend on network calls, real model calls, or globally installed tools in unit tests.
- Cover success and failure paths for gates: missing commands, optional failures, coverage thresholds, benchmark thresholds, secret findings, scanner failures, and report contract failures.
- Keep the runner tolerant of unknown JSONL event types and malformed lines.
- When changing an acceptance or report contract, update config normalization, validation, prompts, reports, the schema, and documentation together.

## Contributing

Issues and pull requests are welcome. For bugs, include the Forge version, workspace type, the command you ran, and the relevant output from `forge doctor` or `.forge/`. For feature proposals, describe the problem first and the smallest change that solves it.

Before opening a pull request:

1. Create a focused branch for the change.
2. Add or update tests and documentation.
3. Run `go test ./...`, `go vet ./...`, `go build ./...`, and `go run ./cmd/forge gates`.
4. Describe the motivation, behavior change, and validation in the PR.

Keep changes focused and respect the boundaries described in [Limitations](#limitations). Be respectful and constructive in issues and reviews.

Security issues should be reported privately through [GitHub Security Advisories](https://github.com/soub4i/forge/security/advisories/new), not in public issues.

Useful links: [issues](https://github.com/soub4i/forge/issues) · [pull requests](https://github.com/soub4i/forge/pulls) · [releases](https://github.com/soub4i/forge/releases)

## Troubleshooting

**`opencode` is missing or unauthenticated.** Run `opencode --version` and authenticate the provider. `forge doctor` reports this as a blocking issue.

**A model reports “Free usage limit reached”.** This is a provider quota, not a harness bug. Wait for the quota to reset, add a paid provider, or route the stage to another available model with `--model` or `models.<stage>.fallbacks`.

**`forge run` says no test or benchmark command exists.** Run `forge doctor` and `forge gates`; either add commands to `forge.config.json` or let the tester/benchmarker stage create them on the first iteration.

**Coverage is reported as 0% or missing.** Make the coverage command print a recognizable percentage or write a supported summary, then set `acceptance.coverage.file` explicitly if discovery does not find it. For Go, `go test -coverprofile=coverage.out ./...` works without printing a summary because Forge computes coverage from the profile.

**Benchmarks fail with a missing metric.** Inspect the configured `resultsFile` and `forge gates` output. Forge flattens nested JSON, but the threshold's metric name must match the flattened key exactly.

**The config changed during a run.** Forge fingerprints the contract. If an agent edits acceptance, commands, models, budget, loop, or release settings, the iteration fails. Revert the change or update the config intentionally outside the loop.

**A real secret is flagged in a test fixture.** Add the `// forge:allow-secret` marker on that exact line. Do not add broad ignore patterns unless the whole directory is genuinely out of scope.

**Git checkpoints are not created.** Checkpoints require a git repository. `forge doctor` reports whether the workspace is a repo. Enable `git.checkpoint` in config or pass `--checkpoint`, and make sure the working tree has changes.

## Limitations

- The model loop assumes OpenCode's `run --format json` event schema. If that schema changes, update the runner.
- Review and security stages are only as good as the model behind them. Deterministic gates are the hard floor; the reviewer is a second signal, not a proof.
- Command detection is intentionally best-effort. Unusual layouts may need explicit `commands`, and `acceptance.coverage.file` may need to point at a machine-readable summary.
- Forge does not sandbox model commands. Run it in a workspace you are willing to let an agent modify, and use git checkpoints for recovery.
- Dollar budgets rely on provider-reported cost or configured `pricing`; unpriced models are treated as free.
- Forge does not scaffold application code or offer starter templates. `forge init` adds control files to an existing project or an empty directory.
- Provider-side quotas and usage limits can fail a model call independently of the harness.

## License

MIT. See [LICENSE](LICENSE).
