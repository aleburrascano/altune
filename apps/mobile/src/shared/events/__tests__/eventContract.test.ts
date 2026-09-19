import * as fs from 'fs';
import * as path from 'path';

import { SERVER_EVENT_TYPES } from '../eventTypes';

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

// The event type is the third argument to Publish(ctx, userId, type, payload):
// a quoted literal, an exported constant (`events.TypeTrackDeleted`), or a
// lower-case parameter a decorator forwards.
const PUBLISH_EVENT_TYPE_ARG = /\.Publish\(\s*[\w.]+,\s*[\w.]+,\s*([^,\s]+)\s*,/g;
const GO_STRING_CONSTANT = /^\s*([A-Z]\w*)\s*=\s*"([a-zA-Z_]+)"/gm;
const SSE_LITERAL_EVENT_LINE = /"event:\s*([a-zA-Z_]+)\\n/g;
const QUOTED_LITERAL = /^"([a-zA-Z_]+)"$/;
const EXPORTED_CONSTANT = /^(?:\w+\.)?([A-Z]\w*)$/;

function listGoSourceFiles(dir: string): string[] {
  const files: string[] = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === 'node_modules' || entry.name.startsWith('.')) continue;
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      files.push(...listGoSourceFiles(full));
    } else if (entry.name.endsWith('.go') && !entry.name.endsWith('_test.go')) {
      files.push(full);
    }
  }
  return files;
}

function readStringConstants(sources: readonly string[]): Map<string, string> {
  const constants = new Map<string, string>();
  for (const text of sources) {
    for (const [, name, value] of text.matchAll(GO_STRING_CONSTANT)) constants.set(name!, value!);
  }
  return constants;
}

type PublishedEventTypes = { types: Set<string>; unresolved: Set<string> };

function derivePublishedEventTypes(root: string): PublishedEventTypes {
  const sources = listGoSourceFiles(root).map((file) => fs.readFileSync(file, 'utf8'));
  const constants = readStringConstants(sources);
  const types = new Set<string>();
  const unresolved = new Set<string>();

  for (const text of sources) {
    for (const [, argument] of text.matchAll(PUBLISH_EVENT_TYPE_ARG)) {
      const literal = QUOTED_LITERAL.exec(argument!);
      if (literal !== null) {
        types.add(literal[1]!);
        continue;
      }
      const constant = EXPORTED_CONSTANT.exec(argument!);
      if (constant === null) continue;
      const value = constants.get(constant[1]!);
      if (value === undefined) unresolved.add(argument!);
      else types.add(value);
    }
    for (const [, type] of text.matchAll(SSE_LITERAL_EVENT_LINE)) types.add(type!);
  }
  return { types, unresolved };
}

describe('published event types, derived from services/go-api at test time', () => {
  it('finds the Go source tree to derive the contract from', () => {
    expect(GO_API_ROOT).not.toBeNull();
  });

  it('resolves every event type a publisher names, so none can go underived', () => {
    const { unresolved } = derivePublishedEventTypes(GO_API_ROOT!);

    expect([...unresolved]).toEqual([]);
  });

  it('matches SERVER_EVENT_TYPES exactly, with no name unpublished and none unhandled', () => {
    const { types } = derivePublishedEventTypes(GO_API_ROOT!);

    expect(types.size).toBeGreaterThan(0);
    expect([...types].sort()).toEqual([...SERVER_EVENT_TYPES].sort());
  });
});
