import { existsSync, readFileSync } from 'node:fs';
import { basename, dirname, extname, join } from 'node:path';

import doctorConfig from '@/../doctor.config';

type Override = { files: string[]; rules: string[] };

const mobileRoot = join(__dirname, '..');

const PLATFORM_SUFFIXES = ['web', 'android', 'ios', 'native'];

function overridesFor(rule: string): Override[] {
  const overrides = (doctorConfig as { ignore: { overrides: Override[] } }).ignore.overrides;
  return overrides.filter((override) => override.rules.includes(rule));
}

function entryList(): string[] {
  const knip = JSON.parse(readFileSync(join(mobileRoot, 'knip.json'), 'utf8')) as {
    entry: string[];
  };
  return knip.entry;
}

function sharedFileEntries(): string[] {
  return entryList().filter(
    (entry) => /^src\/.*\.tsx?$/.test(entry) && !entry.includes('*') && !/\/index\.ts$/.test(entry),
  );
}

function hasPlatformFileBeside(sharedFile: string): boolean {
  const extension = extname(sharedFile);
  const stem = basename(sharedFile, extension);
  return PLATFORM_SUFFIXES.some((suffix) =>
    existsSync(join(mobileRoot, dirname(sharedFile), `${stem}.${suffix}${extension}`)),
  );
}

describe('doctor.config', () => {
  it('has no per-file unused-export ignore: platform files are entries in knip.json', () => {
    expect(overridesFor('deslop/unused-export')).toEqual([]);
  });

  it('silences unused-file only on the shared module doors', () => {
    const silenced = overridesFor('deslop/unused-file').flatMap((override) => override.files);

    expect(silenced).toEqual(['src/shared/*/index.ts']);
  });
});

describe('knip.json entry list', () => {
  it('lists only shared files that still have a platform file beside them', () => {
    const withoutPlatformFile = sharedFileEntries().filter(
      (sharedFile) => !hasPlatformFileBeside(sharedFile),
    );

    expect(withoutPlatformFile).toEqual([]);
  });

  it('lists shared files that exist, so a rename cannot leave a dead entry', () => {
    const missing = sharedFileEntries().filter(
      (sharedFile) => !existsSync(join(mobileRoot, sharedFile)),
    );

    expect(missing).toEqual([]);
  });

  it('leaves .web files off the list, because both tools already resolve an import to them', () => {
    const webEntries = entryList().filter((entry) => /\.web[.}]/.test(entry));

    expect(webEntries).toEqual([]);
  });
});
