import * as fs from 'node:fs';
import * as path from 'node:path';

const SLICE_ROOT = path.join(__dirname, '..');
const FEATURES_ROOT = path.join(__dirname, '..', '..', '..', 'features');

type FeatureFile = { relativePath: string; content: string };

function sliceModuleNames(): string[] {
  return fs
    .readdirSync(SLICE_ROOT, { withFileTypes: true })
    .filter((entry) => entry.isFile() && /\.tsx?$/.test(entry.name))
    .map((entry) => entry.name.replace(/\.tsx?$/, ''))
    .sort();
}

function readSliceFile(moduleName: string): string {
  const withTs = path.join(SLICE_ROOT, `${moduleName}.ts`);
  if (fs.existsSync(withTs)) return fs.readFileSync(withTs, 'utf8');
  return fs.readFileSync(path.join(SLICE_ROOT, `${moduleName}.tsx`), 'utf8');
}

function walkSourceFiles(dir: string): string[] {
  const out: string[] = [];
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === '__tests__' || entry.name === 'node_modules' || entry.name === '_template') {
      continue;
    }
    const fullPath = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      out.push(...walkSourceFiles(fullPath));
    } else if (/\.tsx?$/.test(entry.name)) {
      out.push(fullPath);
    }
  }
  return out;
}

function readFeatureFiles(): FeatureFile[] {
  return walkSourceFiles(FEATURES_ROOT).map((filePath) => ({
    relativePath: path.relative(FEATURES_ROOT, filePath),
    content: fs.readFileSync(filePath, 'utf8'),
  }));
}

const BANNED_RUNTIME_MODULES: ((moduleSpecifier: string) => boolean)[] = [
  (m) => m === 'react' || m.startsWith('react/'),
  (m) => m === 'react-native' || m.startsWith('react-native/'),
  (m) => m === 'expo' || m === 'expo-router' || m.startsWith('expo/') || m.startsWith('expo-'),
  (m) => m === '@tanstack/react-query' || m.startsWith('@tanstack/react-query/'),
  (m) => m === '@shared/api-client' || m.startsWith('@shared/api-client/'),
];

const IMPORT_STATEMENT = /import\s+(type\s+)?[^;\n]*?from\s+['"]([^'"]+)['"]/g;

function runtimeImportViolations(source: string): string[] {
  const violations: string[] = [];
  for (const match of source.matchAll(IMPORT_STATEMENT)) {
    const [, isTypeOnly, moduleSpecifier] = match;
    if (isTypeOnly) continue;
    if (BANNED_RUNTIME_MODULES.some((isBanned) => isBanned(moduleSpecifier!))) {
      violations.push(moduleSpecifier!);
    }
  }
  return violations;
}

function distinctFeatureConsumers(moduleName: string, files: FeatureFile[]): Set<string> {
  const specifierPattern = new RegExp(`@shared/lib/${moduleName}(?=['"/])`);
  const features = new Set<string>();
  for (const { relativePath, content } of files) {
    if (specifierPattern.test(content)) {
      features.add(relativePath.split(/[/\\]/)[0]!);
    }
  }
  return features;
}

function words(source: string): string[] {
  return source
    .split(/[^a-zA-Z0-9]+|(?<=[a-z0-9])(?=[A-Z])/)
    .filter((word) => word.length > 0);
}

function bannedNounViolations(source: string): string[] {
  return words(source).filter((word) => /^songs?$/i.test(word));
}

describe('shared/lib rule 1 — pure utility functions, no React/RN/Expo/react-query/api-client at runtime', () => {
  it.each(sliceModuleNames())('%s.ts imports no banned runtime module', (moduleName) => {
    expect(runtimeImportViolations(readSliceFile(moduleName))).toEqual([]);
  });

  it('flags a runtime import of a banned module while allowing the type-only form of the same module', () => {
    const violating = [
      "import { useState } from 'react';",
      "import { View } from 'react-native';",
      "import * as FileSystem from 'expo-file-system';",
      "import { useQuery } from '@tanstack/react-query';",
      "import { fetchTrack } from '@shared/api-client/tracks';",
    ].join('\n');
    expect(runtimeImportViolations(violating)).toEqual([
      'react',
      'react-native',
      'expo-file-system',
      '@tanstack/react-query',
      '@shared/api-client/tracks',
    ]);

    const typeOnly = [
      "import type { FeaturedArtist } from '@shared/api-client/types';",
      "import type { DiscoveryResult } from '@shared/api-client/discovery';",
    ].join('\n');
    expect(runtimeImportViolations(typeOnly)).toEqual([]);
  });
});

describe('shared/lib rule 2 — extraction requires 2+ distinct feature consumers', () => {
  const featureFiles = readFeatureFiles();

  it.each(sliceModuleNames())('%s is imported by at least two distinct features', (moduleName) => {
    const consumers = [...distinctFeatureConsumers(moduleName, featureFiles)].sort();
    if (consumers.length < 2) {
      throw new Error(
        `@shared/lib/${moduleName} has ${consumers.length} distinct feature consumer(s): ` +
          `[${consumers.join(', ')}]. Extraction to shared/ requires 2+ real consumers.`,
      );
    }
  });

  it('counts distinct feature directories, not distinct files — three files in one feature is one consumer', () => {
    const threeFilesOneFeature: FeatureFile[] = [
      { relativePath: 'detail/a.ts', content: "from '@shared/lib/format'" },
      { relativePath: 'detail/ui/b.tsx', content: "from '@shared/lib/format'" },
      { relativePath: 'detail/hooks/c.ts', content: "from '@shared/lib/format'" },
    ];
    expect(distinctFeatureConsumers('format', threeFilesOneFeature)).toEqual(new Set(['detail']));
  });

  it('a module referenced by two different features counts as two consumers', () => {
    const twoFeatures: FeatureFile[] = [
      { relativePath: 'detail/a.ts', content: "from '@shared/lib/format'" },
      { relativePath: 'library/b.ts', content: "from '@shared/lib/format'" },
    ];
    expect(distinctFeatureConsumers('format', twoFeatures)).toEqual(new Set(['detail', 'library']));
  });

  it('a similarly-named sibling module specifier does not count as a match', () => {
    const decoyOnly: FeatureFile[] = [
      { relativePath: 'detail/a.ts', content: "from '@shared/lib/query-keys'" },
      { relativePath: 'library/b.ts', content: "from '@shared/lib/query-keys-extra'" },
    ];
    expect(distinctFeatureConsumers('query-keys', decoyOnly)).toEqual(new Set(['detail']));
  });
});

describe('shared/lib rule 3 — the banned noun never appears', () => {
  it.each(sliceModuleNames())('%s.ts contains no occurrence of "song"', (moduleName) => {
    expect(bannedNounViolations(readSliceFile(moduleName))).toEqual([]);
  });

  it('flags "song" standalone, in camelCase, in PascalCase, in SCREAMING_SNAKE_CASE and pluralized', () => {
    expect(bannedNounViolations('const song = current;')).toEqual(['song']);
    expect(bannedNounViolations('const songId = current.id;')).toEqual(['song']);
    expect(bannedNounViolations('type SongTitle = string;')).toEqual(['Song']);
    expect(bannedNounViolations('const SONG_ID = 1;')).toEqual(['SONG']);
    expect(bannedNounViolations('const songs = [];')).toEqual(['songs']);
    expect(bannedNounViolations('"legacy songs table import"')).toEqual(['songs']);
  });

  it('does not flag a legitimate word that merely contains "song" as a substring', () => {
    expect(bannedNounViolations('const songwriter = credit.name;')).toEqual([]);
    expect(bannedNounViolations('const songbird = "decoy";')).toEqual([]);
  });
});
