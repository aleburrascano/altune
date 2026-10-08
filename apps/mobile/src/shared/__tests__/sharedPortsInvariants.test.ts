import * as fs from 'node:fs';
import * as path from 'node:path';

const SRC_ROOT = path.join(__dirname, '..', '..');
if (!fs.existsSync(path.join(SRC_ROOT, 'shared', 'playlists'))) {
  throw new Error(`expected ${SRC_ROOT} to be apps/mobile/src — anchor is __dirname, not the cwd`);
}

const PORT_MODULES = ['playlists', 'acquisition', 'favorites', 'offline'] as const;
const API_CLIENT = '@shared/api-client';
const IDS_SPECIFIER = '@shared/api-client/ids';

function listSourceFiles(dir: string): string[] {
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

function isApiClientSpecifier(spec: string): boolean {
  return spec === API_CLIENT || spec.startsWith(`${API_CLIENT}/`);
}

function stripComments(source: string): string {
  return source.replace(/\/\*[\s\S]*?\*\//g, '').replace(/^\s*\/\/.*$/gm, '');
}

const FROM_STATEMENT = /\b(?:import|export)\s+(type\s+)?([\w\s{},*$]*?)\s*from\s*['"]([^'"]+)['"]/g;
const SIDE_EFFECT_IMPORT = /\bimport\s*['"]([^'"]+)['"]/g;
const CALL_LOAD = /\b(?:import|require)\s*\(\s*['"]([^'"]+)['"]\s*\)/g;

function clauseIsTypeOnly(clause: string): boolean {
  const open = clause.indexOf('{');
  const close = clause.lastIndexOf('}');
  if (open === -1 || close === -1) return false;
  const bindingBeforeBraces = clause.slice(0, open).replace(/,/g, '').trim();
  if (bindingBeforeBraces) return false;
  const named = clause
    .slice(open + 1, close)
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean);
  return named.length > 0 && named.every((s) => /^type\s+\S/.test(s));
}

export function apiClientValueImports(source: string): string[] {
  const code = stripComments(source);
  const offenders: string[] = [];
  const consider = (spec: string) => {
    if (isApiClientSpecifier(spec) && spec !== IDS_SPECIFIER) offenders.push(spec);
  };
  for (const [, typeKeyword, clause, spec] of code.matchAll(FROM_STATEMENT)) {
    if (typeKeyword || clauseIsTypeOnly(clause!)) continue;
    consider(spec!);
  }
  for (const [, spec] of code.matchAll(SIDE_EFFECT_IMPORT)) consider(spec!);
  for (const [, spec] of code.matchAll(CALL_LOAD)) consider(spec!);
  return offenders;
}

function isAdapterPath(file: string): boolean {
  return file.split(path.sep).join('/').includes('/adapters/');
}

function offendersIn(file: string, source: string): string[] {
  return isAdapterPath(file) ? [] : apiClientValueImports(source);
}

const NON_ADAPTER = 'shared/playlists/ui/x.tsx';
const ADAPTER = 'shared/playlists/adapters/http/x.ts';

describe('apiClientValueImports', () => {
  it.each([
    ['named', "import { getPlaylists } from '@shared/api-client/playlists';"],
    ['bare barrel', "import { apiFetch } from '@shared/api-client';"],
    ['multi-line', "import {\n  retryAcquisition,\n} from '@shared/api-client/tracks';"],
    ['side-effect', "import '@shared/api-client/audio';"],
    ['re-export named', "export { getPlaylists } from '@shared/api-client/playlists';"],
    ['re-export star', "export * from '@shared/api-client';"],
    ['dynamic import', "const m = await import('@shared/api-client/tracks');"],
    ['require', "const m = require('@shared/api-client/tracks');"],
    ['default plus type named', "import api, { type Foo } from '@shared/api-client';"],
    ['mixed type and value', "import { type A, b } from '@shared/api-client/tracks';"],
    ['ids look-alike', "import { x } from '@shared/api-client/idsHttp';"],
    ['ids subpath', "import { x } from '@shared/api-client/ids/extra';"],
  ])('flags %s', (_name, source) => {
    expect(apiClientValueImports(source)).toHaveLength(1);
    expect(offendersIn(NON_ADAPTER, source)).toHaveLength(1);
    expect(offendersIn(ADAPTER, source)).toEqual([]);
  });

  it.each([
    ['empty source', ''],
    ['import type', "import type { PlaylistResponse } from '@shared/api-client/types';"],
    ['import type namespace', "import type * as tracks from '@shared/api-client/tracks';"],
    ['all inline type', "import { type A, type B } from '@shared/api-client/types';"],
    ['export type', "export type { X } from '@shared/api-client/types';"],
    ['ids value', "import { parseTrackId } from '@shared/api-client/ids';"],
    [
      'ids mixed with type',
      "import { isSafeId, parseTrackId, type TrackId } from '@shared/api-client/ids';",
    ],
    ['unrelated package', "import { foo } from '@shared/api-clientele';"],
    ['commented out', "// import { x } from '@shared/api-client/tracks';"],
  ])('allows %s', (_name, source) => {
    expect(apiClientValueImports(source)).toEqual([]);
  });

  it('does not run an export statement into a following import', () => {
    const source =
      "export const a = 1;\nimport type { T } from '@shared/api-client/tracks';\nexport const b = 2;";
    expect(apiClientValueImports(source)).toEqual([]);
  });

  it('tracks edits to a source', () => {
    const clean = "import type { T } from '@shared/api-client/tracks';";
    expect(apiClientValueImports(clean)).toHaveLength(0);
    const dirty = `${clean}\nimport { retryAcquisition } from '@shared/api-client/tracks';`;
    expect(apiClientValueImports(dirty)).toHaveLength(1);
  });
});

describe('real tree', () => {
  const scanned = PORT_MODULES.map((name) => {
    const files = listSourceFiles(path.join(SRC_ROOT, 'shared', name));
    return { name, files };
  });
  const allFiles = scanned.flatMap((m) => m.files);
  const rel = (file: string) =>
    path.relative(path.join(SRC_ROOT, 'shared'), file).split(path.sep).join('/');

  it.each(PORT_MODULES)('finds source files in %s', (name) => {
    expect(scanned.find((m) => m.name === name)!.files.length).toBeGreaterThan(0);
  });

  it('scans the live ids importers and the exempt adapter', () => {
    const relFiles = allFiles.map(rel);
    for (const f of ['pinnedFiles', 'pinnedStore', 'pinnedIndex']) {
      const file = allFiles.find((p) => rel(p) === `offline/${f}.ts`);
      expect(file).toBeDefined();
      expect(fs.readFileSync(file!, 'utf8')).toContain(IDS_SPECIFIER);
    }
    expect(relFiles).toContain('acquisition/adapters/http/httpAcquisitionPort.ts');
  });

  it('has no value imports of the api-client outside adapters', () => {
    const offenders = allFiles.flatMap((file) =>
      offendersIn(file, fs.readFileSync(file, 'utf8')).map((spec) => `${rel(file)} -> ${spec}`),
    );
    expect(offenders).toEqual([]);
  });
});
