import * as fs from 'fs';
import * as path from 'path';

import { withEnvelope, makeEventId } from '../outbox';
import type { DiscoveryEvent } from '../recordEvent';

function findRepoRoot(): string {
  let dir = __dirname;
  for (let hop = 0; hop < 12; hop += 1) {
    if (fs.existsSync(path.join(dir, 'services', 'go-api'))) return dir;
    const parent = path.dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  throw new Error('repo root not found from ' + __dirname);
}

const SRC_ROOT = path.join(findRepoRoot(), 'apps', 'mobile', 'src');
const TELEMETRY_DIR = path.join(SRC_ROOT, 'shared', 'telemetry');
const API_CLIENT_DIR = path.join(SRC_ROOT, 'shared', 'api-client');
const FEATURES_DIR = path.join(SRC_ROOT, 'features');
const APP_DIR = path.join(SRC_ROOT, 'app');

function listSourceFiles(dir: string): string[] {
  if (!fs.existsSync(dir)) return [];
  const files: string[] = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === '__tests__' || entry.name === 'node_modules') continue;
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      files.push(...listSourceFiles(full));
    } else if (/\.tsx?$/.test(entry.name) && !entry.name.endsWith('.d.ts')) {
      files.push(full);
    }
  }
  return files;
}

function extractParenArg(source: string, callParenIndex: number): string {
  let depth = 0;
  for (let i = callParenIndex; i < source.length; i += 1) {
    if (source[i] === '(') depth += 1;
    else if (source[i] === ')') {
      depth -= 1;
      if (depth === 0) return source.slice(callParenIndex + 1, i);
    }
  }
  throw new Error(`unbalanced parens starting at ${callParenIndex}`);
}

type CallSite = { file: string; types: string[] };

function scanEnqueueCriticalCallSites(files: string[]): CallSite[] {
  const sites: CallSite[] = [];
  for (const file of files) {
    const source = fs.readFileSync(file, 'utf8');
    if (!/from\s+['"]@shared\/telemetry\/outbox['"]/.test(source)) continue;
    for (const m of source.matchAll(/\benqueueCritical\(/g)) {
      const parenIndex = m.index! + m[0].length - 1;
      const arg = extractParenArg(source, parenIndex);
      const types = [...arg.matchAll(/type:\s*'([a-zA-Z_]+)'/g)].map((x) => x[1]!);
      if (types.length === 0) {
        throw new Error(`enqueueCritical call in ${file} has no literal type: field this scanner can read`);
      }
      sites.push({ file, types });
    }
  }
  return sites;
}

function resolveShorthandTypeLiterals(source: string, beforeIndex: number): string[] {
  const before = source.slice(0, beforeIndex);
  const unionMatches = [...before.matchAll(/\btype:\s*((?:'[a-zA-Z_]+'\s*\|\s*)*'[a-zA-Z_]+')/g)];
  const last = unionMatches[unionMatches.length - 1];
  if (!last) return [];
  return [...last[1]!.matchAll(/'([a-zA-Z_]+)'/g)].map((x) => x[1]!);
}

function scanFireAndForgetCallSites(files: string[]): CallSite[] {
  const sites: CallSite[] = [];
  for (const file of files) {
    const source = fs.readFileSync(file, 'utf8');
    if (!/from\s+['"]@shared\/telemetry\/useRecordEvent['"]/.test(source)) continue;
    for (const m of source.matchAll(/\.mutate\(/g)) {
      const parenIndex = m.index! + m[0].length - 1;
      const arg = extractParenArg(source, parenIndex);
      if (!/\btype\b/.test(arg)) continue;

      const literalTypes = [...arg.matchAll(/type:\s*'([a-zA-Z_]+)'/g)].map((x) => x[1]!);
      if (literalTypes.length > 0) {
        sites.push({ file, types: literalTypes });
        continue;
      }

      if (/\{\s*type\s*[,}]/.test(arg)) {
        const shorthandTypes = resolveShorthandTypeLiterals(source, m.index!);
        if (shorthandTypes.length === 0) {
          throw new Error(`.mutate call in ${file} passes a shorthand 'type' this scanner cannot resolve`);
        }
        sites.push({ file, types: shorthandTypes });
      }
    }
  }
  return sites;
}

const LABEL_CRITICAL_TYPES = new Set(['library_add', 'wrong_album']);

describe('two-tier reliability: label-critical event types go through enqueueCritical, never the fire-and-forget hook', () => {
  const featureAndAppFiles = [...listSourceFiles(FEATURES_DIR), ...listSourceFiles(APP_DIR)];

  it('every enqueueCritical call site sends only library_add or wrong_album', () => {
    const sites = scanEnqueueCriticalCallSites(featureAndAppFiles);
    expect(sites.length).toBeGreaterThan(0);

    const offenders = sites.flatMap((s) => s.types.filter((t) => !LABEL_CRITICAL_TYPES.has(t)));
    expect(offenders).toEqual([]);
  });

  it('no call through the useRecordEvent fire-and-forget hook ever sends library_add or wrong_album', () => {
    const sites = scanFireAndForgetCallSites(featureAndAppFiles);
    expect(sites.length).toBeGreaterThan(0);

    const offenders = sites.flatMap((s) => s.types.filter((t) => LABEL_CRITICAL_TYPES.has(t)));
    expect(offenders).toEqual([]);
  });
});

describe('dependency direction is one-way', () => {
  it('no file in shared/telemetry imports from a features/* module', () => {
    const offenders: string[] = [];
    for (const file of listSourceFiles(TELEMETRY_DIR)) {
      const source = fs.readFileSync(file, 'utf8');
      if (/from\s+['"][^'"]*\bfeatures\/[^'"]*['"]/.test(source)) offenders.push(path.basename(file));
    }
    expect(offenders).toEqual([]);
  });

  it('nothing in shared/api-client imports from shared/telemetry', () => {
    const offenders: string[] = [];
    for (const file of listSourceFiles(API_CLIENT_DIR)) {
      const source = fs.readFileSync(file, 'utf8');
      if (/from\s+['"][^'"]*\btelemetry\/[^'"]*['"]/.test(source)) offenders.push(path.basename(file));
    }
    expect(offenders).toEqual([]);
  });
});

describe('no console.log in this slice', () => {
  it('every console call site in shared/telemetry is console.warn, never console.log', () => {
    const offenders: string[] = [];
    for (const file of listSourceFiles(TELEMETRY_DIR)) {
      const source = fs.readFileSync(file, 'utf8');
      if (/console\.log\(/.test(source)) offenders.push(path.basename(file));
    }
    expect(offenders).toEqual([]);
  });

  it('console.warn is present at least once, confirming the scan is exercising real code, not an empty slice', () => {
    const hasWarn = listSourceFiles(TELEMETRY_DIR).some((file) =>
      /console\.warn\(/.test(fs.readFileSync(file, 'utf8')),
    );
    expect(hasWarn).toBe(true);
  });
});

describe('the banned noun never appears in this slice', () => {
  it('no identifier or string literal in shared/telemetry contains "song"', () => {
    const offenders: string[] = [];
    for (const file of listSourceFiles(TELEMETRY_DIR)) {
      const source = fs.readFileSync(file, 'utf8');
      if (/\bsong/i.test(source)) offenders.push(path.basename(file));
    }
    expect(offenders).toEqual([]);
  });
});

describe('security: no auth material reaches this slice or the disk it writes to', () => {
  it('no file in shared/telemetry imports expo-secure-store, a Supabase client, or reads an access token', () => {
    const offenders: string[] = [];
    for (const file of listSourceFiles(TELEMETRY_DIR)) {
      const source = fs.readFileSync(file, 'utf8');
      const importsSecureStore = /expo-secure-store/.test(source);
      const importsSupabase = /supabase/i.test(source);
      const readsAccessToken = /access_token|accessToken/.test(source);
      if (importsSecureStore || importsSupabase || readsAccessToken) offenders.push(path.basename(file));
    }
    expect(offenders).toEqual([]);
  });

  it('withEnvelope adds exactly event_id and client_occurred_at to the caller event, nothing else', () => {
    const event: DiscoveryEvent = { type: 'library_add', payload: { title: 'x' } };

    const entry = withEnvelope(event, 'id-1', '2026-01-01T00:00:00.000Z');

    expect(Object.keys(entry).sort()).toEqual(['client_occurred_at', 'event_id', 'payload', 'type']);
  });

  it('the persisted entry never carries a key that looks like a token, header, or authorization field', () => {
    const entry = withEnvelope(
      { type: 'wrong_album', payload: { result_signature: 'sig' } },
      'id-1',
      '2026-01-01T00:00:00.000Z',
    );

    expect(Object.keys(entry).some((k) => /token|auth|header/i.test(k))).toBe(false);
  });

  it('makeEventId is unique across a large sample, the property a dedup key needs', () => {
    const sampleSize = 5000;
    const ids = new Set(Array.from({ length: sampleSize }, () => makeEventId()));

    expect(ids.size).toBe(sampleSize);
  });
});
