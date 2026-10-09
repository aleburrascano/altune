import * as fs from 'node:fs';
import * as path from 'node:path';

const featuresRoot = path.join(__dirname, '..');
const PURE_MODULES = new Set([
  'featuredArtists',
  'ids',
  'trackAcquisition',
  'trackToDiscoveryResult',
  'types',
  'wireDecoders',
]);

function sourceFiles(dir: string): string[] {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      return entry.name === 'adapters' || entry.name === '__tests__' ? [] : sourceFiles(full);
    }
    return /\.tsx?$/.test(entry.name) ? [full] : [];
  });
}

function featureFiles(feature: string) {
  const root = path.join(featuresRoot, feature);
  return sourceFiles(root).map((file) => ({
    name: `${feature}/${path.relative(root, file)}`,
    text: fs.readFileSync(file, 'utf8'),
  }));
}

const IMPORT =
  /import\s+(?<typeOnly>type\s+)?(?<specifiers>[^;]*?)\s+from\s+['"](?<source>[^'"]+)['"]/g;

function apiClientValueImports(text: string): string[] {
  const offenders: string[] = [];
  for (const { groups } of text.matchAll(IMPORT)) {
    const { typeOnly, specifiers = '', source = '' } = groups ?? {};
    const match =
      /(?:^|\/)shared\/api-client(?:\/([^/]+))?$|^@shared\/api-client(?:\/([^/]+))?$/.exec(source);
    if (!match || typeOnly) continue;
    const module = match[1] ?? match[2];
    if (module && PURE_MODULES.has(module)) continue;
    const named = /^\{([\s\S]*)\}$/.exec(specifiers.trim());
    const allInlineType =
      named !== null &&
      (named[1] ?? '')
        .split(',')
        .map((part) => part.trim())
        .filter(Boolean)
        .every((part) => part.startsWith('type '));
    if (!allInlineType) offenders.push(source);
  }
  return offenders;
}

const PORTED_FEATURES = ['library', 'discovery', 'auth', 'settings', 'detail', 'playback'];
const files = PORTED_FEATURES.flatMap(featureFiles);

test.each(PORTED_FEATURES)('%s has source files to check', (feature) => {
  expect(files.filter(({ name }) => name.startsWith(`${feature}/`)).length).toBeGreaterThan(0);
});

test('the import scan flags a value import and spares type and pure-module imports', () => {
  expect(apiClientValueImports("import { getTracks } from '@shared/api-client';")).toHaveLength(1);
  expect(
    apiClientValueImports("import { suggestDiscovery } from '@shared/api-client/discovery';"),
  ).toHaveLength(1);
  expect(apiClientValueImports("import type { T } from '@shared/api-client';")).toHaveLength(0);
  expect(apiClientValueImports("import { type T } from '@shared/api-client';")).toHaveLength(0);
  expect(apiClientValueImports("import { asTrackId } from '@shared/api-client/ids';")).toHaveLength(
    0,
  );
  expect(apiClientValueImports("import { ApiError } from '@shared/failure/errors';")).toHaveLength(
    0,
  );
});

test.each(files)(
  '$name takes nothing from the api-client but types and pure modules',
  ({ name, text }) => {
    expect({ file: name, valueImports: apiClientValueImports(text) }).toEqual({
      file: name,
      valueImports: [],
    });
  },
);
