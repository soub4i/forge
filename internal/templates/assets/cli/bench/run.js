import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { countText } from '../src/lib.js';

const here = path.dirname(fileURLToPath(import.meta.url));
const lines = 100_000;
const sample = '2026-01-01T00:00:00Z INFO request completed in 12ms';
const text = Array.from({ length: lines }, (_, index) => `${sample} #${index}`).join('\n');
const bytes = Buffer.byteLength(text, 'utf8');

for (let index = 0; index < 2; index += 1) countText(text);

const runs = 5;
const startedAt = performance.now();
for (let index = 0; index < runs; index += 1) countText(text);
const elapsedMs = performance.now() - startedAt;
const seconds = elapsedMs / 1000;

const results = {
  name: 'countText',
  metrics: {
    lines,
    bytes,
    runs,
    elapsed_ms: Number(elapsedMs.toFixed(2)),
    lines_per_sec: Math.round((lines * runs) / seconds),
    mb_per_sec: Number(((bytes * runs) / seconds / 1_000_000).toFixed(2)),
  },
};

fs.mkdirSync(here, { recursive: true });
fs.writeFileSync(path.join(here, 'results.json'), `${JSON.stringify(results, null, 2)}\n`, 'utf8');
process.stdout.write(`${JSON.stringify(results, null, 2)}\n`);
