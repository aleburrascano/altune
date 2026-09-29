import * as fs from 'node:fs';
import * as path from 'node:path';

const SRC = path.join(__dirname, '..', '..', 'src');

function baselineFilesUnder(dir: string): string[] {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) return baselineFilesUnder(full);
    return entry.name === 'platform-legacy.json' ? [path.relative(SRC, full)] : [];
  });
}

describe('platform-legacy baselines stay retired', () => {
  it('finds no platform-legacy.json under src', () => {
    expect(baselineFilesUnder(SRC)).toEqual([]);
  });
});
