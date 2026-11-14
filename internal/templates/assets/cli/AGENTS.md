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

- Keep the CLI dependency-free and portable across Node.js >= 20.
- Pure logic belongs in `src/lib.js`; `src/cli.js` should stay a thin IO shell.
- Every behavior change needs a unit test. CLI behavior needs an integration test.
- Tests and benchmarks must be deterministic and offline.
- Do not weaken tests, benchmarks, or acceptance criteria to make a gate pass.
- Never commit secrets or machine-specific paths.
