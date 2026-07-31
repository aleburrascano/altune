import * as fs from 'fs';
import * as path from 'path';

import { featuredArtistsFromExtras } from '../featured';

function findRepoRoot(): string | null {
  let dir = __dirname;
  for (let hop = 0; hop < 12; hop += 1) {
    const candidate = path.join(dir, 'services', 'go-api');
    if (fs.existsSync(candidate)) return dir;
    const parent = path.dirname(dir);
    if (parent === dir) break;
    dir = parent;
  }
  return null;
}

const REPO_ROOT = findRepoRoot();

function readGoSource(relativePath: string): string {
  return fs.readFileSync(path.join(REPO_ROOT!, relativePath), 'utf8');
}

function readTsSource(relativePath: string): string {
  return fs.readFileSync(path.join(REPO_ROOT!, 'apps', 'mobile', relativePath), 'utf8');
}

function extractBraceBlock(source: string, header: RegExp): string {
  const match = source.match(header);
  if (!match) throw new Error(`could not find block matching ${header}`);
  return match[1]!;
}

function deriveGoExtrasMapEmittedKeys(source: string): Set<string> {
  const body = extractBraceBlock(
    source,
    /func \(f FeaturedArtist\) ToExtrasMap\(\) map\[string\]any \{([\s\S]*?)\n\}/,
  );
  const keys = new Set<string>();
  for (const match of body.matchAll(/"(\w+)":\s*f\./g)) keys.add(match[1]!);
  for (const match of body.matchAll(/m\["(\w+)"\]\s*=/g)) keys.add(match[1]!);
  return keys;
}

function deriveGoFeaturedArtistDTOKeys(source: string): Set<string> {
  const body = extractBraceBlock(source, /type FeaturedArtistDTO struct \{([\s\S]*?)\n\}/);
  const keys = new Set<string>();
  for (const match of body.matchAll(/json:"(\w+)/g)) keys.add(match[1]!);
  return keys;
}

function deriveTsFeaturedArtistTypeKeys(source: string): Set<string> {
  const body = extractBraceBlock(source, /export type FeaturedArtist = \{([\s\S]*?)\n\};/);
  const keys = new Set<string>();
  for (const match of body.matchAll(/^\s*(\w+):/gm)) keys.add(match[1]!);
  return keys;
}

function deriveTsReadKeysAtRuntime(): Set<string> {
  const accessed = new Set<string>();
  const probe = new Proxy(
    {},
    {
      get(_target, prop) {
        if (typeof prop === 'string') accessed.add(prop);
        return prop === 'name' ? 'probe-name' : undefined;
      },
    },
  );
  featuredArtistsFromExtras([probe]);
  return accessed;
}

describe('cross-surface contract: services/go-api ToExtrasMap <-> featuredArtistsFromExtras', () => {
  it('finds the repo root to derive both sides of the contract from', () => {
    expect(REPO_ROOT).not.toBeNull();
  });

  it('reads only the keys ToExtrasMap actually emits, plus a documented known gap on "role"', () => {
    const goSource = readGoSource(
      path.join('services', 'go-api', 'internal', 'discovery', 'domain', 'featured_artist.go'),
    );
    const goEmittedKeys = deriveGoExtrasMapEmittedKeys(goSource);
    expect(goEmittedKeys.size).toBeGreaterThan(0);

    const tsReadKeys = deriveTsReadKeysAtRuntime();
    expect(tsReadKeys.size).toBeGreaterThan(0);

    for (const key of tsReadKeys) {
      expect(goEmittedKeys.has(key)).toBe(true);
    }

    const unread = new Set([...goEmittedKeys].filter((key) => !tsReadKeys.has(key)));
    expect(unread).toEqual(new Set(['role']));
  });
});

describe('cross-surface contract: FeaturedArtist (TS write side) <-> FeaturedArtistDTO (Go CreateTrackRequest ingest)', () => {
  it('finds the repo root to derive both sides of the contract from', () => {
    expect(REPO_ROOT).not.toBeNull();
  });

  it('the mobile client can only send keys the Go ingest DTO declares, and vice versa', () => {
    const goDtoSource = readGoSource(
      path.join('services', 'go-api', 'internal', 'catalog', 'service', 'track_dto.go'),
    );
    const goDtoKeys = deriveGoFeaturedArtistDTOKeys(goDtoSource);
    expect(goDtoKeys.size).toBeGreaterThan(0);

    const tsTypeSource = readTsSource(path.join('src', 'shared', 'api-client', 'types.ts'));
    const tsTypeKeys = deriveTsFeaturedArtistTypeKeys(tsTypeSource);
    expect(tsTypeKeys.size).toBeGreaterThan(0);

    expect([...tsTypeKeys].sort()).toEqual([...goDtoKeys].sort());
  });
});
