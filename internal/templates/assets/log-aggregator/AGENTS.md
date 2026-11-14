# Agent instructions

This repository is managed by Forge, an autonomous goal-driven harness.

## Goal

{{goal}}

## Commands

- Test: `npm test`
- Coverage: `npm run test:coverage`
- Lint: `npm run lint`
- Build: `npm run build`
- Benchmark: `npm run bench`

## Rules

- Keep the tool dependency-free and streaming. Do not read entire large inputs
  into memory unless the goal explicitly allows it.
- `src/aggregator.js` should stay pure and unit-testable; `src/cli.js` owns IO.
- Malformed input must be counted, never fatal.
- Every behavior change needs a unit test. CLI behavior needs an integration test.
- Tests and benchmarks must be deterministic and offline.
- Do not weaken tests, benchmarks, or acceptance criteria to make a gate pass.
- Never commit secrets or machine-specific paths.
