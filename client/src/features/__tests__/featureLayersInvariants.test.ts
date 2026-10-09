import { existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync } from 'fs';
import { tmpdir } from 'os';
import { join } from 'path';

const FEATURES_ROOT = join(__dirname, '..');
const IMPORT_STATEMENT = /^\s*(import|export)\s+(type\s+)?([^;]*?\s+from\s+)?['"]([^'"]+)['"]/gms;
const FORBIDDEN_VALUE_SOURCE =
  /^(?!@shared\/ui\/(asyncView|displayFormat)$)(react|react-native|expo(-.*)?|lucide-react-native|@shared\/ui|.*\/(hooks|ui|adapters)(\/.*)?)$/;

const DISCOVER_DOMAIN_FILE_COUNT = 9;

function domainFiles(featuresRoot: string = FEATURES_ROOT): { feature: string; file: string }[] {
  return readdirSync(featuresRoot, { withFileTypes: true })
    .filter((entry) => entry.isDirectory() && entry.name !== '__tests__')
    .flatMap((entry) => {
      const dir = join(featuresRoot, entry.name, 'domain');
      if (!existsSync(dir)) return [];
      return readdirSync(dir)
        .filter((name) => /\.(ts|tsx)$/.test(name))
        .map((file) => ({ feature: entry.name, file }));
    });
}

function forbiddenValueImports(source: string): string[] {
  return [...source.matchAll(IMPORT_STATEMENT)]
    .filter((match) => match[2] === undefined)
    .map((match) => match[4] ?? '')
    .filter((specifier) => FORBIDDEN_VALUE_SOURCE.test(specifier));
}

describe('feature domain layers — pure rules import no framework, UI or outer layer', () => {
  const files = domainFiles();

  it('finds domain files across features, so a broken walk cannot pass by finding nothing', () => {
    expect(files.length).toBeGreaterThanOrEqual(8);
  });

  it.each(files.map(({ feature, file }) => [`${feature}/domain/${file}`, feature, file]))(
    '%s has no value import from a framework or outer layer',
    (_name, feature, file) => {
      const source = readFileSync(join(FEATURES_ROOT, feature!, 'domain', file!), 'utf8');
      expect({ file: _name, forbidden: forbiddenValueImports(source) }).toEqual({
        file: _name,
        forbidden: [],
      });
    },
  );

  it('covers all 9 discovery domain files', () => {
    expect(files.filter(({ feature }) => feature === 'discovery')).toHaveLength(
      DISCOVER_DOMAIN_FILE_COUNT,
    );
  });

  it('covers the 15 playback domain files', () => {
    expect(
      files
        .filter(({ feature }) => feature === 'playback')
        .map(({ file }) => file)
        .sort(),
    ).toEqual(
      [
        'clamp.ts',
        'derivePlaybackState.ts',
        'loadToken.ts',
        'lyricsSync.ts',
        'playbackError.ts',
        'playbackHealth.ts',
        'playerStatus.ts',
        'playsThroughTrackPlayer.ts',
        'playsThroughTrackPlayer.web.ts',
        'presignWindow.ts',
        'queueResume.ts',
        'queueStateWire.ts',
        'signals.ts',
        'time.ts',
        'upNextQueue.ts',
      ].sort(),
    );
  });

  it('covers the 4 settings domain files', () => {
    expect(
      files
        .filter(({ feature }) => feature === 'settings')
        .map(({ file }) => file)
        .sort(),
    ).toEqual([
      'backfillStatus.ts',
      'dangerZoneActions.ts',
      'downloadStatsModel.ts',
      'failureCopyForAction.ts',
    ]);
  });

  it('adds no cases and does not throw for a feature with no domain/ folder', () => {
    const root = mkdtempSync(join(tmpdir(), 'features-'));
    mkdirSync(join(root, 'bare'));
    expect(domainFiles(root)).toEqual([]);
  });

  it('flags a value import of react but allows the same import as a type', () => {
    expect(forbiddenValueImports("import { useMemo } from 'react';")).toEqual(['react']);
    expect(forbiddenValueImports("import type { ReactNode } from 'react';")).toEqual([]);
    expect(forbiddenValueImports("import { x } from '../hooks/useThing';")).toEqual([
      '../hooks/useThing',
    ]);
  });

  it('allows a zustand value import, which is why only react-using stores leave domain', () => {
    expect(forbiddenValueImports("import { create } from 'zustand';")).toEqual([]);
  });

  it('tells a look-alike path from an outer-layer import', () => {
    expect(
      forbiddenValueImports("import type { X } from '@features/discovery/hooks/useX';"),
    ).toEqual([]);
    expect(forbiddenValueImports("import { X } from '@features/discovery/hooks/useX';")).toEqual([
      '@features/discovery/hooks/useX',
    ]);
    expect(forbiddenValueImports("import { x } from './uiText';")).toEqual([]);
  });
});
