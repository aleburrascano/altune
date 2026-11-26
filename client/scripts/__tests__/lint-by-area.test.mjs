import { describe, test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

import { areaOf, renderTable, tallyByArea } from '../lint-by-area.mjs';

const client = join(dirname(fileURLToPath(import.meta.url)), '..', '..');
const ROOT = '/work/client';

const error = (ruleId) => ({ severity: 2, ruleId });
const result = (path, ...messages) => ({ filePath: `${ROOT}/${path}`, messages });

describe('areaOf', () => {
  test('names a shared module, a feature and a top-level source folder', () => {
    assert.equal(areaOf('src/shared/auth/core/userLocalData.ts'), 'src/shared/auth');
    assert.equal(areaOf('src/shared/auth/index.ts'), 'src/shared/auth');
    assert.equal(areaOf('src/features/library/view/Row.tsx'), 'src/features/library');
    assert.equal(areaOf('src/mobile/RootLayout.tsx'), 'src/mobile');
    assert.equal(areaOf('src/app/(tabs)/library.tsx'), 'src/app');
  });

  test('names the top-level folders outside src and the files at the client root', () => {
    assert.equal(areaOf('__tests__/rootLayout.integration.test.tsx'), '__tests__/');
    assert.equal(areaOf('scripts/lint-by-area.mjs'), 'scripts/');
    assert.equal(areaOf('eslint.config.js'), '(client root files)');
  });

  test('keeps a file directly under shared or features in that folder', () => {
    assert.equal(areaOf('src/shared/index.ts'), 'src/shared');
    assert.equal(areaOf('src/features/README.md'), 'src/features');
  });
});

describe('tallyByArea and renderTable', () => {
  const results = [
    result(
      'src/shared/auth/__tests__/a.test.ts',
      error('no-restricted-properties'),
      error('no-restricted-properties'),
      error('local/test-imports-through-doors'),
    ),
    result('src/shared/auth/core/b.ts', error('no-restricted-imports')),
    result('src/features/library/__tests__/c.test.tsx', error('no-restricted-properties')),
    result('__tests__/d.test.tsx', error(null)),
    result('src/shared/clean/e.ts', { severity: 1, ruleId: 'some-warning' }),
  ];

  test('counts errors per area and rule, and ignores warnings', () => {
    const areas = tallyByArea(results, ROOT);

    assert.equal(areas.get('src/shared/auth').get('no-restricted-properties'), 2);
    assert.equal(areas.get('src/shared/auth').get('local/test-imports-through-doors'), 1);
    assert.equal(areas.get('src/shared/auth').get('no-restricted-imports'), 1);
    assert.equal(areas.get('src/features/library').get('no-restricted-properties'), 1);
    assert.equal(areas.get('__tests__/').get('(parse error)'), 1);
    assert.equal(areas.has('src/shared/clean'), false);
  });

  test('renders areas by total, rules by total, with a totals row', () => {
    const table = renderTable(tallyByArea(results, ROOT)).split('\n');

    assert.equal(
      table[0],
      '| area | `no-restricted-properties` | `(parse error)` | `local/test-imports-through-doors` | `no-restricted-imports` | total |',
    );
    assert.equal(table[2], '| src/shared/auth | 2 | 0 | 1 | 1 | 4 |');
    assert.equal(table[3], '| __tests__/ | 0 | 1 | 0 | 0 | 1 |');
    assert.equal(table[4], '| src/features/library | 1 | 0 | 0 | 0 | 1 |');
    assert.equal(table.at(-1), '| **total** | 3 | 1 | 1 | 1 | 6 |');
  });
});

describe('lint-by-area.mjs', () => {
  test('refuses an unknown flag with the usage and exit code 2', () => {
    const run = spawnSync(process.execPath, ['scripts/lint-by-area.mjs', '--fix'], {
      cwd: client,
      encoding: 'utf8',
    });

    assert.equal(run.status, 2);
    assert.match(run.stderr, /usage: lint-by-area\.mjs/);
  });

  test('exits 0 for a clean file when run from another folder, resolving the path from there', () => {
    const run = spawnSync(process.execPath, ['../scripts/lint-by-area.mjs', '../babel.config.js'], {
      cwd: join(client, 'src'),
      encoding: 'utf8',
    });

    assert.equal(run.status, 0, run.stderr);
    assert.match(run.stdout, /No lint errors in 1 files/);
  });

  test('exits 2 when every path given is ignored', () => {
    const run = spawnSync(
      process.execPath,
      ['scripts/lint-by-area.mjs', 'node_modules/eslint/package.json'],
      { cwd: client, encoding: 'utf8' },
    );

    assert.equal(run.status, 2);
    assert.match(run.stderr, /every path given is ignored/);
  });

  test('exits 2 when a path matches no file', () => {
    const run = spawnSync(process.execPath, ['scripts/lint-by-area.mjs', 'src/nope/missing.ts'], {
      cwd: client,
      encoding: 'utf8',
    });

    assert.equal(run.status, 2);
    assert.match(run.stderr, /No files matching/);
  });
});

describe('areaOf from the client root', () => {
  test('puts a feature file under its feature whichever folder the script ran from', () => {
    const results = [{ filePath: `${ROOT}/src/features/player/a.ts`, messages: [error('x')] }];

    assert.deepEqual([...tallyByArea(results, ROOT).keys()], ['src/features/player']);
  });

  test('puts a file outside the client under its own ../ area', () => {
    const results = [{ filePath: '/work/other/a.ts', messages: [error('x')] }];

    assert.deepEqual([...tallyByArea(results, ROOT).keys()], ['../']);
  });
});
