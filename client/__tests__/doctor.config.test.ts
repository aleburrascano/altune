import { readdirSync } from 'node:fs';
import { join, relative } from 'node:path';

import doctorConfig from '../doctor.config';

type Override = { files: string[]; rules: string[] };

const mobileRoot = join(__dirname, '..');

function overridesFor(rule: string): Override[] {
  const overrides = (doctorConfig as { ignore: { overrides: Override[] } }).ignore.overrides;
  return overrides.filter((override) => override.rules.includes(rule));
}

function nativeSiblingsOfWebFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) {
      return entry.name === '__tests__' ? [] : nativeSiblingsOfWebFiles(path);
    }
    const webMatch = /^(.*)\.web(\.tsx?)$/.exec(entry.name);
    return webMatch ? [relative(mobileRoot, join(directory, webMatch[1] + webMatch[2]))] : [];
  });
}

describe('doctor.config', () => {
  it('silences unused-export on the native file of every platform-split pair', () => {
    const silenced = overridesFor('deslop/unused-export').flatMap((override) => override.files);

    expect(silenced.sort()).toEqual(nativeSiblingsOfWebFiles(join(mobileRoot, 'src')).sort());
  });

  it('silences unused-file on the dynamically loaded eslint consistency rules', () => {
    const silenced = overridesFor('deslop/unused-file').flatMap((override) => override.files);

    expect(silenced).toEqual(['eslint/consistency/**']);
  });
});
