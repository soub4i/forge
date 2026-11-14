import assert from 'node:assert/strict';
import test from 'node:test';
import { countText, formatCount, parseArgs, UsageError } from '../src/lib.js';

test('parseArgs defaults to count from stdin', () => {
  const options = parseArgs([]);
  assert.equal(options.command, 'count');
  assert.equal(options.json, false);
  assert.equal(options.file, null);
  assert.deepEqual(options.paths, []);
});

test('parseArgs accepts json, file, and paths', () => {
  const options = parseArgs(['--json', '--file', 'app.log']);
  assert.equal(options.json, true);
  assert.equal(options.file, 'app.log');
  assert.deepEqual(options.paths, []);

  const withEquals = parseArgs(['--file=app.log']);
  assert.equal(withEquals.file, 'app.log');

  const withPaths = parseArgs(['a.log', 'b.log']);
  assert.deepEqual(withPaths.paths, ['a.log', 'b.log']);
});

test('parseArgs rejects unknown flags and missing values', () => {
  assert.throws(() => parseArgs(['--nope']), UsageError);
  assert.throws(() => parseArgs(['--file']), UsageError);
  assert.throws(() => parseArgs(['--file=']), UsageError);
});

test('countText counts lines, words, and UTF-8 bytes', () => {
  assert.deepEqual(countText('hello world\n'), { lines: 1, words: 2, bytes: 12 });
  assert.deepEqual(countText('a\nb'), { lines: 2, words: 2, bytes: 3 });
  assert.deepEqual(countText(''), { lines: 0, words: 0, bytes: 0 });
  assert.deepEqual(countText('héllo\n'), { lines: 1, words: 1, bytes: 7 });
});

test('formatCount emits text and JSON', () => {
  assert.equal(formatCount({ lines: 1, words: 2, bytes: 3 }, 'x'), '1 2 3 x\n');
  const json = JSON.parse(formatCount({ lines: 1, words: 2, bytes: 3 }, 'x', { json: true }));
  assert.deepEqual(json, { lines: 1, words: 2, bytes: 3, source: 'x' });
});
