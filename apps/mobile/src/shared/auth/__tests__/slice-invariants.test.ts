import * as fs from 'node:fs';
import * as path from 'node:path';

const SRC_ROOT = path.join(process.cwd(), 'src');
if (!fs.existsSync(SRC_ROOT)) {
  throw new Error(
    `expected ${SRC_ROOT} to exist — this suite resolves paths from process.cwd(), which must be apps/mobile`,
  );
}
const AUTH_DIR = path.join(SRC_ROOT, 'shared', 'auth');

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

const IMPORT_STATEMENT = /import\s+(type\s+)?[^;\n]*?from\s+['"]([^'"]+)['"]/g;

function valueImportsOf(source: string, moduleSpecifier: string): boolean {
  for (const match of source.matchAll(IMPORT_STATEMENT)) {
    const [, isTypeOnly, spec] = match;
    if (!isTypeOnly && spec === moduleSpecifier) return true;
  }
  return false;
}

function createClientCallCount(source: string): number {
  return (source.match(/\bcreateClient\(/g) ?? []).length;
}

function words(source: string): string[] {
  return source.split(/[^a-zA-Z0-9]+|(?<=[a-z0-9])(?=[A-Z])/).filter((word) => word.length > 0);
}

function bannedNounViolations(source: string): string[] {
  return words(source).filter((word) => /^songs?$/i.test(word));
}

describe('sanity: this suite is actually scanning the real slice', () => {
  it('shared/auth is found under process.cwd()/src and contains supabaseClient.ts', () => {
    const names = listSourceFiles(AUTH_DIR).map((file) => path.basename(file));
    expect(names.length).toBeGreaterThan(0);
    expect(names).toContain('supabaseClient.ts');
  });
});

describe('architecture: @supabase/supabase-js is value-imported from exactly one file', () => {
  it('every file under src that value-imports @supabase/supabase-js is shared/auth/supabaseClient.ts', () => {
    const offenders = listSourceFiles(SRC_ROOT)
      .filter((file) => valueImportsOf(fs.readFileSync(file, 'utf8'), '@supabase/supabase-js'))
      .map((file) => path.relative(SRC_ROOT, file));

    expect(offenders).toEqual([path.join('shared', 'auth', 'supabaseClient.ts')]);
  });

  it('a type-only import of @supabase/supabase-js does not count as a value import, a bare import does', () => {
    expect(
      valueImportsOf("import type { Session } from '@supabase/supabase-js';", '@supabase/supabase-js'),
    ).toBe(false);
    expect(
      valueImportsOf("import { createClient } from '@supabase/supabase-js';", '@supabase/supabase-js'),
    ).toBe(true);
  });
});

describe('architecture: exactly one createClient( call exists in the app', () => {
  it('summed across every file that imports createClient from @supabase/supabase-js, there is exactly one call site, in supabaseClient.ts', () => {
    const sites = listSourceFiles(SRC_ROOT)
      .map((file) => ({ file, source: fs.readFileSync(file, 'utf8') }))
      .filter(({ source }) => valueImportsOf(source, '@supabase/supabase-js'))
      .flatMap(({ file, source }) =>
        Array<string>(createClientCallCount(source)).fill(path.relative(SRC_ROOT, file)),
      );

    expect(sites).toEqual([path.join('shared', 'auth', 'supabaseClient.ts')]);
  });
});

describe('no console.log in shared/auth', () => {
  it('no file in the slice calls console.log', () => {
    const offenders = listSourceFiles(AUTH_DIR)
      .filter((file) => /console\.log\(/.test(fs.readFileSync(file, 'utf8')))
      .map((file) => path.basename(file));

    expect(offenders).toEqual([]);
  });
});

describe('the banned noun never appears in shared/auth', () => {
  it('no identifier or string literal in shared/auth contains "song"', () => {
    const offenders = listSourceFiles(AUTH_DIR)
      .filter((file) => bannedNounViolations(fs.readFileSync(file, 'utf8')).length > 0)
      .map((file) => path.basename(file));

    expect(offenders).toEqual([]);
  });

  it('flags "song" standalone, in camelCase, PascalCase, SCREAMING_SNAKE_CASE and pluralized', () => {
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

describe('security: no file in shared/auth reaches for AsyncStorage', () => {
  it('no file imports @react-native-async-storage/async-storage or references the AsyncStorage identifier', () => {
    const offenders = listSourceFiles(AUTH_DIR)
      .filter((file) => /AsyncStorage|async-storage/.test(fs.readFileSync(file, 'utf8')))
      .map((file) => path.basename(file));

    expect(offenders).toEqual([]);
  });
});

describe('security: the slice never decodes or inspects the access token', () => {
  it('no file calls atob, a jwt-decode helper, or slices a token on its dot separators', () => {
    const offenders = listSourceFiles(AUTH_DIR)
      .filter((file) => /atob\(|jwt-decode|jwtDecode|\.split\(['"]\.['"]\)/.test(fs.readFileSync(file, 'utf8')))
      .map((file) => path.basename(file));

    expect(offenders).toEqual([]);
  });
});
