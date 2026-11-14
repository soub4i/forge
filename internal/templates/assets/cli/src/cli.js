#!/usr/bin/env node
import fs from 'node:fs';
import { pathToFileURL } from 'node:url';
import { countText, formatCount, helpText, parseArgs, UsageError } from './lib.js';

const VERSION = '0.1.0';

async function readStdin() {
  const chunks = [];
  for await (const chunk of process.stdin) chunks.push(chunk);
  return Buffer.concat(chunks).toString('utf8');
}

async function readInput(options) {
  if (options.file) {
    return { text: fs.readFileSync(options.file, 'utf8'), source: options.file };
  }
  if (options.paths.length > 0) {
    const parts = options.paths.map((file) => fs.readFileSync(file, 'utf8'));
    return { text: parts.join(''), source: options.paths.join(',') };
  }
  return { text: await readStdin(), source: null };
}

export async function main(argv = process.argv.slice(2), io = {}) {
  const stdout = io.stdout || process.stdout;
  const stderr = io.stderr || process.stderr;
  try {
    const options = parseArgs(argv);
    if (options.help) {
      stdout.write(helpText());
      return 0;
    }
    if (options.version) {
      stdout.write(`${VERSION}\n`);
      return 0;
    }
    const { text, source } = await readInput(options);
    const count = countText(text);
    stdout.write(formatCount(count, source, { json: options.json }));
    return 0;
  } catch (error) {
    if (error instanceof UsageError) {
      stderr.write(`error: ${error.message}\n\n${helpText()}`);
      return 2;
    }
    if (error && error.code === 'ENOENT') {
      stderr.write(`error: file not found: ${error.path}\n`);
      return 1;
    }
    stderr.write(`error: ${error?.message || String(error)}\n`);
    return 1;
  }
}

const isMain = process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href;
if (isMain) {
  main().then((code) => {
    process.exitCode = code;
  });
}
