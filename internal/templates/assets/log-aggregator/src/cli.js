#!/usr/bin/env node
import fs from 'node:fs';
import readline from 'node:readline';
import { pathToFileURL } from 'node:url';
import {
  createAggregator,
  formatSummary,
  helpText,
  parseArgs,
  parseLine,
  UsageError,
} from './aggregator.js';

const VERSION = '0.1.0';

async function consumeStream(stream, aggregator) {
  const reader = readline.createInterface({ input: stream, crlfDelay: Infinity });
  for await (const line of reader) aggregator.add(parseLine(line));
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
    const aggregator = createAggregator({
      minLevel: options.level,
      since: options.since,
      until: options.until,
    });

    if (options.paths.length > 0) {
      for (const file of options.paths) {
        await consumeStream(fs.createReadStream(file), aggregator);
      }
    } else {
      await consumeStream(process.stdin, aggregator);
    }

    stdout.write(formatSummary(aggregator.summary(), options.format));
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
