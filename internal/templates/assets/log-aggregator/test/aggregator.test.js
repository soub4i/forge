import assert from 'node:assert/strict';
import test from 'node:test';
import {
  createAggregator,
  formatSummary,
  latencySummary,
  normalizeLevel,
  parseArgs,
  parseLine,
  percentile,
  UsageError,
} from '../src/aggregator.js';

test('normalizeLevel handles aliases and unknown values', () => {
  assert.equal(normalizeLevel('WARNING'), 'warn');
  assert.equal(normalizeLevel('crit'), 'fatal');
  assert.equal(normalizeLevel('trace'), 'debug');
  assert.equal(normalizeLevel('nonsense'), 'info');
});

test('parseLine parses valid NDJSON and normalizes fields', () => {
  const entry = parseLine(
    JSON.stringify({ timestamp: '2026-01-01T00:00:00Z', level: 'WARN', message: 'slow', latency_ms: 12.5 }),
  );
  assert.equal(entry.malformed, false);
  assert.equal(entry.level, 'warn');
  assert.equal(entry.message, 'slow');
  assert.equal(entry.latencyMs, 12.5);
  assert.equal(entry.timestamp, '2026-01-01T00:00:00.000Z');
});

test('parseLine handles malformed and empty lines', () => {
  assert.equal(parseLine(''), null);
  assert.equal(parseLine('not json').malformed, true);
  assert.equal(parseLine('[1,2,3]').malformed, true);
});

test('percentile uses nearest rank', () => {
  const values = [1, 2, 3, 4, 5];
  assert.equal(percentile(values, 50), 3);
  assert.equal(percentile(values, 95), 5);
  assert.equal(percentile([], 50), null);
});

test('latencySummary reports min, max, avg, and percentiles', () => {
  const summary = latencySummary([10, 20, 30, 40, 50]);
  assert.equal(summary.count, 5);
  assert.equal(summary.min, 10);
  assert.equal(summary.max, 50);
  assert.equal(summary.avg, 30);
  assert.equal(summary.p50, 30);
  assert.equal(summary.p95, 50);
});

test('aggregator counts levels, malformed lines, and latency', () => {
  const aggregator = createAggregator();
  aggregator.add(parseLine(JSON.stringify({ level: 'info', latency_ms: 10 })));
  aggregator.add(parseLine(JSON.stringify({ level: 'error', latency_ms: 20 })));
  aggregator.add(parseLine('bad line'));
  const summary = aggregator.summary();
  assert.equal(summary.total, 2);
  assert.equal(summary.malformed, 1);
  assert.equal(summary.counts.info, 1);
  assert.equal(summary.counts.error, 1);
  assert.equal(summary.latencyMs.p50, 10);
});

test('aggregator filters by minimum level', () => {
  const aggregator = createAggregator({ minLevel: 'warn' });
  aggregator.add(parseLine(JSON.stringify({ level: 'info' })));
  aggregator.add(parseLine(JSON.stringify({ level: 'warn' })));
  aggregator.add(parseLine(JSON.stringify({ level: 'fatal' })));
  const summary = aggregator.summary();
  assert.equal(summary.total, 2);
  assert.equal(summary.filtered, 1);
  assert.equal(summary.counts.info, 0);
});

test('aggregator filters by time window', () => {
  const aggregator = createAggregator({
    since: '2026-01-01T00:00:00Z',
    until: '2026-01-01T23:59:59Z',
  });
  aggregator.add(parseLine(JSON.stringify({ timestamp: '2025-12-31T23:59:59Z' })));
  aggregator.add(parseLine(JSON.stringify({ timestamp: '2026-01-01T12:00:00Z' })));
  aggregator.add(parseLine(JSON.stringify({ timestamp: '2026-01-02T00:00:00Z' })));
  assert.equal(aggregator.summary().total, 1);
});

test('formatSummary emits table and JSON', () => {
  const summary = {
    total: 2,
    malformed: 1,
    filtered: 0,
    counts: { debug: 0, info: 1, warn: 0, error: 1, fatal: 0 },
    latencyMs: { min: 1, max: 2, avg: 1.5, p50: 1, p95: 2, p99: 2, count: 2 },
    firstTimestamp: '2026-01-01T00:00:00.000Z',
    lastTimestamp: '2026-01-01T00:00:01.000Z',
  };
  assert.match(formatSummary(summary, 'table'), /total=2/);
  assert.deepEqual(JSON.parse(formatSummary(summary, 'json')), summary);
});

test('parseArgs validates flags and formats', () => {
  const options = parseArgs(['--level', 'warn', '--since', '2026-01-01', '--json', 'a.log']);
  assert.equal(options.level, 'warn');
  assert.equal(options.format, 'json');
  assert.deepEqual(options.paths, ['a.log']);
  assert.throws(() => parseArgs(['--format', 'yaml']), UsageError);
  assert.throws(() => parseArgs(['--level']), UsageError);
  assert.throws(() => parseArgs(['--bogus']), UsageError);
});
