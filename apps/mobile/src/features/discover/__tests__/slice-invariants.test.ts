import * as fs from 'node:fs';
import * as path from 'node:path';

const SRC_ROOT = path.join(process.cwd(), 'src');
if (!fs.existsSync(SRC_ROOT)) {
  throw new Error(
    `expected ${SRC_ROOT} to exist — this suite resolves paths from process.cwd(), which must be apps/mobile`,
  );
}
const DISCOVER_DIR = path.join(SRC_ROOT, 'features', 'discover');

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

function words(source: string): string[] {
  return source.split(/[^a-zA-Z0-9]+|(?<=[a-z0-9])(?=[A-Z])/).filter((word) => word.length > 0);
}

function bannedNounViolations(source: string): string[] {
  return words(source).filter((word) => /^songs?$/i.test(word));
}

describe('sanity: this suite is actually scanning the real slice', () => {
  it('features/discover is found under process.cwd()/src and contains state.ts', () => {
    const names = listSourceFiles(DISCOVER_DIR).map((file) => path.basename(file));
    expect(names.length).toBeGreaterThan(0);
    expect(names).toContain('state.ts');
  });
});

describe('the banned noun never appears in features/discover', () => {
  it('no identifier or string literal in features/discover contains "song"', () => {
    const offenders = listSourceFiles(DISCOVER_DIR)
      .filter((file) => bannedNounViolations(fs.readFileSync(file, 'utf8')).length > 0)
      .map((file) => path.relative(DISCOVER_DIR, file));

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
