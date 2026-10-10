import * as fs from 'node:fs';
import * as path from 'node:path';

const featuresRoot = path.join(__dirname, '..');
const API_CLIENT_SOURCE =
  /(?:^|\/)shared\/apiClient(?:\/([^/]+))?$|^@shared\/apiClient(?:\/([^/]+))?$/;
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
    const match = API_CLIENT_SOURCE.exec(source);
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

const portedFeatures = fs
  .readdirSync(featuresRoot, { withFileTypes: true })
  .filter(
    (entry) =>
      entry.isDirectory() && fs.existsSync(path.join(featuresRoot, entry.name, 'adapters')),
  )
  .map((entry) => entry.name);
const files = portedFeatures.flatMap(featureFiles);

test('finds features with an adapters/ folder, so a broken walk cannot pass by finding nothing', () => {
  expect(portedFeatures.length).toBeGreaterThan(0);
});

test.each(portedFeatures)('%s has source files to check', (feature) => {
  expect(files.filter(({ name }) => name.startsWith(`${feature}/`)).length).toBeGreaterThan(0);
});

test('the import scan flags a value import and spares type and pure-module imports', () => {
  expect(apiClientValueImports("import { getTracks } from '@shared/apiClient';")).toHaveLength(1);
  expect(
    apiClientValueImports("import { suggestDiscovery } from '@shared/apiClient/discovery';"),
  ).toHaveLength(1);
  expect(apiClientValueImports("import type { T } from '@shared/apiClient';")).toHaveLength(0);
  expect(apiClientValueImports("import { type T } from '@shared/apiClient';")).toHaveLength(0);
  expect(apiClientValueImports("import { asTrackId } from '@shared/apiClient/ids';")).toHaveLength(
    0,
  );
  expect(apiClientValueImports("import { ApiError } from '@shared/failure/errors';")).toHaveLength(
    0,
  );
});

test.each(files)(
  '$name takes nothing from the apiClient but types and pure modules',
  ({ name, text }) => {
    expect({ file: name, valueImports: apiClientValueImports(text) }).toEqual({
      file: name,
      valueImports: [],
    });
  },
);
