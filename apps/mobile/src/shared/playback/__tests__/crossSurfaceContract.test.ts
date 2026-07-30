import * as fs from 'fs';
import * as path from 'path';

function findGoApiRoot(): string | null {
  let dir = __dirname;
  for (let hop = 0; hop < 12; hop += 1) {
    const candidate = path.join(dir, 'services', 'go-api');
    if (fs.existsSync(candidate)) return candidate;
    const parent = path.dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  return null;
}

const GO_API_ROOT = findGoApiRoot();

const STRUCT_FIELD_LINE = /^\s*\w+\s+(\S+)\s+`json:"([a-zA-Z_]+)(?:,omitempty)?"`/gm;

function extractGoStruct(source: string, structName: string): string {
  const marker = `type ${structName} struct {`;
  const start = source.indexOf(marker);
  if (start === -1) throw new Error(`struct ${structName} not found in source`);
  const bodyStart = start + marker.length;
  const end = source.indexOf('\n}', bodyStart);
  if (end === -1) throw new Error(`unterminated struct ${structName}`);
  return source.slice(bodyStart, end);
}

function deriveGoFieldTypes(structBody: string): Map<string, string> {
  const fields = new Map<string, string>();
  for (const match of structBody.matchAll(STRUCT_FIELD_LINE)) {
    fields.set(match[2]!, match[1]!);
  }
  return fields;
}

function extractTsFunctionBody(source: string, functionName: string): string {
  const marker = `export function ${functionName}`;
  const start = source.indexOf(marker);
  if (start === -1) throw new Error(`function ${functionName} not found in source`);
  const rest = source.slice(start);
  const nextExport = rest.indexOf('export function', 1);
  return nextExport === -1 ? rest : rest.slice(0, nextExport);
}

function deriveConsumedFields(functionBody: string): Set<string> {
  const fields = new Set<string>();
  for (const match of functionBody.matchAll(/\bt\.(\w+)/g)) {
    fields.add(match[1]!);
  }
  return fields;
}

describe('cross-surface contract, derived from services/go-api at test time', () => {
  it('finds the Go source tree to derive the contract from', () => {
    expect(GO_API_ROOT).not.toBeNull();
  });

  it("toPlaybackTrack reads only fields the catalog TrackDTO actually sends", () => {
    const trackDtoSource = fs.readFileSync(
      path.join(GO_API_ROOT!, 'internal', 'catalog', 'service', 'track_dto.go'),
      'utf8',
    );
    const goFields = deriveGoFieldTypes(extractGoStruct(trackDtoSource, 'TrackDTO'));

    const toPlaybackTrackSource = fs.readFileSync(
      path.join(__dirname, '..', 'toPlaybackTrack.ts'),
      'utf8',
    );
    const consumed = deriveConsumedFields(
      extractTsFunctionBody(toPlaybackTrackSource, 'toPlaybackTrack'),
    );

    expect(consumed.size).toBeGreaterThan(0);
    const unknownFields = [...consumed].filter((field) => !goFields.has(field));
    expect(unknownFields).toEqual([]);
  });

  it('the artwork_url and duration_seconds fields toPlaybackTrack coerces are nullable on the Go side', () => {
    const trackDtoSource = fs.readFileSync(
      path.join(GO_API_ROOT!, 'internal', 'catalog', 'service', 'track_dto.go'),
      'utf8',
    );
    const goFields = deriveGoFieldTypes(extractGoStruct(trackDtoSource, 'TrackDTO'));

    expect(goFields.get('artwork_url')).toMatch(/^\*/);
    expect(goFields.get('duration_seconds')).toMatch(/^\*/);
  });

  it('currentTrackToPlaybackTrack reads only fields the queue-state currentTrackResponse actually sends', () => {
    const queueHandlerSource = fs.readFileSync(
      path.join(GO_API_ROOT!, 'internal', 'playback', 'adapters', 'handler', 'queue_handler.go'),
      'utf8',
    );
    const goFields = deriveGoFieldTypes(extractGoStruct(queueHandlerSource, 'currentTrackResponse'));

    const toPlaybackTrackSource = fs.readFileSync(
      path.join(__dirname, '..', 'toPlaybackTrack.ts'),
      'utf8',
    );
    const consumed = deriveConsumedFields(
      extractTsFunctionBody(toPlaybackTrackSource, 'currentTrackToPlaybackTrack'),
    );

    expect(consumed.size).toBeGreaterThan(0);
    const unknownFields = [...consumed].filter((field) => !goFields.has(field));
    expect(unknownFields).toEqual([]);
  });

  it('the artwork_url and duration_seconds fields currentTrackToPlaybackTrack coerces are nullable on the Go side', () => {
    const queueHandlerSource = fs.readFileSync(
      path.join(GO_API_ROOT!, 'internal', 'playback', 'adapters', 'handler', 'queue_handler.go'),
      'utf8',
    );
    const goFields = deriveGoFieldTypes(extractGoStruct(queueHandlerSource, 'currentTrackResponse'));

    expect(goFields.get('artwork_url')).toMatch(/^\*/);
    expect(goFields.get('duration_seconds')).toMatch(/^\*/);
  });

  it("the queueSourceDTO kind values match the QueueSource union's kinds exactly", () => {
    const queueSourceGoSource = fs.readFileSync(
      path.join(GO_API_ROOT!, 'internal', 'playback', 'domain', 'queue_source.go'),
      'utf8',
    );
    const goKinds = new Set<string>();
    for (const match of queueSourceGoSource.matchAll(/SourceKind\w+\s*=\s*"(\w+)"/g)) {
      goKinds.add(match[1]!);
    }

    const typesSource = fs.readFileSync(path.join(__dirname, '..', 'types.ts'), 'utf8');
    const queueSourceBlock = typesSource.slice(typesSource.indexOf('export type QueueSource ='));
    const tsKinds = new Set<string>();
    for (const match of queueSourceBlock.matchAll(/kind:\s*'(\w+)'/g)) {
      tsKinds.add(match[1]!);
    }

    expect(goKinds.size).toBeGreaterThan(0);
    expect([...tsKinds].sort()).toEqual([...goKinds].sort());
  });
});
