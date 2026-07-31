import * as fs from 'fs';
import * as path from 'path';

import { getSessionId } from '../session';

function findRepoRoot(): string | null {
  let dir = __dirname;
  for (let hop = 0; hop < 12; hop += 1) {
    if (fs.existsSync(path.join(dir, 'services', 'go-api'))) return dir;
    const parent = path.dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  return null;
}

const REPO_ROOT = findRepoRoot();
const GO_API_ROOT = REPO_ROOT === null ? null : path.join(REPO_ROOT, 'services', 'go-api');
const MOBILE_SRC = path.join(REPO_ROOT!, 'apps', 'mobile', 'src');
const TELEMETRY_SRC = path.join(MOBILE_SRC, 'shared', 'telemetry');

function readGoFile(...relParts: string[]): string {
  return fs.readFileSync(path.join(GO_API_ROOT!, ...relParts), 'utf8');
}

function findMatchingBrace(text: string, openIndex: number): number {
  let depth = 0;
  for (let i = openIndex; i < text.length; i += 1) {
    if (text[i] === '{') depth += 1;
    else if (text[i] === '}') {
      depth -= 1;
      if (depth === 0) return i;
    }
  }
  throw new Error(`unbalanced braces starting at ${openIndex}`);
}

function extractBlock(source: string, marker: string): string {
  const start = source.indexOf(marker);
  if (start === -1) throw new Error(`marker not found: ${marker}`);
  const braceStart = source.indexOf('{', start);
  return source.slice(braceStart + 1, findMatchingBrace(source, braceStart));
}

function listMobileSourceFiles(root: string): string[] {
  const found: string[] = [];
  for (const dirent of fs.readdirSync(root, { withFileTypes: true })) {
    const full = path.join(root, dirent.name);
    if (dirent.isDirectory()) {
      if (dirent.name === '__tests__' || dirent.name === 'node_modules') continue;
      found.push(...listMobileSourceFiles(full));
    } else if (/\.tsx?$/.test(dirent.name) && !dirent.name.endsWith('.d.ts')) {
      found.push(full);
    }
  }
  return found;
}

const EVENTS_GO = 'internal/discovery/domain/events.go';
const RECORD_EVENT_TS = path.join(TELEMETRY_SRC, 'recordEvent.ts');
const OUTBOX_TS = path.join(TELEMETRY_SRC, 'outbox.ts');
const SESSION_TS = path.join(TELEMETRY_SRC, 'session.ts');

function deriveGoEventTypeNames(eventsSource: string): Map<string, string> {
  const block = extractBlock(eventsSource, 'var eventTypeNames = map[EventType]string{');
  const names = new Map<string, string>();
  for (const m of block.matchAll(/EventType(\w+):\s*"([a-z_]+)"/g)) {
    names.set(`EventType${m[1]!}`, m[2]!);
  }
  return names;
}

function deriveGoClientSubmittableNames(eventsSource: string, byIdent: Map<string, string>): Set<string> {
  const body = extractBlock(eventsSource, 'func (e EventType) ClientSubmittable() bool {');
  const submittable = new Set<string>();
  for (const m of body.matchAll(/case\s+([^:]+):/g)) {
    for (const rawIdent of m[1]!.split(',')) {
      const ident = rawIdent.trim();
      const name = byIdent.get(ident);
      if (name) submittable.add(name);
    }
  }
  return submittable;
}

function deriveMobileDiscoveryEventTypeUnion(recordEventSource: string): Set<string> {
  const start = recordEventSource.indexOf('export type DiscoveryEventType =');
  if (start === -1) throw new Error('DiscoveryEventType union not found');
  const end = recordEventSource.indexOf(';', start);
  const block = recordEventSource.slice(start, end);
  const names = new Set<string>();
  for (const m of block.matchAll(/'([a-z_]+)'/g)) names.add(m[1]!);
  return names;
}

function deriveGoEnvelopeFieldNames(handlerSource: string): Set<string> {
  const body = extractBlock(handlerSource, 'type DiscoveryEventRequest struct {');
  const names = new Set<string>();
  for (const m of body.matchAll(/json:"(\w+)"/g)) names.add(m[1]!);
  return names;
}

function deriveMobileEnvelopeFieldNames(recordEventSource: string): Set<string> {
  const start = recordEventSource.indexOf('export type DiscoveryEvent = {');
  if (start === -1) throw new Error('DiscoveryEvent type not found');
  const braceStart = recordEventSource.indexOf('{', start);
  const body = recordEventSource.slice(braceStart + 1, findMatchingBrace(recordEventSource, braceStart));
  const names = new Set<string>();
  for (const m of body.matchAll(/^\s*(\w+)\??:/gm)) names.add(m[1]!);
  return names;
}

type PayloadTypeConstraints = {
  numberKeys: Set<string>;
  booleanKeys: Set<string>;
  stringKeys: Set<string>;
};

function deriveGoPayloadTypeConstraints(recordEventServiceSource: string): PayloadTypeConstraints {
  const body = extractBlock(recordEventServiceSource, 'func validatePayloadTypes(payload map[string]any) error {');

  const numberMatch = body.match(
    /\[\.\.\.\]string\{([^}]*)\}\s*\{\s*if v, ok := payload\[key\]; ok \{\s*if _, isNum := v\.\(float64\)/,
  );
  const booleanMatch = body.match(/payload\["(\w+)"\]; ok \{\s*if _, isBool := v\.\(bool\)/);
  const stringMatch = body.match(
    /\[\.\.\.\]string\{([^}]*)\}\s*\{\s*if v, ok := payload\[key\]; ok \{\s*if _, isStr := v\.\(string\)/,
  );

  if (!numberMatch || !booleanMatch || !stringMatch) {
    throw new Error('validatePayloadTypes shape changed in a way this scanner cannot follow');
  }

  const parseKeys = (group: string): Set<string> =>
    new Set([...group.matchAll(/"([a-zA-Z_]+)"/g)].map((m) => m[1]!));

  return {
    numberKeys: parseKeys(numberMatch[1]!),
    booleanKeys: new Set([booleanMatch[1]!]),
    stringKeys: parseKeys(stringMatch[1]!),
  };
}

function deriveSessionKeyWrittenByTelemetry(recordEventSource: string): string {
  const m = recordEventSource.match(/payload:\s*\{[\s\S]*?(\w+):\s*getSessionId\(\)/);
  if (!m) throw new Error('could not find the payload key telemetry stamps via getSessionId()');
  return m[1]!;
}

describe('finds the Go source tree to derive every contract from', () => {
  it('locates services/go-api from this test file', () => {
    expect(GO_API_ROOT).not.toBeNull();
  });
});

describe('event-name set: every DiscoveryEventType the mobile client declares is a name Go knows', () => {
  it('is a subset of the names in eventTypeNames, since search_performed is legitimately server-only', () => {
    const goNames = new Set(deriveGoEventTypeNames(readGoFile(EVENTS_GO)).values());
    const mobileNames = deriveMobileDiscoveryEventTypeUnion(fs.readFileSync(RECORD_EVENT_TS, 'utf8'));

    expect(goNames.size).toBeGreaterThan(0);
    expect(mobileNames.size).toBeGreaterThan(0);
    for (const name of mobileNames) {
      expect(goNames.has(name)).toBe(true);
    }
  });
});

describe('client-submittable subset: every DiscoveryEventType the mobile client can send is one Go accepts at POST /v1/discovery/events', () => {
  it('every type the mobile union declares is one Go.ClientSubmittable() accepts', () => {
    const eventsSource = readGoFile(EVENTS_GO);
    const byIdent = deriveGoEventTypeNames(eventsSource);
    const submittable = deriveGoClientSubmittableNames(eventsSource, byIdent);
    const mobileNames = deriveMobileDiscoveryEventTypeUnion(fs.readFileSync(RECORD_EVENT_TS, 'utf8'));

    expect(submittable.size).toBeGreaterThan(0);
    for (const name of mobileNames) {
      expect(submittable.has(name)).toBe(true);
    }
  });

  it('results_shown is client-submittable, because only the device can observe the viewport', () => {
    const eventsSource = readGoFile(EVENTS_GO);
    const byIdent = deriveGoEventTypeNames(eventsSource);
    const submittable = deriveGoClientSubmittableNames(eventsSource, byIdent);

    expect(submittable.has('results_shown')).toBe(true);
    expect(submittable.has('search_performed')).toBe(false);
  });
});

describe('envelope field names: every field the mobile client sends is one search_endpoints.go decodes', () => {
  it('DiscoveryEvent field names are a subset of DiscoveryEventRequest json tags', () => {
    const goFields = deriveGoEnvelopeFieldNames(
      readGoFile('internal/discovery/adapters/handler/search_endpoints.go'),
    );
    const mobileFields = deriveMobileEnvelopeFieldNames(fs.readFileSync(RECORD_EVENT_TS, 'utf8'));

    expect(goFields.size).toBeGreaterThan(0);
    expect(mobileFields.size).toBeGreaterThan(0);
    for (const field of mobileFields) {
      expect(goFields.has(field)).toBe(true);
    }
  });

  it('the outbox envelope (event_id, client_occurred_at) also names fields Go decodes', () => {
    const goFields = deriveGoEnvelopeFieldNames(
      readGoFile('internal/discovery/adapters/handler/search_endpoints.go'),
    );
    const outboxSource = fs.readFileSync(OUTBOX_TS, 'utf8');

    expect(outboxSource).toContain('event_id');
    expect(outboxSource).toContain('client_occurred_at');
    expect(goFields.has('event_id')).toBe(true);
    expect(goFields.has('client_occurred_at')).toBe(true);
  });
});

describe('payload value types: the key this slice writes into payload satisfies the type Go requires', () => {
  it('session_id is derived as a Go string-constrained payload key', () => {
    const constraints = deriveGoPayloadTypeConstraints(
      readGoFile('internal/discovery/service/record_event.go'),
    );
    const key = deriveSessionKeyWrittenByTelemetry(fs.readFileSync(RECORD_EVENT_TS, 'utf8'));

    expect(key).toBe('session_id');
    expect(constraints.stringKeys.has(key)).toBe(true);
    expect(constraints.numberKeys.has(key)).toBe(false);
    expect(constraints.booleanKeys.has(key)).toBe(false);
  });

  it('getSessionId() always returns a string, satisfying the derived constraint', () => {
    expect(fs.readFileSync(SESSION_TS, 'utf8')).toContain('export function getSessionId');
    for (let i = 0; i < 5; i += 1) {
      expect(typeof getSessionId()).toBe('string');
    }
  });

  it('no mobile call site sends a Go string-constrained payload key as null, which the server 400s', () => {
    const constraints = deriveGoPayloadTypeConstraints(
      readGoFile('internal/discovery/service/record_event.go'),
    );
    const offenders: string[] = [];

    for (const filePath of listMobileSourceFiles(MOBILE_SRC)) {
      const source = fs.readFileSync(filePath, 'utf8');
      for (const key of constraints.stringKeys) {
        const assignedNull = new RegExp(`\\b${key}\\s*:\\s*[^,;\\n}]*\\bnull\\b`);
        if (assignedNull.test(source)) {
          offenders.push(`${path.relative(MOBILE_SRC, filePath)}:${key}`);
        }
      }
    }

    expect(offenders).toEqual([]);
  });
});
