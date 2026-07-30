import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';

import { RESTART_THRESHOLD_MS } from '../constants';

const PLAYBACK_DIR = path.resolve(__dirname, '..');
const SRC_DIR = path.resolve(__dirname, '..', '..', '..');
const ALLOWED_QUEUE_STORE_IMPORTERS = [
  path.join(SRC_DIR, 'shared', 'playback'),
  path.join(SRC_DIR, 'features', 'playback'),
];

const ACQUISITION_READY_LITERAL = /['"]ready['"]/;
const QUEUE_STORE_REFERENCE = /\buseQueueStore\b/;

function listSourceFiles(dir: string): string[] {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    if (entry.name === '__tests__' || entry.name === 'node_modules') return [];
    const fullPath = path.join(dir, entry.name);
    if (entry.isDirectory()) return listSourceFiles(fullPath);
    if (!/\.(ts|tsx)$/.test(entry.name)) return [];
    return [fullPath];
  });
}

function findAcquisitionReadyCheckers(dir: string): string[] {
  return listSourceFiles(dir)
    .filter((file) => path.basename(file) !== 'canPlay.ts')
    .filter((file) => ACQUISITION_READY_LITERAL.test(fs.readFileSync(file, 'utf8')));
}

function findQueueStoreImporters(dir: string): string[] {
  return listSourceFiles(dir).filter((file) =>
    QUEUE_STORE_REFERENCE.test(fs.readFileSync(file, 'utf8')),
  );
}

function isUnderAnAllowedDir(file: string, allowedDirs: string[]): boolean {
  return allowedDirs.some((allowed) => file.startsWith(allowed + path.sep));
}

function withTempFixtureDir(build: (dir: string) => void, run: (dir: string) => void): void {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'playback-invariant-'));
  try {
    build(dir);
    run(dir);
  } finally {
    fs.rmSync(dir, { recursive: true, force: true });
  }
}

describe('canPlay.ts is the only place playability is checked', () => {
  it('finds no other file in shared/playback comparing an acquisition status against the ready literal', () => {
    const offenders = findAcquisitionReadyCheckers(PLAYBACK_DIR);

    expect(offenders).toEqual([]);
  });

  it.each([
    ['a direct comparison', "export const gate = (s: string) => s === 'ready';\n"],
    ['a negated comparison', "export const gate = (s: string) => s !== 'ready';\n"],
    ['the literal on the left', "export const gate = (s: string) => 'ready' === s;\n"],
    ['double quotes', 'export const gate = (s: string) => s === "ready";\n'],
    [
      'a renamed destructured binding',
      "export const gate = (t: { acquisition_status: string }) => {\n" +
        '  const { acquisition_status: s } = t;\n' +
        "  return s === 'ready';\n" +
        '};\n',
    ],
  ])('flags a fixture file expressing playability as %s', (_spelling, source) => {
    withTempFixtureDir(
      (dir) => {
        fs.writeFileSync(path.join(dir, 'sneakyGate.ts'), source);
      },
      (dir) => {
        const offenders = findAcquisitionReadyCheckers(dir);

        expect(offenders).toEqual([path.join(dir, 'sneakyGate.ts')]);
      },
    );
  });
});

describe('the restart threshold has exactly one definition', () => {
  it.each([
    path.join(SRC_DIR, 'features', 'playback', 'ui', 'FullPlayer.tsx'),
    path.join(SRC_DIR, 'features', 'playback', 'service.ts'),
  ])('%s derives from the shared constant instead of restating its value', (file) => {
    const source = fs.readFileSync(file, 'utf8');

    expect(source).toContain('RESTART_THRESHOLD_MS');
    expect(source).not.toMatch(new RegExp(`\\b${RESTART_THRESHOLD_MS}\\b`));
  });
});

describe('useQueueStore is reached only through the useQueuePlayback facade', () => {
  it('finds no importer of useQueueStore outside shared/playback and features/playback', () => {
    const offenders = findQueueStoreImporters(SRC_DIR).filter(
      (file) => !isUnderAnAllowedDir(file, ALLOWED_QUEUE_STORE_IMPORTERS),
    );

    expect(offenders).toEqual([]);
  });

  it('flags a fixture file outside the facade that reaches the store directly, proving the scan is not vacuous', () => {
    withTempFixtureDir(
      (dir) => {
        fs.writeFileSync(
          path.join(dir, 'RogueScreen.tsx'),
          "import { useQueueStore } from '@shared/playback/queueStore';\n" +
            'export const RogueScreen = () => useQueueStore.getState().currentIndex;\n',
        );
      },
      (dir) => {
        const offenders = findQueueStoreImporters(dir).filter(
          (file) => !isUnderAnAllowedDir(file, ALLOWED_QUEUE_STORE_IMPORTERS),
        );

        expect(offenders).toEqual([path.join(dir, 'RogueScreen.tsx')]);
      },
    );
  });
});
