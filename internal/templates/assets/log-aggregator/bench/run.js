import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createAggregator, parseLine } from '../src/aggregator.js';

const here = path.dirname(fileURLToPath(import.meta.url));
const lineCount = 200_000;
const lines = Array.from({ length: lineCount }, (_, index) =>
  JSON.stringify({
    timestamp: new Date(1_767_225_600_000 + index * 1000).toISOString(),
    level: index % 20 === 0 ? 'error' : index % 5 === 0 ? 'warn' : 'info',
    message: `request ${index} completed`,
    latency_ms: index % 250,
  }),
);

for (let index = 0; index < 20_000; index += 1) {
  const aggregator = createAggregator();
  aggregator.add(parseLine(lines[index]));
}

const runs = 3;
let elapsedMs = 0;
for (let run = 0; run < runs; run += 1) {
  const startedAt = performance.now();
  const aggregator = createAggregator();
  for (const line of lines) aggregator.add(parseLine(line));
  aggregator.summary();
  elapsedMs += performance.now() - startedAt;
}
const seconds = elapsedMs / 1000;

const results = {
  name: 'parse-and-aggregate',
  metrics: {
    lines: lineCount,
    runs,
    elapsed_ms: Number(elapsedMs.toFixed(2)),
    events_per_sec: Math.round((lineCount * runs) / seconds),
  },
};

fs.mkdirSync(here, { recursive: true });
fs.writeFileSync(path.join(here, 'results.json'), `${JSON.stringify(results, null, 2)}\n`, 'utf8');
process.stdout.write(`${JSON.stringify(results, null, 2)}\n`);
