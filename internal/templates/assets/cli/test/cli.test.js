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

test('CLI counts stdin', () => {
  const result = run([], 'hello world\n');
  assert.equal(result.status, 0);
  assert.equal(result.stdout, '1 2 12\n');
});

test('CLI emits JSON', () => {
  const result = run(['--json'], 'a\nb\n');
  assert.equal(result.status, 0);
  assert.deepEqual(JSON.parse(result.stdout), { lines: 2, words: 2, bytes: 4, source: null });
});

test('CLI reads a file', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'cli-test-'));
  const file = path.join(dir, 'sample.txt');
  fs.writeFileSync(file, 'one two\nthree\n', 'utf8');
  const result = run([file]);
  assert.equal(result.status, 0);
  assert.equal(result.stdout, '2 3 14 sample.txt\n'.replace('sample.txt', file));
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

test('CLI rejects unknown flags with exit code 2', () => {
  const result = run(['--bogus']);
  assert.equal(result.status, 2);
  assert.match(result.stderr, /unknown option/);
});

test('CLI reports missing files with exit code 1', () => {
  const result = run(['/definitely/not/here.txt']);
  assert.equal(result.status, 1);
  assert.match(result.stderr, /file not found/);
});
