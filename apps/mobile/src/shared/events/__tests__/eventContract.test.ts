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

const PUBLISH_LITERAL = /\.Publish\(\s*[\w.]+,\s*"([a-zA-Z_]+)"/g;
const SSE_LITERAL_EVENT_LINE = /"event:\s*([a-zA-Z_]+)\\n/g;

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

function derivePublishedEventTypes(root: string): Set<string> {
  const types = new Set<string>();
  for (const file of listGoSourceFiles(root)) {
    const text = fs.readFileSync(file, 'utf8');
    for (const match of text.matchAll(PUBLISH_LITERAL)) types.add(match[1]!);
    for (const match of text.matchAll(SSE_LITERAL_EVENT_LINE)) types.add(match[1]!);
  }
  return types;
}

describe('published event types, derived from services/go-api at test time', () => {
  it('finds the Go source tree to derive the contract from', () => {
    expect(GO_API_ROOT).not.toBeNull();
  });

  it('matches SERVER_EVENT_TYPES exactly, with no name unpublished and none unhandled', () => {
    const published = derivePublishedEventTypes(GO_API_ROOT!);

    expect(published.size).toBeGreaterThan(0);
    expect([...published].sort()).toEqual([...SERVER_EVENT_TYPES].sort());
  });
});
