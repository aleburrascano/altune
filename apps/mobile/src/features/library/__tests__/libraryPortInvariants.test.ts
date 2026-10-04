import * as fs from 'node:fs';
import * as path from 'node:path';

const libraryRoot = path.join(__dirname, '..');
const PURE_MODULES = new Set(['ids', 'trackAcquisition', 'types', 'wireDecoders']);

function sourceFiles(dir: string): string[] {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      return entry.name === 'adapters' || entry.name === '__tests__' ? [] : sourceFiles(full);
    }
    return /\.tsx?$/.test(entry.name) ? [full] : [];
  });
}

const files = sourceFiles(libraryRoot).map((file) => ({
  name: path.relative(libraryRoot, file),
  text: fs.readFileSync(file, 'utf8'),
}));

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

test('the library has source files to check', () => {
  expect(files.length).toBeGreaterThan(10);
});

test('the import scan flags a value import and spares type and pure-module imports', () => {
  expect(apiClientValueImports("import { getTracks } from '@shared/api-client';")).toHaveLength(1);
  expect(apiClientValueImports("import { a } from '../../shared/api-client/tracks';")).toHaveLength(
    1,
  );
  expect(apiClientValueImports("import type { T } from '@shared/api-client';")).toHaveLength(0);
  expect(apiClientValueImports("import { type T } from '@shared/api-client';")).toHaveLength(0);
  expect(apiClientValueImports("import { asTrackId } from '@shared/api-client/ids';")).toHaveLength(
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

describe('each library mutation has one calling hook', () => {
  test.each([
    ['port.deleteTrack(', 'hooks/useDeleteTracks.ts'],
    ['port.reacquireTrack(', 'hooks/useReacquireTrack.ts'],
  ])('%s appears only in %s', (call, owner) => {
    const callers = files.filter(({ text }) => text.includes(call)).map(({ name }) => name);

    expect(callers).toEqual([owner]);
  });
});
