import { existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync } from 'fs';
import { tmpdir } from 'os';
import { join } from 'path';

const FEATURES_ROOT = join(__dirname, '..');
const IMPORT_STATEMENT = /^\s*(import|export)\s+(type\s+)?([^;]*?\s+from\s+)?['"]([^'"]+)['"]/gms;
const FORBIDDEN_VALUE_SOURCE =
  /^(react|react-native|expo(-.*)?|lucide-react-native|@shared\/ui|.*\/(hooks|ui|adapters)(\/.*)?)$/;

const DISCOVER_DOMAIN_FILE_COUNT = 8;

const ROOT_FILE_CEILING: Record<string, number> = {
  auth: 12,
  detail: 13,
  discover: 0,
  library: 7,
  playback: 21,
  settings: 1,
};

function featureNames(): string[] {
  return readdirSync(FEATURES_ROOT, { withFileTypes: true })
    .filter((entry) => entry.isDirectory() && entry.name !== '__tests__')
    .map((entry) => entry.name);
}

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

function rootFileCount(feature: string): number {
  return readdirSync(join(FEATURES_ROOT, feature), { withFileTypes: true }).filter(
    (entry) => entry.isFile() && /\.(ts|tsx)$/.test(entry.name),
  ).length;
}

function forbiddenValueImports(source: string): string[] {
  return [...source.matchAll(IMPORT_STATEMENT)]
    .filter((match) => match[2] === undefined)
    .map((match) => match[4] ?? '')
    .filter((specifier) => FORBIDDEN_VALUE_SOURCE.test(specifier));
}

function ratchetProblem(feature: string, count: number, ceiling: number): string | null {
  if (count > ceiling) {
    return `${feature} has ${count} loose root files, over its ceiling of ${ceiling}: put new files in domain/ports/adapters/hooks/ui`;
  }
  if (count < ceiling) {
    return `${feature} has ${count} loose root files, under its ceiling of ${ceiling}: lower the ceiling`;
  }
  return null;
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

  it('covers all 8 discover domain files', () => {
    expect(files.filter(({ feature }) => feature === 'discover')).toHaveLength(
      DISCOVER_DOMAIN_FILE_COUNT,
    );
  });

  it('covers the 3 settings domain files', () => {
    expect(
      files.filter(({ feature }) => feature === 'settings').map(({ file }) => file).sort(),
    ).toEqual(['backfillStatus.ts', 'downloadStatsModel.ts', 'failureCopyForAction.ts']);
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

  it('tells a look-alike path from an outer-layer import', () => {
    expect(
      forbiddenValueImports("import type { X } from '@features/discover/hooks/useX';"),
    ).toEqual([]);
    expect(forbiddenValueImports("import { X } from '@features/discover/hooks/useX';")).toEqual([
      '@features/discover/hooks/useX',
    ]);
    expect(forbiddenValueImports("import { x } from './uiText';")).toEqual([]);
  });
});

describe('feature root-file ratchet — loose root files may only go down', () => {
  it.each(featureNames())('%s keeps exactly its ceiling of loose root files', (feature) => {
    const ceiling = ROOT_FILE_CEILING[feature] ?? 0;
    expect(ratchetProblem(feature, rootFileCount(feature), ceiling)).toBeNull();
  });

  it("reports discover at 0 on today's tree", () => {
    expect(rootFileCount('discover')).toBe(0);
  });

  it('fails a count of ceiling+1 and asks to lower the ceiling when the count drops', () => {
    expect(ratchetProblem('x', 9, 8)).toContain('over its ceiling');
    expect(ratchetProblem('x', 7, 8)).toContain('lower the ceiling');
    expect(ratchetProblem('x', 8, 8)).toBeNull();
    expect(ratchetProblem('newFeature', 1, ROOT_FILE_CEILING['newFeature'] ?? 0)).not.toBeNull();
  });
});
