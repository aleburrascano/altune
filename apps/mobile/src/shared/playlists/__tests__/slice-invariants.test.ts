import * as fs from 'node:fs';
import * as path from 'node:path';

const SRC_ROOT = path.join(__dirname, '..', '..', '..');
if (!fs.existsSync(path.join(SRC_ROOT, 'shared', 'playlists'))) {
  throw new Error(`expected ${SRC_ROOT} to be apps/mobile/src — anchor is __dirname, not the cwd`);
}
const SLICE_DIR = path.join(SRC_ROOT, 'shared', 'playlists');
const FEATURES_ROOT = path.join(SRC_ROOT, 'features');
const APP_ROOT = path.join(SRC_ROOT, 'app');

function listSourceFiles(dir: string): string[] {
  const files: string[] = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === '__tests__' || entry.name === 'node_modules' || entry.name === '_template') continue;
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      files.push(...listSourceFiles(full));
    } else if (/\.tsx?$/.test(entry.name) && !entry.name.endsWith('.d.ts')) {
      files.push(full);
    }
  }
  return files;
}

function readAll(files: string[]): { file: string; source: string }[] {
  return files.map((file) => ({ file, source: fs.readFileSync(file, 'utf8') }));
}

const IMPORT_STATEMENT = /import\s+(type\s+)?[^;\n]*?from\s+['"]([^'"]+)['"]/g;

function importSpecifiers(source: string, includeTypeOnly: boolean): string[] {
  const specs: string[] = [];
  for (const match of source.matchAll(IMPORT_STATEMENT)) {
    const [, isTypeOnly, spec] = match;
    if (isTypeOnly && !includeTypeOnly) continue;
    specs.push(spec!);
  }
  return specs;
}

function namedValueImportsFrom(source: string, moduleSpecifier: string): string[] {
  const names: string[] = [];
  const re = new RegExp(`import\\s+(type\\s+)?\\{([^}]*)\\}\\s+from\\s+['"]${moduleSpecifier}['"]`, 'g');
  for (const match of source.matchAll(re)) {
    if (match[1]) continue;
    for (const raw of match[2]!.split(',')) {
      const name = raw.trim().split(/\s+as\s+/)[0]!.trim();
      if (name) names.push(name);
    }
  }
  return names;
}

function exportedFunctionNames(source: string): string[] {
  const names: string[] = [];
  for (const match of source.matchAll(/export function (\w+)/g)) names.push(match[1]!);
  for (const match of source.matchAll(/export const (\w+)/g)) names.push(match[1]!);
  return names;
}

function words(source: string): string[] {
  return source.split(/[^a-zA-Z0-9]+|(?<=[a-z0-9])(?=[A-Z])/).filter((word) => word.length > 0);
}

function bannedNounViolations(source: string): string[] {
  return words(source).filter((word) => /^songs?$/i.test(word));
}

describe('sanity: this suite is actually scanning the real slice', () => {
  it('shared/playlists is found under process.cwd()/src and contains mutations.ts, index.ts and the two component files', () => {
    const names = listSourceFiles(SLICE_DIR)
      .map((file) => path.basename(file))
      .sort();
    expect(names).toEqual(['AddToPlaylistSheet.tsx', 'CreatePlaylistModal.tsx', 'index.ts', 'mutations.ts']);
  });
});

describe('rule 1 — every membership write is a batch, never a singular-track hook', () => {
  const mutationsSource = fs.readFileSync(path.join(SLICE_DIR, 'mutations.ts'), 'utf8');
  const hookNames = exportedFunctionNames(mutationsSource);

  function singularMembershipHooks(names: string[]): string[] {
    return names.filter((name) => /(Add|Remove)/.test(name) && /Track(?!s)/.test(name));
  }

  it('mutations.ts exports the membership hooks (sanity: the scan actually found them)', () => {
    expect(hookNames).toEqual(
      expect.arrayContaining(['useAddTracksToPlaylist', 'useRemoveTracksFromPlaylist']),
    );
  });

  it('no exported hook is a singular-track membership variant', () => {
    expect(singularMembershipHooks(hookNames)).toEqual([]);
  });

  it('the singular-variant filter fires on a hook shaped like useAddTrackToPlaylist, and leaves the batch hooks alone', () => {
    expect(
      singularMembershipHooks([
        'useAddTrackToPlaylist',
        'useRemoveTrackFromPlaylist',
        'useAddTracksToPlaylist',
        'useRemoveTracksFromPlaylist',
        'useCreatePlaylist',
      ]),
    ).toEqual(['useAddTrackToPlaylist', 'useRemoveTrackFromPlaylist']);
  });

  it('mutations.ts never types a membership write with a bare singular track id (trackId, not trackIds)', () => {
    expect(mutationsSource.match(/\btrackId\b/g)).toBeNull();
  });

  it('the singular-trackId check fires on a fixture that reintroduces one', () => {
    const withSingular = 'type AddTrackVariables = { playlistId: string; trackId: string };';
    expect(withSingular.match(/\btrackId\b/g)).toEqual(['trackId']);
  });
});

describe('rule 2 — cache keys come from playlistKeys, never a retyped literal', () => {
  const sliceFiles = readAll(listSourceFiles(SLICE_DIR));
  const KEY_LITERAL = /\[\s*['"]playlists?['"]/;

  it('scanned at least one slice source file (sanity)', () => {
    expect(sliceFiles.length).toBeGreaterThan(0);
  });

  it('no slice file hand-writes a playlist cache-key array literal', () => {
    const offenders = sliceFiles
      .filter(({ source }) => KEY_LITERAL.test(source))
      .map(({ file }) => path.basename(file));
    expect(offenders).toEqual([]);
  });

  it('the literal-key detector fires on ["playlists"] and ["playlist", id] shaped fixtures, not on a playlistKeys reference', () => {
    expect(KEY_LITERAL.test("const key = ['playlists'];")).toBe(true);
    expect(KEY_LITERAL.test('const key = ["playlist", id];')).toBe(true);
    expect(KEY_LITERAL.test('queryKey: playlistKeys.list')).toBe(false);
  });

  it('every react-query queryKey in the slice resolves through playlistKeys, never an inline array', () => {
    const offenders = sliceFiles
      .filter(({ source }) => /queryKey:\s*\[/.test(source))
      .map(({ file }) => path.basename(file));
    expect(offenders).toEqual([]);
  });

  it('the inline-queryKey detector fires on queryKey: [literal]', () => {
    expect(/queryKey:\s*\[/.test("queryKey: ['playlists']")).toBe(true);
    expect(/queryKey:\s*\[/.test('queryKey: playlistKeys.list')).toBe(false);
  });
});

describe('rule 3 — no feature imports from shared/playlists', () => {
  const sliceFiles = readAll(listSourceFiles(SLICE_DIR));

  function featureImportViolations(source: string): string[] {
    return importSpecifiers(source, true).filter(
      (spec) => spec.startsWith('@features/') || /(^|\/)features(\/|$)/.test(spec),
    );
  }

  it('scanned at least one slice source file (sanity)', () => {
    expect(sliceFiles.length).toBeGreaterThan(0);
  });

  it('no slice file imports from @features or reaches a feature by relative path', () => {
    const offenders = sliceFiles
      .map(({ file, source }) => ({
        file: path.basename(file),
        violations: featureImportViolations(source),
      }))
      .filter(({ violations }) => violations.length > 0);
    expect(offenders).toEqual([]);
  });

  it('the feature-import detector fires on the alias form, the relative form, and even a type-only import', () => {
    expect(featureImportViolations("import { X } from '@features/library';")).toEqual(['@features/library']);
    expect(featureImportViolations("import { X } from '../../features/library/hooks';")).toEqual([
      '../../features/library/hooks',
    ]);
    expect(featureImportViolations("import type { X } from '@features/library';")).toEqual([
      '@features/library',
    ]);
    expect(featureImportViolations("import { X } from '@shared/lib/query-keys';")).toEqual([]);
  });
});

describe('rule 4 — components never call playlist write functions directly; only mutations.ts does', () => {
  const WRITE_FUNCTIONS = [
    'createPlaylist',
    'addTracksToPlaylist',
    'removeTracksFromPlaylist',
    'renamePlaylist',
    'deletePlaylist',
    'reorderPlaylistTracks',
  ];
  const componentFiles = listSourceFiles(SLICE_DIR).filter((file) => file.endsWith('.tsx'));

  it('found the slice component files (sanity)', () => {
    expect(componentFiles.map((file) => path.basename(file)).sort()).toEqual([
      'AddToPlaylistSheet.tsx',
      'CreatePlaylistModal.tsx',
    ]);
  });

  it('no component imports a playlist writer from @shared/api-client/playlists', () => {
    const offenders = componentFiles
      .map((file) => ({
        file: path.basename(file),
        writers: namedValueImportsFrom(
          fs.readFileSync(file, 'utf8'),
          '@shared/api-client/playlists',
        ).filter((name) => WRITE_FUNCTIONS.includes(name)),
      }))
      .filter(({ writers }) => writers.length > 0);
    expect(offenders).toEqual([]);
  });

  it('AddToPlaylistSheet is allowed to call the read function getPlaylists, and only that one', () => {
    const sheetSource = fs.readFileSync(path.join(SLICE_DIR, 'AddToPlaylistSheet.tsx'), 'utf8');
    expect(namedValueImportsFrom(sheetSource, '@shared/api-client/playlists')).toEqual(['getPlaylists']);
  });

  it('the writer-import detector fires when a component imports createPlaylist directly', () => {
    const violating = "import { createPlaylist, getPlaylists } from '@shared/api-client/playlists';";
    expect(
      namedValueImportsFrom(violating, '@shared/api-client/playlists').filter((name) =>
        WRITE_FUNCTIONS.includes(name),
      ),
    ).toEqual(['createPlaylist']);
  });
});

describe('rule 5 — the barrel is the public surface', () => {
  const indexSource = fs.readFileSync(path.join(SLICE_DIR, 'index.ts'), 'utf8');
  const otherSliceFiles = listSourceFiles(SLICE_DIR).filter((file) => path.basename(file) !== 'index.ts');

  it('found source files besides the barrel to check against it (sanity)', () => {
    expect(otherSliceFiles.length).toBeGreaterThan(0);
  });

  it('index.ts re-exports every top-level symbol the other slice files define', () => {
    const missing: string[] = [];
    for (const file of otherSliceFiles) {
      const names = exportedFunctionNames(fs.readFileSync(file, 'utf8'));
      for (const name of names) {
        if (!new RegExp(`\\b${name}\\b`).test(indexSource)) missing.push(`${path.basename(file)}:${name}`);
      }
    }
    expect(missing).toEqual([]);
  });

  it('the missing-export check fires when the barrel omits a symbol the slice defines', () => {
    const thinBarrel = "export { useCreatePlaylist } from './mutations';";
    expect(/\buseAddTracksToPlaylist\b/.test(thinBarrel)).toBe(false);
  });

  function deepImportViolations(source: string): string[] {
    return importSpecifiers(source, true).filter(
      (spec) => spec.startsWith('@shared/playlists/') || /(^|\/)shared\/playlists\/[^'"]+$/.test(spec),
    );
  }

  it('found consumer files to scan across features and app (sanity)', () => {
    const consumerFiles = [...listSourceFiles(FEATURES_ROOT), ...listSourceFiles(APP_ROOT)];
    expect(consumerFiles.length).toBeGreaterThan(0);
  });

  it('no file under src/features or src/app reaches into a slice file directly — every consumer imports from @shared/playlists itself', () => {
    const consumerFiles = [...listSourceFiles(FEATURES_ROOT), ...listSourceFiles(APP_ROOT)];
    const offenders: string[] = [];
    for (const file of consumerFiles) {
      const source = fs.readFileSync(file, 'utf8');
      for (const spec of deepImportViolations(source)) {
        offenders.push(`${path.relative(SRC_ROOT, file)}: ${spec}`);
      }
    }
    expect(offenders).toEqual([]);
  });

  it('the deep-import detector fires on @shared/playlists/mutations and a relative equivalent, not on the bare barrel specifier', () => {
    expect(deepImportViolations("import { useCreatePlaylist } from '@shared/playlists/mutations';")).toEqual([
      '@shared/playlists/mutations',
    ]);
    expect(
      deepImportViolations("import { useCreatePlaylist } from '../../../shared/playlists/mutations';"),
    ).toEqual(['../../../shared/playlists/mutations']);
    expect(deepImportViolations("import { AddToPlaylistSheet } from '@shared/playlists';")).toEqual([]);
  });
});

describe('rule 6 — the banned noun never appears in this slice', () => {
  const sliceFiles = readAll(listSourceFiles(SLICE_DIR));

  it('scanned exactly the slice\'s four source modules (sanity)', () => {
    expect(sliceFiles.map(({ file }) => path.basename(file)).sort()).toEqual([
      'AddToPlaylistSheet.tsx',
      'CreatePlaylistModal.tsx',
      'index.ts',
      'mutations.ts',
    ]);
  });

  it('no identifier or string literal in mutations.ts, AddToPlaylistSheet.tsx, CreatePlaylistModal.tsx or index.ts contains "song"', () => {
    const offenders = sliceFiles
      .filter(({ source }) => bannedNounViolations(source).length > 0)
      .map(({ file }) => path.basename(file));
    expect(offenders).toEqual([]);
  });

  it('flags "song" standalone, in camelCase, in PascalCase, in SCREAMING_SNAKE_CASE and pluralized', () => {
    expect(bannedNounViolations('const song = current;')).toEqual(['song']);
    expect(bannedNounViolations('const songId = current.id;')).toEqual(['song']);
    expect(bannedNounViolations('type SongTitle = string;')).toEqual(['Song']);
    expect(bannedNounViolations('const SONG_ID = 1;')).toEqual(['SONG']);
    expect(bannedNounViolations('const songs = [];')).toEqual(['songs']);
  });

  it('does not flag a legitimate word that merely contains "song" as a substring', () => {
    expect(bannedNounViolations('const songwriter = credit.name;')).toEqual([]);
    expect(bannedNounViolations('const songbird = "decoy";')).toEqual([]);
  });
});

describe('rule 5b — the barrel is executable, not just textually correct', () => {
  it('re-exports every documented symbol as a live binding, so a consumer importing the slice root gets working values', () => {
    const barrel = require('../index');
    const exported = Object.keys(barrel).sort();

    expect(exported).toEqual([
      'AddToPlaylistSheet',
      'CreatePlaylistModal',
      'useAddTracksToPlaylist',
      'useCreatePlaylist',
      'useCreatePlaylistWithTracks',
      'useDeletePlaylist',
      'useRemoveTracksFromPlaylist',
      'useRenamePlaylist',
    ]);
    for (const name of exported) {
      expect(typeof barrel[name as keyof typeof barrel]).toBe('function');
    }
  });
});
