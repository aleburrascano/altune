import { describe, test } from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { copyFileSync, mkdtempSync, mkdirSync, writeFileSync, rmSync, symlinkSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const client = join(dirname(fileURLToPath(import.meta.url)), '..', '..');
const COPIED = [
  'package.json',
  'tsconfig.json',
  '.fallowrc.json',
  'fallow/modules.json',
  'fallow/parts.json',
  'scripts/fallow-changed.sh',
];

const BASE = {
  'src/shared/math/index.ts': "export { clamp } from './core/clamp';\n",
  'src/shared/math/core/clamp.ts':
    'export function clamp(n: number, low: number, high: number): number {\n  return Math.min(Math.max(n, low), high);\n}\n',
  'src/shared/words/index.ts': "export { shout } from './core/shout';\n",
  'src/shared/words/core/shout.ts': 'export function shout(s: string): string {\n  return s.toUpperCase();\n}\n',
  'src/features/price/index.ts': "export { price } from './core/price';\n",
  'src/features/price/core/price.ts':
    "import { clamp } from '@shared/math';\n\nexport function price(cents: number): number {\n  return clamp(cents, 0, 100);\n}\n",
  'src/features/cart/index.ts': "export { total } from './core/total';\n",
  'src/features/cart/core/total.ts':
    'export function total(items: number[]): number {\n  return items.reduce((sum, n) => sum + n, 0);\n}\n',
  'src/mobile/index.ts':
    "import { price } from '@features/price';\nimport { total } from '@features/cart';\nimport { shout } from '@shared/words';\n\nexport const app = [price(1), total([1]), shout('a')];\n",
  'src/app/index.ts': "import { app } from '@/mobile';\n\nexport default app;\n",
};

function git(cwd, ...args) {
  const r = spawnSync('git', args, { cwd, encoding: 'utf8' });
  assert.equal(r.status, 0, r.stderr);
  return r.stdout.trim();
}

function write(root, files) {
  for (const [path, body] of Object.entries(files)) {
    mkdirSync(dirname(join(root, path)), { recursive: true });
    writeFileSync(join(root, path), body);
  }
}

function repo(t, baseFiles = {}) {
  const root = mkdtempSync(join(tmpdir(), 'fallow-changed-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  for (const path of COPIED) {
    mkdirSync(dirname(join(root, path)), { recursive: true });
    copyFileSync(join(client, path), join(root, path));
  }
  symlinkSync(join(client, 'node_modules'), join(root, 'node_modules'));
  write(root, { '.gitignore': 'node_modules\n', ...BASE, ...baseFiles });
  git(root, 'init', '-q');
  git(root, 'config', 'user.email', 'probe@example.com');
  git(root, 'config', 'user.name', 'probe');
  git(root, 'config', 'commit.gpgsign', 'false');
  git(root, 'add', '-A');
  git(root, 'commit', '-qm', 'base');
  return { root, base: git(root, 'rev-parse', 'HEAD') };
}

function run(root, ...args) {
  const r = spawnSync('bash', [join(root, 'scripts/fallow-changed.sh'), ...args], {
    cwd: root,
    encoding: 'utf8',
  });
  return { status: r.status, output: r.stdout + r.stderr };
}

function changedRun(t, files, baseFiles) {
  const { root, base } = repo(t, baseFiles);
  write(root, files);
  return run(root, base);
}

const README_HINT = /Read client\/src\/README\.md/;

describe('fallow-changed', () => {
  test('passes a change that keeps every rule', (t) => {
    const r = changedRun(t, {
      'src/shared/math/core/clamp.ts':
        'export function clamp(n: number, low: number, high: number): number {\n  return Math.max(low, Math.min(n, high));\n}\n',
    });
    assert.equal(r.status, 0, r.output);
  });

  test('fails a feature that imports another feature', (t) => {
    const r = changedRun(t, {
      'src/features/cart/core/total.ts':
        "import { price } from '@features/price';\n\nexport function total(items: number[]): number {\n  return items.reduce((sum, n) => sum + price(n), 0);\n}\n",
    });
    assert.equal(r.status, 1, r.output);
    assert.match(r.output, /src\/features\/cart\/core\/total\.ts/);
    assert.match(r.output, README_HINT);
  });

  test('fails a module that imports past another module door', (t) => {
    const r = changedRun(t, {
      'src/features/cart/core/total.ts':
        "import { clamp } from '@shared/math/core/clamp';\n\nexport function total(items: number[]): number {\n  return clamp(items.length, 0, 9);\n}\n",
    });
    assert.equal(r.status, 1, r.output);
    assert.match(r.output, /src\/features\/cart\/core\/total\.ts/);
  });

  test('fails core that imports view', (t) => {
    const r = changedRun(t, {
      'src/shared/words/view/label.ts': "export const label = 'x';\n",
      'src/shared/words/core/shout.ts':
        "import { label } from '../view/label';\n\nexport function shout(s: string): string {\n  return s.toUpperCase() + label;\n}\n",
    });
    assert.equal(r.status, 1, r.output);
    assert.match(r.output, /src\/shared\/words\/core\/shout\.ts/);
  });

  test('fails a .ts file that sits in no part', (t) => {
    const r = changedRun(t, {
      'src/shared/words/helpers.ts': "export const bang = '!';\n",
      'src/shared/words/index.ts':
        "export { shout } from './core/shout';\nexport { bang } from './helpers';\n",
    });
    assert.equal(r.status, 1, r.output);
    assert.match(r.output, /src\/shared\/words\/helpers\.ts:1 no matching boundary zone/);
  });

  test('fails fetch outside adapters', (t) => {
    const r = changedRun(t, {
      'src/shared/words/core/shout.ts':
        "export function shout(s: string): string {\n  void fetch('https://example.com');\n  return s.toUpperCase();\n}\n",
    });
    assert.equal(r.status, 1, r.output);
    assert.match(r.output, /fetch/);
  });

  test('fails code duplicated in a changed file', (t) => {
    const body =
      'export function NAME(rows: { id: string; score: number }[]): string[] {\n  const kept = rows.filter((row) => row.score > 10 && row.id.length > 2);\n  const sorted = [...kept].sort((a, b) => b.score - a.score || a.id.localeCompare(b.id));\n  const top = sorted.slice(0, 25);\n  const labels = top.map((row) => `${row.id}:${row.score.toFixed(2)}`);\n  const unique = [...new Set(labels)];\n  return unique.filter((label) => label.length < 40);\n}\n';
    const r = changedRun(t, {
      'src/shared/words/core/rank.ts': body.replace('NAME', 'rank'),
      'src/shared/math/core/order.ts': body.replace('NAME', 'order'),
      'src/shared/words/index.ts': "export { shout } from './core/shout';\nexport { rank } from './core/rank';\n",
      'src/shared/math/index.ts': "export { clamp } from './core/clamp';\nexport { order } from './core/order';\n",
    });
    assert.equal(r.status, 1, r.output);
    assert.match(r.output, /core\/rank\.ts/);
  });

  test('ignores a violation the branch did not change', (t) => {
    const r = changedRun(
      t,
      {
        'src/shared/math/core/clamp.ts':
          'export function clamp(n: number, low: number, high: number): number {\n  return Math.max(low, Math.min(n, high));\n}\n',
      },
      {
        'src/features/cart/core/total.ts':
          "import { price } from '@features/price';\n\nexport function total(items: number[]): number {\n  return items.reduce((sum, n) => sum + price(n), 0);\n}\n",
      },
    );
    assert.equal(r.status, 0, r.output);
  });

  test('refuses a base it cannot resolve, since fallow would then check the whole tree', (t) => {
    const { root } = repo(t);
    const r = run(root, 'no-such-ref');
    assert.equal(r.status, 2, r.output);
    assert.match(r.output, /cannot resolve no-such-ref/);
  });
});
