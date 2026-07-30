import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';

import { downloadPinned, pinnedDir } from '../pinnedFiles';

const OFFLINE_DIR = path.resolve(__dirname, '..');
const DOCUMENT_ROOT = 'file:///document';
const CACHE_ROOT = 'file:///cache';

const EXPO_FILE_SYSTEM_IMPORT = /from\s+['"]expo-file-system['"]/;
const CACHE_WORD = /\bcache\b/i;

function listSourceFiles(dir: string): string[] {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    if (entry.name === '__tests__' || entry.name === 'node_modules') return [];
    const fullPath = path.join(dir, entry.name);
    if (entry.isDirectory()) return listSourceFiles(fullPath);
    if (!/\.(ts|tsx)$/.test(entry.name)) return [];
    return [fullPath];
  });
}

function findCacheRootReferences(dir: string): string[] {
  return listSourceFiles(dir).filter((file) => {
    const source = fs.readFileSync(file, 'utf8');
    return EXPO_FILE_SYSTEM_IMPORT.test(source) && CACHE_WORD.test(source);
  });
}

function withTempFixtureDir(build: (dir: string) => void, run: (dir: string) => void): void {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'offline-invariant-'));
  try {
    build(dir);
    run(dir);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
}

describe('pinned audio lives under the document root, never the cache root', () => {
  it('no source file in shared/offline references the cache root', () => {
    const offenders = findCacheRootReferences(OFFLINE_DIR);

    expect(offenders).toEqual([]);
  });

  it.each([
    [
      'a direct Paths.cache reference',
      "import { Paths } from 'expo-file-system';\n" + 'export const root = Paths.cache;\n',
    ],
    [
      'a destructured binding',
      "import { Paths } from 'expo-file-system';\n" +
        'const { cache } = Paths;\n' +
        'export const root = cache;\n',
    ],
    [
      'a renamed destructured binding',
      "import { Paths } from 'expo-file-system';\n" +
        'const { cache: root } = Paths;\n' +
        'export const dir = root;\n',
    ],
    [
      'a bracket-access reference',
      "import { Paths } from 'expo-file-system';\n" + "export const root = Paths['cache'];\n",
    ],
  ])('flags a fixture file expressing the cache root as %s', (_spelling, source) => {
    withTempFixtureDir(
      (dir) => {
        fs.writeFileSync(path.join(dir, 'sneakyCache.ts'), source);
      },
      (dir) => {
        const offenders = findCacheRootReferences(dir);

        expect(offenders).toEqual([path.join(dir, 'sneakyCache.ts')]);
      },
    );
  });

  it('pinnedDir() resolves under the document root, not the cache root', () => {
    const dir = pinnedDir();

    expect(dir.uri.startsWith(DOCUMENT_ROOT)).toBe(true);
    expect(dir.uri.startsWith(CACHE_ROOT)).toBe(false);
  });

  it('downloadPinned resolves a uri under the document root, not the cache root', async () => {
    const uri = await downloadPinned('t1', 'https://cdn.example.com/audio/t1.mp3');

    expect(uri.startsWith(DOCUMENT_ROOT)).toBe(true);
    expect(uri.startsWith(CACHE_ROOT)).toBe(false);
  });
});
