import { readdirSync, readFileSync, statSync } from 'fs';
import { join } from 'path';

const SLICE_ROOT = join(__dirname, '..');
const BANNED_NOUN = /\bsongs?\b/i;

function sourceFiles(dir: string): string[] {
  const out: string[] = [];
  for (const name of readdirSync(dir)) {
    if (name === '__tests__') continue;
    const full = join(dir, name);
    if (statSync(full).isDirectory()) {
      out.push(...sourceFiles(full));
      continue;
    }
    if (/\.(ts|tsx)$/.test(name)) out.push(full);
  }
  return out;
}

describe('library vocabulary invariant — the noun is Track, never Song', () => {
  it('has source files to scan, so a broken walk cannot pass by finding nothing', () => {
    expect(sourceFiles(SLICE_ROOT).length).toBeGreaterThan(10);
  });

  it('contains the banned noun in no shipped source file, chip and list vocabulary included', () => {
    const offenders = sourceFiles(SLICE_ROOT).filter((file) =>
      BANNED_NOUN.test(readFileSync(file, 'utf8')),
    );
    expect(offenders).toEqual([]);
  });
});
