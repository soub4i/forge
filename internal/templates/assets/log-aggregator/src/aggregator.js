export class UsageError extends Error {
  constructor(message) {
    super(message);
    this.name = 'UsageError';
  }
}

export const LEVELS = {
  debug: 10,
  info: 20,
  warn: 30,
  error: 40,
  fatal: 50,
};

const LEVEL_ALIASES = {
  trace: 'debug',
  debug: 'debug',
  info: 'info',
  notice: 'info',
  warn: 'warn',
  warning: 'warn',
  error: 'error',
  err: 'error',
  fatal: 'fatal',
  critical: 'fatal',
  crit: 'fatal',
};

export function normalizeLevel(value) {
  const raw = String(value ?? 'info').toLowerCase().trim();
  return LEVEL_ALIASES[raw] || 'info';
}

function normalizeTimestamp(value) {
  if (value === null || value === undefined || value === '') return null;
  if (typeof value === 'number' && Number.isFinite(value)) {
    const ms = value < 1_000_000_000_000 ? value * 1000 : value;
    const date = new Date(ms);
    return Number.isNaN(date.getTime()) ? null : date.toISOString();
  }
  const parsed = Date.parse(String(value));
  return Number.isNaN(parsed) ? null : new Date(parsed).toISOString();
}

/**
 * Parse one NDJSON log line. Malformed lines are counted, not thrown, so a
 * single bad record cannot stop a stream.
 */
export function parseLine(line) {
  const raw = String(line ?? '').trim();
  if (!raw) return null;
  let data;
  try {
    data = JSON.parse(raw);
  } catch {
    return { malformed: true, raw };
  }
  if (!data || typeof data !== 'object' || Array.isArray(data)) {
    return { malformed: true, raw };
  }
  const latencyValue = data.latency_ms ?? data.latency ?? data.duration_ms;
  const latency = Number(latencyValue);
  return {
    malformed: false,
    raw,
    level: normalizeLevel(data.level ?? data.severity ?? 'info'),
    message: String(data.message ?? data.msg ?? data.event ?? ''),
    timestamp: normalizeTimestamp(data.timestamp ?? data.time ?? data.ts ?? null),
    latencyMs: Number.isFinite(latency) && latency >= 0 ? latency : null,
  };
}

export function percentile(sortedValues, percent) {
  if (!sortedValues || sortedValues.length === 0) return null;
  const rank = Math.ceil((percent / 100) * sortedValues.length);
  const index = Math.min(sortedValues.length - 1, Math.max(0, rank - 1));
  return sortedValues[index];
}

export function latencySummary(values) {
  if (!values || values.length === 0) return null;
  const sorted = [...values].sort((a, b) => a - b);
  const sum = sorted.reduce((total, value) => total + value, 0);
  return {
    count: sorted.length,
    min: Number(sorted[0].toFixed(2)),
    max: Number(sorted[sorted.length - 1].toFixed(2)),
    avg: Number((sum / sorted.length).toFixed(2)),
    p50: Number(percentile(sorted, 50).toFixed(2)),
    p95: Number(percentile(sorted, 95).toFixed(2)),
    p99: Number(percentile(sorted, 99).toFixed(2)),
  };
}

export function createAggregator(options = {}) {
  const minLevel = options.minLevel ? normalizeLevel(options.minLevel) : null;
  const since = options.since ? Date.parse(String(options.since)) : null;
  const until = options.until ? Date.parse(String(options.until)) : null;
  if (options.since && Number.isNaN(since)) throw new UsageError(`invalid --since timestamp: ${options.since}`);
  if (options.until && Number.isNaN(until)) throw new UsageError(`invalid --until timestamp: ${options.until}`);

  const counts = Object.fromEntries(Object.keys(LEVELS).map((level) => [level, 0]));
  const latencies = [];
  const maxLatencySamples = Number(options.maxLatencySamples || 1_000_000) || 1_000_000;
  let total = 0;
  let malformed = 0;
  let filtered = 0;
  let firstTimestamp = null;
  let lastTimestamp = null;

  function add(entry) {
    if (!entry) return false;
    if (entry.malformed) {
      malformed += 1;
      return false;
    }
    if (minLevel && LEVELS[entry.level] < LEVELS[minLevel]) {
      filtered += 1;
      return false;
    }
    if (since !== null || until !== null) {
      if (!entry.timestamp) {
        filtered += 1;
        return false;
      }
      const time = Date.parse(entry.timestamp);
      if ((since !== null && time < since) || (until !== null && time > until)) {
        filtered += 1;
        return false;
      }
    }
    total += 1;
    counts[entry.level] = (counts[entry.level] || 0) + 1;
    if (entry.latencyMs !== null && latencies.length < maxLatencySamples) {
      latencies.push(entry.latencyMs);
    }
    if (entry.timestamp) {
      if (!firstTimestamp || entry.timestamp < firstTimestamp) firstTimestamp = entry.timestamp;
      if (!lastTimestamp || entry.timestamp > lastTimestamp) lastTimestamp = entry.timestamp;
    }
    return true;
  }

  function summary() {
    return {
      total,
      malformed,
      filtered,
      counts: { ...counts },
      latencyMs: latencySummary(latencies),
      firstTimestamp,
      lastTimestamp,
    };
  }

  return { add, summary };
}

export function formatSummary(summary, format = 'table') {
  if (format === 'json') return `${JSON.stringify(summary, null, 2)}\n`;
  const levels = Object.entries(summary.counts)
    .filter(([, count]) => count > 0)
    .map(([level, count]) => `${level}=${count}`)
    .join(' ');
  const lines = [
    `total=${summary.total} malformed=${summary.malformed} filtered=${summary.filtered}`,
    `levels: ${levels || '(none)'}`,
  ];
  if (summary.latencyMs) {
    const latency = summary.latencyMs;
    lines.push(
      `latency_ms: min=${latency.min} p50=${latency.p50} p95=${latency.p95} p99=${latency.p99} max=${latency.max} avg=${latency.avg}`,
    );
  }
  if (summary.firstTimestamp) lines.push(`window: ${summary.firstTimestamp} .. ${summary.lastTimestamp}`);
  return `${lines.join('\n')}\n`;
}

export function parseArgs(argv) {
  const options = {
    level: null,
    since: null,
    until: null,
    format: 'table',
    help: false,
    version: false,
    paths: [],
  };
  const args = [...argv];
  while (args.length > 0) {
    const arg = args.shift();
    if (arg === '--help' || arg === '-h') options.help = true;
    else if (arg === '--version' || arg === '-v') options.version = true;
    else if (arg === '--json') options.format = 'json';
    else if (arg === '--level' || arg === '-l') {
      const value = args.shift();
      if (!value) throw new UsageError(`${arg} requires a level`);
      options.level = value;
    } else if (arg.startsWith('--level=')) {
      options.level = arg.slice('--level='.length);
    } else if (arg === '--since') {
      const value = args.shift();
      if (!value) throw new UsageError('--since requires a timestamp');
      options.since = value;
    } else if (arg.startsWith('--since=')) {
      options.since = arg.slice('--since='.length);
    } else if (arg === '--until') {
      const value = args.shift();
      if (!value) throw new UsageError('--until requires a timestamp');
      options.until = value;
    } else if (arg.startsWith('--until=')) {
      options.until = arg.slice('--until='.length);
    } else if (arg === '--format' || arg === '-f') {
      const value = args.shift();
      if (!value) throw new UsageError(`${arg} requires json or table`);
      options.format = value;
    } else if (arg.startsWith('--format=')) {
      options.format = arg.slice('--format='.length);
    } else if (arg.startsWith('-')) {
      throw new UsageError(`unknown option: ${arg}`);
    } else {
      options.paths.push(arg);
    }
  }
  if (!['json', 'table'].includes(options.format)) {
    throw new UsageError(`invalid format: ${options.format} (expected json or table)`);
  }
  return options;
}

export function helpText(name = '{{name}}') {
  return `Usage: ${name} [options] [file...]

Aggregate NDJSON logs from stdin or files.

Options:
  -l, --level <level>   Minimum level: debug, info, warn, error, fatal
      --since <time>    Include records at or after this timestamp
      --until <time>    Include records at or before this timestamp
  -f, --format <fmt>    Output format: table (default) or json
      --json            Alias for --format json
  -h, --help            Show this help
  -v, --version         Show the version

Examples:
  cat app.ndjson | ${name} --level warn --json
  ${name} --since 2026-01-01T00:00:00Z app.ndjson
`;
}
