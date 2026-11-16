# Forge (Go)

[![CI](https://github.com/soub4i/forge/actions/workflows/ci.yml/badge.svg)](https://github.com/soub4i/forge/actions/workflows/ci.yml)

Forge is a reusable, language-agnostic, multi-model coding harness. You give it a goal and a workspace; it plans, designs, implements, tests, benchmarks, security-reviews, independently reviews, and releases in a loop until machine-checked gates and an independent reviewer agree.

This is the Go implementation of Forge. The harness is pure Go with no third-party dependencies. It builds projects in any language: Forge detects project manifests, package managers, and task runners, then runs the project's native install, lint, typecheck, build, test, coverage, and benchmark commands. Explicit `commands` in `forge.config.json` always win; detection fills the gaps. The starter/demo projects it generates remain **JavaScript** on purpose: the Go binary embeds the original JS `cli` and `log-aggregator` templates so you can build and test real Node.js projects while the harness itself runs as a single native binary.

```sh
forge init linecount --template cli --goal "Build a fast, dependency-free line counter CLI"
cd linecount
forge doctor
forge gates
forge run
```

## Why it exists

Most AI coding tools stop when the model says it is done. Forge stops only when hard checks pass:

- test, coverage, lint, typecheck, and build commands succeed
- benchmark metrics meet configured thresholds
- the built-in secret scan is clean and external scanners report nothing above the allowed severity
- an independent reviewer, which did not write the code, approves with no blocking findings
- the acceptance config was not weakened while the loop was running

If a check fails, Forge writes a bounded feedback file, repairs, escalates to the architect-tier model when it is stuck, and tries again until the budget or iteration limit is exhausted. The model is an implementation detail; the gates are the contract.

## Requirements

- **Go 1.23+** to build the harness (the module targets `go 1.23`)
- The **`opencode` CLI** on `PATH`, authenticated with at least one model
- **Git** optional, for repository context, checkpoints, and release tags
- The **native toolchain of the project you want to build** (Node.js, Go, Rust, Python, Java, .NET, Ruby, PHP, C/C++, Swift, Dart/Flutter, Elixir, Scala, Haskell, Zig, Deno, Lua, R, Julia, or a Makefile/just/Taskfile/Bazel/Nix-driven build)
- **Node.js 20+** only if you generate and run the bundled JavaScript starter projects
- No database, daemon, API key manager, or runtime package dependencies

Check the environment:

```sh
go version
opencode --version
node --version   # only needed for the JS starter projects
```

## Install

From this directory:

```sh
# Build a local binary in ./forge
go build -o forge ./cmd/forge

# Or install onto your PATH (usually $GOBIN or $HOME/go/bin)
go install ./cmd/forge
forge version
```

To install the published module directly:

```sh
go install github.com/soub4i/forge/cmd/forge@latest
```

If `forge` is not found after `go install`, add Go's bin directory to `PATH`:

```sh
export PATH="$PATH:$(go env GOPATH)/bin"
```

You can also run the harness directly without installing it:

```sh
go run ./cmd/forge --help
```

## Quick start

```sh
# 1. Create a JavaScript starter workspace from the embedded template
forge init my-cli --template cli --goal "Build a dependency-free line counter"

cd my-cli

# 2. Check OpenCode, model availability, commands, config, and git
forge doctor

# 3. Inspect the resolved plan without calling any model or writing run files
forge run --dry-run

# 4. Run only the deterministic gates
forge gates

# 5. Run the full loop with a small budget for the first experiment
forge run --max-iterations 2 --budget 1

# 6. Inspect the result
forge status
forge status --report
```

### Use an existing project

`--template none` writes Forge's control files into a project you already have, in any language. Forge records the commands it detected in `forge.config.json`, and you can edit or replace them at any time:

```sh
cd my-existing-project
forge init . --template none --goal "Add structured logging without changing the public API"
forge doctor
forge run --dry-run
forge gates
```

If you prefer to keep your own config, create `forge.config.json` manually with only a `goal` and let detection fill the rest. A root `Makefile` target, `just` recipe, `Taskfile` task, or executable `test.sh`/`build.sh` is enough for the script fallback.

`forge init` creates a complete workspace:

```text
my-cli/
  forge.config.json        goal, acceptance criteria, commands, models, budget
  .forge/forge.schema.json JSON schema for the config
  docs/GOAL.md             machine-readable copy of the goal and acceptance
  AGENTS.md                project profile, resolved commands, and agent rules
  .gitignore               ignores .forge/, dependencies, coverage, and build output
  src/ test/ bench/        JavaScript starter project (for cli/log-aggregator)
```

### Templates

| Template | What it starts with |
| --- | --- |
| `cli` (default) | Dependency-free line/word/byte counter with CLI and library tests, coverage, and a throughput benchmark |
| `log-aggregator` | Streaming NDJSON log aggregator with filtering, percentile summaries, tests, and a benchmark |
| `none` | Empty workspace with a goal, config, schema, and agent instructions only |

The `cli` and `log-aggregator` starters are JavaScript on purpose; Forge does not convert them to Go. Use `--template none` to add Forge to an existing project in any language. The generated `AGENTS.md` records the detected project profile and the commands the agents should run.

### Language and project support

Detection is best-effort and additive. Forge scans manifests and source files, chooses a primary ecosystem from source-file counts when several are present, and fills missing gates from other ecosystems, task runners, and scripts. Explicit `commands` remain authoritative.

| Project | Detected from |
| --- | --- |
| Node.js / Bun / Deno | `package.json`, `pnpm-lock.yaml`, `yarn.lock`, `bun.lockb`, `deno.json` |
| Go | `go.mod` (workspace `go.work` files are recorded as manifests) |
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

Projects with only source files (no manifest) still get a language profile, but Forge does not invent commands for them. Add a task runner, a conventional script, or explicit `commands`.

### Init flags

| Flag | Meaning |
| --- | --- |
| `--goal <text>` | Override the template's default goal |
| `--template <name>` | `cli`, `log-aggregator`, or `none` |
| `--force` | Overwrite generated files and an existing `forge.config.json` |

`forge init` detects the project type, writes runnable commands into `forge.config.json`, and picks model routing from the models currently available through OpenCode. It is safe to run `forge init . --force` to regenerate control files, but `--force` can overwrite generated starter files.

## Commands

```text
forge init [dir]        Create a workspace, goal, config, and starter template
forge run [dir]         Run the goal-driven loop until gates + review pass
forge gates [dir]       Run only the machine gates (no model calls)
forge status [dir]      Show the latest run and recent history
forge doctor [dir]      Check OpenCode, models, commands, config, and git
forge models [dir]      Show model routing and prices
forge version           Show the harness version
forge help              Show CLI help
```

The workspace argument is optional and defaults to the current directory. Both `forge run /path/to/project` and `cd /path/to/project && forge run` work.

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

Examples:

```sh
forge run --max-iterations 4 --budget 5
forge run --model reviewer=provider/strong-model --model implementer=provider/cheap-model
forge run --config ./configs/strict.json --checkpoint
```

`status`, `doctor`, and `models` also accept `--json`. `status --report` prints the latest markdown report.

## How the loop works

One iteration:

1. **Architect** writes `docs/ARCHITECTURE.md` and `docs/PLAN.md`.
2. **Designer** writes `docs/DESIGN.md` and refines the plan.
3. **Implementer** writes production code, tests, and scripts.
4. **Tester** strengthens tests and closes test gaps.
5. **Benchmarker** creates or repairs the benchmark when one is required.
6. **Machine gates** run install, lint, typecheck, build, test, coverage, benchmark, secret scan, and external scanners.
7. **Fixer** runs up to `loop.repairAttempts` times if the deterministic gates fail.
8. **Security** performs an adversarial review and writes `.forge/security.json`.
9. **Reviewer** independently reviews the result and writes `.forge/review.json`.
10. **Release** writes `README.md`, `CHANGELOG.md`, and `docs/RELEASE.md`, and optionally creates a git tag.

Architecture and design are skipped on later iterations unless their docs are missing or the reviewer requests a design change. The benchmarker is skipped when benchmarks are not required and when a benchmark command already exists. This keeps repair iterations cheaper.

When a failure repeats `loop.escalateAfter` times, a **Triage** stage diagnoses the root cause and the implementer/fixer use the architect-tier model for that iteration. A reviewer can request a design change, in which case architecture and design are regenerated before implementation resumes.

A stage runs in a fresh OpenCode session and hands work to the next stage through files. Every stage has a narrow write scope: security, review, and triage are read-only for production code; release may only touch documentation and release metadata.

## Machine gates

`forge gates` is safe to run without any model calls and is the fastest way to check whether a workspace is releasable. It writes command logs and a summary under `.forge/gates/<timestamp>/`.

Commands come from `forge.config.json` first, then from project detection, then from task runners and scripts. A missing enabled command is a failure, so `forge init` only enables gates for which it found or wrote a command.

The deterministic gates are evaluated in this order:

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

Supported formats include Istanbul JSON, Python coverage JSON, Cobertura XML, JaCoCo XML, LCOV, and text summaries from tools such as `go tool cover`, `nyc`, PHPUnit, SimpleCov, `mix test --cover`, JaCoCo, and tarpaulin. For Go, the extractor prefers the `total: (statements) X%` line, so multi-package output is parsed correctly even when individual packages report `0.0%`.

### Benchmarks

The benchmark command must exit successfully and report metrics either in the configured results file (default `bench/results.json`) or in stdout as JSON. Metrics can be nested under `metrics` or supplied as an array of objects with `name`/`metric`/`key` and `value`:

```json
{
  "metrics": {
    "lines_per_sec": 1250000,
    "p95_latency_ms": 3.2
  }
}
```

Thresholds support `min` and `max`. A threshold whose metric is missing fails. Units are labels only; Forge compares raw numbers.

### Secrets and external scanners

The built-in scanner checks source files for API keys, tokens, private keys, hard-coded credentials, and committed `.env` files, and redacts evidence in output. To exempt an intentional test fixture, put the marker on the same line as the value:

```go
// The scanner ignores this line.
fixture := `AKIAIOSFODNN7EXAMPLE` // forge:allow-secret
```

The marker is intentionally line-scoped. Prefer it over disabling a whole file.

External scanners are auto-detected when the relevant tool and project marker exist:

- `npm audit`, `pnpm audit`, or `yarn audit` for Node.js projects
- `pip-audit` for Python projects
- `govulncheck` for Go projects
- `cargo audit` for Rust projects
- `composer audit` for PHP projects
- `bundle-audit` for Ruby projects
- `dotnet list package --vulnerable` for .NET projects
- `osv-scanner` when it is installed

You can also configure scanners explicitly. An explicit scanner replaces an auto-detected scanner with the same name. Supported output formats are `npm-audit`, `pnpm-audit`, `yarn-audit`, `pip-audit`, `composer-audit`, `bundler-audit`, `dotnet-vulnerable`, `osv`, `semgrep`, `trivy`, `gitleaks`, `govulncheck`, `json`, and `text`.

## Model routing

Models are routed by stage and capability tier. Defaults use the free OpenCode models available in the development environment, but every stage can point at any provider/model. The tier is documentation and a routing hint; the actual choice is `models.<stage>.model`.

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

Each stage has fallbacks. If the selected model is unavailable or a call fails, Forge tries the next fallback, bounded by `loop.modelRetries`. `forge models` shows routing, prices, and availability warnings. `forge doctor` flags stages whose primary model is unavailable.

Override one stage for a run:

```sh
forge run --model reviewer=provider/strong-model --model implementer=provider/cheap-model
```

Or permanently in `forge.config.json`:

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

Prices are USD per million tokens. They feed the budget ledger and reports. Free or unpriced models are treated as zero cost, so token usage is still recorded but a dollar limit cannot be enforced precisely.

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
- `commands` can be omitted entirely. Forge detects the project's native commands and fills each missing gate; set a command to `null` to force detection for that gate.
- `acceptance.coverage` also accepts a number: `"coverage": 80`.
- `acceptance.benchmarks` also accepts an array of thresholds, or `false` to disable benchmarks.
- A command can be a string or `{ "command": "...", "timeoutMs": 600000, "optional": false }`.
- `acceptance.security.scanners` accepts entries such as `{ "name": "semgrep", "command": "semgrep --json ...", "format": "semgrep", "required": false }`.
- `acceptance.security.ignore` accepts glob patterns. `vendor/**` matches a directory tree; `*.min.js` matches a basename.
- `git.checkpoint` commits after each iteration when the workspace is a git repository. Forge excludes `.forge/` through `.git/info/exclude`.
- `release.enabled` requires the release stage to produce `README.md`, `CHANGELOG.md`, and `docs/RELEASE.md`; `release.tag` creates `v<version>` when a recognized project manifest exposes a version (`package.json`, `Cargo.toml`, `pyproject.toml`, `pom.xml`, Gradle, .NET project files, `composer.json`, gemspecs, `pubspec.yaml`, `mix.exs`, `build.sbt`, `DESCRIPTION`, CMake, Autotools, Meson, and others).

The harness fingerprints acceptance, commands, models, budget, loop, and release settings at the start of a run and checks them after every model stage. If a model edits any of them, the iteration fails with a tamper message. This prevents an agent from making the goal easier. Use `forge init --force` or edit the config outside a run when you intentionally want to change the contract.

Environment overrides are applied after config files:

```sh
FORGE_MODEL_ARCHITECT=provider/model
FORGE_MODEL_IMPLEMENTER=provider/model
FORGE_MAX_ITERATIONS=4
FORGE_BUDGET_USD=2.50
FORGE_MAX_MINUTES=60
```

## Reports and artifacts

Everything lives under `.forge/` in the target workspace:

| Path | Purpose |
| --- | --- |
| `.forge/state.json` | Run history and latest status |
| `.forge/runs/<id>/run.json` | Full run record, stage results, costs, gate outcomes |
| `.forge/runs/<id>/report.md` | Human-readable run report |
| `.forge/runs/<id>/prompts/` | Exact prompt sent to each stage |
| `.forge/runs/<id>/events/` | Raw OpenCode JSONL event streams |
| `.forge/runs/<id>/stages/` | Model text, token usage, errors |
| `.forge/runs/<id>/commands/` | Command output for every gate |
| `.forge/runs/<id>/iterations/` | Per-iteration feedback records |
| `.forge/gates/<id>/` | Deterministic-only gate runs |
| `.forge/review.json` | Latest machine-readable independent review |
| `.forge/security.json` | Latest machine-readable security review |
| `.forge/triage.md` | Escalation diagnosis |

The reviewer and security reports are JSON contracts. A missing or malformed report is a blocking failure rather than a silent pass.

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

## Repository layout

```text
cmd/forge/                 CLI entry point
internal/cli/              command parsing, commands, help, exit codes
internal/config/           defaults, load/merge, normalization, validation, fingerprint
internal/models/           model discovery, routing, pricing, budget ledger
internal/orchestrator/     goal loop, stages, repair/escalation, release, reporting
internal/runner/           OpenCode JSONL runner and event summarizer
internal/prompts/          per-stage prompt contracts and write scopes
internal/context/          repository snapshot, git facts, project facts
internal/gates/            command detection, deterministic gates, review gate
internal/coverage/         coverage parsing from JSON, XML, LCOV, Go profiles, and text
internal/benchmark/        benchmark result parsing and threshold evaluation
internal/security/         secret scan, scanner parsers, security gate
internal/reports/          .forge/ state, report contracts, markdown/JSON reports
internal/templates/        embedded JS starter templates and workspace init
internal/git/              checkpoints, tags, .forge exclusion
internal/util/             commands, files, JSONL, hashes, formatting
bench/main.go              self-benchmark for the production event parser
internal/templates/assets/ embedded JavaScript cli and log-aggregator templates
forge.config.json          self-hosting config used to gate Forge itself
```

The Go code uses only the standard library. The embedded JavaScript assets are copied byte-for-byte (with `{{name}}`, `{{goal}}`, `{{date}}`, and `{{version}}` substitution) into generated workspaces. `//go:embed all:assets` is used so dotfiles such as `.gitignore` are included.

## Developing Forge

```sh
go test ./...                 # unit and integration tests
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | tail -1
go vet ./...                  # lint
go build ./...                # build
go run ./bench                # writes bench/results.json
gofmt -w cmd internal bench   # format
```

Run the harness against itself:

```sh
go run ./cmd/forge gates
```

The self-hosting `forge.config.json` in this directory requires 70% statement coverage, `go vet`, a successful build, and `parse_ops_per_sec >= 100000` from the real JSONL parser benchmark. At the time of writing, the suite is near 79% coverage and the parser benchmark is comfortably above the 100k threshold on the development machine, but treat those numbers as measurements, not guarantees.

To run the full model loop on Forge itself:

```sh
go run ./cmd/forge run --max-iterations 2 --budget 5
```

This consumes OpenCode model time. Free-model usage limits are enforced by the provider and can stop a run even when the harness is working correctly.

### Testing guidance

- Add tests next to the package they exercise.
- Prefer fake `opencode` scripts and temporary directories; do not depend on network calls, real model calls, or globally installed tools in unit tests.
- Cover success and failure paths for gates: missing commands, optional failures, coverage thresholds, benchmark thresholds, secret findings, scanner failures, and report contract failures.
- Keep the runner tolerant of unknown JSONL event types and malformed lines.
- If you change an acceptance or report contract, update the config normalization, validation, prompts, reports, schema, and this README together.

## Troubleshooting

**`opencode` is missing or unauthenticated.** Run `opencode --version` and authenticate the provider. `forge doctor` reports this as a blocking issue.

**A model reports “Free usage limit reached”.** This is a provider quota, not a harness bug. Wait for the quota to reset, add a paid provider, or route the stage to another available model with `--model` or `models.<stage>.fallbacks`.

**`forge run` says no test or benchmark command exists.** Run `forge doctor` and `forge gates`; either add commands to `forge.config.json` or let the tester/benchmarker stage create them on the first iteration.

**Coverage is reported as 0% or missing.** Make the coverage command print a recognizable percentage or write a supported summary (Istanbul JSON, Cobertura XML, JaCoCo XML, LCOV, or a text summary), then set `acceptance.coverage.file` explicitly if discovery does not find it. For Go, `go test -coverprofile=coverage.out ./...` works without printing a summary because Forge computes coverage from the profile; `go tool cover -func=coverage.out | tail -1` also works.

**Benchmarks fail with a missing metric.** Inspect the configured `resultsFile` and `forge gates` output. Forge flattens nested JSON, but the threshold's metric name must match the flattened key exactly.

**The config changed during a run.** Forge fingerprints the contract. If an agent edits acceptance, commands, models, budget, loop, or release settings, the iteration fails. Revert the change or update the config intentionally outside the loop.

**A real secret is flagged in a test fixture.** Add the `// forge:allow-secret` marker on that exact line. Do not add broad ignore patterns unless the whole directory is genuinely out of scope.

**Git checkpoints are not created.** Checkpoints require a git repository. `forge doctor` reports whether the workspace is a repo. Enable `git.checkpoint` in config or pass `--checkpoint`, and make sure the working tree has changes.

## Limitations

- The model loop assumes OpenCode's `run --format json` event schema. If that schema changes, update `internal/runner`.
- Review and security stages are only as good as the model behind them. The deterministic gates are the hard floor; the reviewer is a second signal, not a proof.
- Command detection is intentionally best-effort. It recognizes common manifests and task runners, but unusual layouts may need explicit `commands`. Configure `acceptance.coverage.file` for a machine-readable summary when your test runner supports it.
- Forge does not sandbox model commands. Run it in a workspace you are willing to let an agent modify, and use git checkpoints for recovery.
- Dollar budgets rely on provider-reported cost or configured `pricing`; unpriced models are treated as free.
- The bundled starter projects are JavaScript by design. Use the `none` template to add Forge to a Go, Rust, Python, or other project; Forge detects its commands, and you can override any of them in `forge.config.json`.
- Free OpenCode models are subject to provider quotas and can fail independently of the harness.

## License

MIT. See [LICENSE](LICENSE).
