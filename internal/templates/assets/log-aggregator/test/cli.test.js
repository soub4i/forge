import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const cli = path.join(here, '..', 'src', 'cli.js');

function run(args, input = '') {
  return spawnSync(process.execPath, [cli, ...args], {
    input,
    encoding: 'utf8',
    timeout: 10_000,
  });
}

const ndjson = [
  JSON.stringify({ timestamp: '2026-01-01T00:00:00Z', level: 'info', message: 'ok', latency_ms: 10 }),
  JSON.stringify({ timestamp: '2026-01-01T00:00:01Z', level: 'error', message: 'boom', latency_ms: 20 }),
  'not-json',
].join('\n');

test('CLI aggregates stdin and emits JSON', () => {
  const result = run(['--json'], `${ndjson}\n`);
  assert.equal(result.status, 0);
  const summary = JSON.parse(result.stdout);
  assert.equal(summary.total, 2);
  assert.equal(summary.malformed, 1);
  assert.equal(summary.counts.error, 1);
  assert.equal(summary.latencyMs.p95, 20);
});

test('CLI filters by level', () => {
  const result = run(['--level', 'error', '--json'], `${ndjson}\n`);
  assert.equal(result.status, 0);
  const summary = JSON.parse(result.stdout);
  assert.equal(summary.total, 1);
  assert.equal(summary.filtered, 1);
});

test('CLI reads files and emits a table', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'log-agg-'));
  const file = path.join(dir, 'app.ndjson');
  fs.writeFileSync(file, `${ndjson}\n`, 'utf8');
  const result = run([file]);
  assert.equal(result.status, 0);
  assert.match(result.stdout, /total=2 malformed=1/);
  assert.match(result.stdout, /levels: info=1 error=1/);
  fs.rmSync(dir, { recursive: true, force: true });
});

test('CLI help and version exit zero', () => {
  const help = run(['--help']);
  assert.equal(help.status, 0);
  assert.match(help.stdout, /Usage:/);
  const version = run(['--version']);
  assert.equal(version.status, 0);
  assert.match(version.stdout, /^\d+\.\d+\.\d+/);
});

test('CLI rejects invalid usage', () => {
  assert.equal(run(['--format', 'yaml']).status, 2);
  assert.equal(run(['--bogus']).status, 2);
  assert.equal(run(['/definitely/not/here.ndjson']).status, 1);
});
