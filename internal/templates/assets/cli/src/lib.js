export class UsageError extends Error {
  constructor(message) {
    super(message);
    this.name = 'UsageError';
  }
}

const COMMANDS = new Set(['count']);

/**
 * Parse a deliberately small argv surface. Unknown flags are errors, not
 * silently ignored, so scripts fail loudly.
 */
export function parseArgs(argv) {
  const options = {
    command: 'count',
    json: false,
    help: false,
    version: false,
    file: null,
    paths: [],
  };
  const args = [...argv];
  while (args.length > 0) {
    const arg = args.shift();
    if (arg === '--help' || arg === '-h') {
      options.help = true;
    } else if (arg === '--version' || arg === '-v') {
      options.version = true;
    } else if (arg === '--json') {
      options.json = true;
    } else if (arg === '--file' || arg === '-f') {
      const value = args.shift();
      if (!value) throw new UsageError(`${arg} requires a path`);
      options.file = value;
    } else if (arg.startsWith('--file=')) {
      const value = arg.slice('--file='.length);
      if (!value) throw new UsageError('--file requires a path');
      options.file = value;
    } else if (COMMANDS.has(arg)) {
      options.command = arg;
    } else if (arg.startsWith('-')) {
      throw new UsageError(`unknown option: ${arg}`);
    } else {
      options.paths.push(arg);
    }
  }
  return options;
}

export function countText(text) {
  const value = String(text ?? '');
  const lines = value.length === 0 ? 0 : value.split('\n').length - (value.endsWith('\n') ? 1 : 0);
  const words = value.match(/\S+/g)?.length ?? 0;
  const bytes = Buffer.byteLength(value, 'utf8');
  return { lines, words, bytes };
}

export function formatCount(count, source = null, { json = false } = {}) {
  if (json) {
    return `${JSON.stringify({ ...count, source }, null, 2)}\n`;
  }
  const suffix = source ? ` ${source}` : '';
  return `${count.lines} ${count.words} ${count.bytes}${suffix}\n`;
}

export function helpText(name = '{{name}}') {
  return `Usage: ${name} [count] [options] [file...]

Count lines, words, and UTF-8 bytes from stdin or files.

Options:
  -f, --file <path>   Read a single file instead of stdin
      --json          Emit a JSON object
  -h, --help          Show this help
  -v, --version       Show the version

Examples:
  cat app.log | ${name}
  ${name} --json src/index.js
  ${name} --file app.log
`;
}
