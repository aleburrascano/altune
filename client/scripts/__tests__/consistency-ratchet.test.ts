import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { execFileSync } from 'node:child_process';
import { pathToFileURL } from 'node:url';
import { loadRules } from '../../eslint/consistency';

const mobileRoot = path.join(__dirname, '..', '..');
const ratchetScript = path.join(mobileRoot, 'scripts', 'consistency-ratchet.mjs');
const ratchetUrl = pathToFileURL(ratchetScript).href;

async function lintSnippet(rule: ReturnType<typeof loadRules>[number], source: string) {
  const bridge = [
    `const { lintSource } = await import(${JSON.stringify(ratchetUrl)});`,
    `const messages = await lintSource(${JSON.stringify(rule)}, ${JSON.stringify(source)}, ${JSON.stringify(`${rule.id}.tsx`)});`,
    'process.stdout.write(JSON.stringify(messages));',
  ].join('\n');
  const output = execFileSync('node', ['--input-type=module', '-e', bridge], {
    cwd: mobileRoot,
    encoding: 'utf8',
  });
  return JSON.parse(output) as { message: string; ruleId?: string | null }[];
}

function runRatchetCli(env: NodeJS.ProcessEnv = {}) {
  try {
    const stdout = execFileSync('node', [ratchetScript], {
      cwd: mobileRoot,
      encoding: 'utf8',
      env: { ...process.env, ...env },
    });
    return { status: 0, stdout };
  } catch (error) {
    const failure = error as { status: number | null; stdout?: string; stderr?: string };
    return { status: failure.status, stdout: `${failure.stdout ?? ''}${failure.stderr ?? ''}` };
  }
}

const rules = loadRules();

test('at least one rule module is registered', () => {
  expect(rules.length).toBeGreaterThan(0);
});

describe.each(rules)('$id', (rule) => {
  test('flags every invalid example', async () => {
    for (const invalid of rule.examples.invalid) {
      const messages = await lintSnippet(rule, invalid);
      expect(messages.length).toBeGreaterThan(0);
    }
  });

  test('passes every valid example', async () => {
    for (const valid of rule.examples.valid) {
      const messages = await lintSnippet(rule, valid);
      expect(messages).toHaveLength(0);
    }
  });
});

test('checklist doc lists MC-1 through MC-7', () => {
  const doc = fs.readFileSync(
    path.join(__dirname, '..', '..', '..', 'docs', 'conventions', 'mobile-consistency.md'),
    'utf8',
  );
  for (let n = 1; n <= 7; n += 1) {
    expect(doc).toMatch(new RegExp(`MC-${n}\\b`));
  }
});

test('running the real script exits 0 on the current src tree, one line per rule module', () => {
  const { status, stdout } = runRatchetCli();

  expect(status).toBe(0);
  for (const rule of rules) {
    expect(stdout).toMatch(new RegExp(`^${rule.id}: \\d+$`, 'm'));
  }
});

test('running the real script reports MC-1 at its baselined supabaseClient imports and no raw network', () => {
  const { status, stdout } = runRatchetCli();

  expect(status).toBe(0);
  expect(stdout).toMatch(/^MC-1: \d+$/m);
  expect(stdout).not.toContain('new violation');
});

test('running the real script exits 1 when one violation is fixed and another added, count unchanged', () => {
  const relativePath = path.join('src', '__consistency_ratchet_swap__.ts');
  const fixturePath = path.join(mobileRoot, relativePath);
  const baselineLines = fs
    .readFileSync(path.join(mobileRoot, 'consistency.baseline'), 'utf8')
    .split('\n')
    .filter((line) => line !== '');
  const fixedLine = 'MC-1 src/gone.ts Use apiFetch/apiSend from @shared/api-client (MC-1).';
  const swapped = path.join(os.tmpdir(), `consistency-swap-${process.pid}.baseline`);
  fs.writeFileSync(swapped, `${[...baselineLines, fixedLine].join('\n')}\n`);
  fs.writeFileSync(
    fixturePath,
    "export function f() {\n  return fetch('https://example.com');\n}\n",
  );

  try {
    const { status, stdout } = runRatchetCli({ CONSISTENCY_BASELINE_FILE: swapped });

    expect(status).toBe(1);
    expect(stdout).toContain(`new violation: MC-1 ${relativePath}`);
    expect(stdout).toContain(`fixed: remove it from the baseline: ${fixedLine}`);
  } finally {
    fs.rmSync(fixturePath, { force: true });
    fs.rmSync(swapped, { force: true });
  }
});

test('running the real script exits 1 for a listed violation that no longer occurs', () => {
  const stale = 'MC-1 src/gone.ts Use apiFetch/apiSend from @shared/api-client (MC-1).';
  const listed = path.join(os.tmpdir(), `consistency-stale-${process.pid}.baseline`);
  fs.writeFileSync(
    listed,
    `${fs.readFileSync(path.join(mobileRoot, 'consistency.baseline'), 'utf8')}${stale}\n`,
  );

  try {
    const { status, stdout } = runRatchetCli({ CONSISTENCY_BASELINE_FILE: listed });

    expect(status).toBe(1);
    expect(stdout).toContain(`fixed: remove it from the baseline: ${stale}`);
  } finally {
    fs.rmSync(listed, { force: true });
  }
});

test('running the real script exits 1 and names MC-1 and the file when a raw fetch is added', () => {
  const relativePath = path.join('src', '__consistency_ratchet_fixture__.ts');
  const fixturePath = path.join(mobileRoot, relativePath);
  fs.writeFileSync(
    fixturePath,
    "export function f() {\n  return fetch('https://example.com');\n}\n",
  );

  try {
    const { status, stdout } = runRatchetCli();

    expect(status).toBe(1);
    expect(stdout).toContain('MC-1');
    expect(stdout).toContain(relativePath);
  } finally {
    fs.rmSync(fixturePath, { force: true });
  }
});

describe.each([
  [
    'MC-9',
    path.join('src', 'features', 'auth', 'ui', '__mc9_fixture__.ts'),
    "import { x } from '../adapters/http/x';\nexport const y = x;\n",
  ],
  [
    'MC-9',
    path.join('src', 'features', 'library', 'hooks', '__mc9_fixture__.ts'),
    "import { x } from '../adapters/http/x';\nexport const y = x;\n",
  ],
  [
    'MC-10',
    path.join('src', 'features', 'library', 'hooks', '__mc10_fixture__.ts'),
    "export const k = { queryKey: ['x'] };\n",
  ],
])('running the real script for %s', (ruleId, relativePath, source) => {
  test(`exits 1 and names ${relativePath} when it is added`, () => {
    const fixturePath = path.join(mobileRoot, relativePath);
    fs.writeFileSync(fixturePath, source);

    try {
      const { status, stdout } = runRatchetCli();

      expect(status).toBe(1);
      expect(stdout).toContain(`new violation: ${ruleId} ${relativePath}`);
    } finally {
      fs.rmSync(fixturePath, { force: true });
    }
  });
});

async function lintMc2At(filename: string, source: string) {
  const mc2 = rules.find((rule) => rule.id === 'MC-2')!;
  const bridge = [
    `const { lintSource } = await import(${JSON.stringify(ratchetUrl)});`,
    `const messages = await lintSource(${JSON.stringify(mc2)}, ${JSON.stringify(source)}, ${JSON.stringify(filename)});`,
    'process.stdout.write(JSON.stringify(messages));',
  ].join('\n');
  const output = execFileSync('node', ['--input-type=module', '-e', bridge], {
    cwd: mobileRoot,
    encoding: 'utf8',
  });
  return JSON.parse(output) as { message: string; ruleId?: string | null }[];
}

describe('MC-2 owner of the library delete and re-acquire calls', () => {
  const importing = (name: string) =>
    `import { ${name} } from '@shared/api-client/tracks';\nexport const f = ${name};\n`;

  test.each([
    ['useDeleteTracks.ts', 'deleteTrack'],
    ['useReacquireTrack.ts', 'reacquireTrack'],
  ])('flags %s importing %s from the api-client', async (hook, name) => {
    const messages = await lintMc2At(`src/features/library/hooks/${hook}`, importing(name));

    expect(messages.length).toBeGreaterThan(0);
    expect(messages[0].message).toContain('MC-2');
  });

  test.each(['deleteTrack', 'reacquireTrack'])(
    'allows httpLibraryPort.ts to import %s',
    async (name) => {
      const messages = await lintMc2At(
        'src/features/library/adapters/http/httpLibraryPort.ts',
        importing(name),
      );

      expect(messages).toHaveLength(0);
    },
  );
});

describe('MC-2 owner of the settings featured-artist backfill', () => {
  const importing =
    "import { backfillFeaturedArtists } from '@shared/api-client/tracks';\nexport const f = backfillFeaturedArtists;\n";

  test('flags useBackfillFeatured.ts importing backfillFeaturedArtists from the api-client', async () => {
    const messages = await lintMc2At(
      'src/features/settings/hooks/useBackfillFeatured.ts',
      importing,
    );

    expect(messages.length).toBeGreaterThan(0);
    expect(messages[0].message).toContain('MC-2');
  });

  test('allows httpSettingsPort.ts to import backfillFeaturedArtists', async () => {
    const messages = await lintMc2At(
      'src/features/settings/adapters/http/httpSettingsPort.ts',
      importing,
    );

    expect(messages).toHaveLength(0);
  });
});

describe('MC-2 keeps backfillFeaturedArtists out of every file but the settings adapter', () => {
  const importing =
    "import { backfillFeaturedArtists } from '@shared/api-client/tracks';\nexport const f = backfillFeaturedArtists;\n";

  test.each([
    'src/features/settings/components/LibraryCard.tsx',
    'src/features/settings/ports/SettingsPortProvider.tsx',
    'src/features/library/adapters/http/httpLibraryPort.ts',
    'src/features/settings/adapters/http/httpSettingsPortHelper.ts',
  ])('flags %s importing it from the api-client', async (filename) => {
    const messages = await lintMc2At(filename, importing);

    expect(messages.length).toBeGreaterThan(0);
    expect(messages[0].message).toContain('MC-2');
  });

  test('flags the hook importing it under an alias', async () => {
    const messages = await lintMc2At(
      'src/features/settings/hooks/useBackfillFeatured.ts',
      "import { backfillFeaturedArtists as runBackfill } from '@shared/api-client/tracks';\nexport const f = runBackfill;\n",
    );

    expect(messages.length).toBeGreaterThan(0);
    expect(messages[0].message).toContain('MC-2');
  });
});

async function lintMc8At(filename: string, source: string) {
  const mc8 = rules.find((rule) => rule.id === 'MC-8')!;
  const bridge = [
    `const { lintSource } = await import(${JSON.stringify(ratchetUrl)});`,
    `const messages = await lintSource(${JSON.stringify(mc8)}, ${JSON.stringify(source)}, ${JSON.stringify(filename)});`,
    'process.stdout.write(JSON.stringify(messages));',
  ].join('\n');
  const output = execFileSync('node', ['--input-type=module', '-e', bridge], {
    cwd: mobileRoot,
    encoding: 'utf8',
  });
  return JSON.parse(output) as { message: string; ruleId?: string | null }[];
}

describe('MC-8 features reach the API only through their own adapters', () => {
  const barrelImport =
    "import { getTracks } from '@shared/api-client';\nexport const g = getTracks;\n";

  test('checklist doc has an MC-8 row', () => {
    const doc = fs.readFileSync(
      path.join(__dirname, '..', '..', '..', 'docs', 'conventions', 'mobile-consistency.md'),
      'utf8',
    );
    expect(doc).toMatch(/^\| MC-8 \|/m);
  });

  test('the real script prints MC-8 and exits 0', () => {
    const { status, stdout } = runRatchetCli();

    expect(status).toBe(0);
    expect(stdout).toMatch(/^MC-8: \d+$/m);
  });

  test('flags a value import from the barrel in a feature hook', async () => {
    const messages = await lintMc8At('src/features/library/hooks/useThing.ts', barrelImport);

    expect(messages.length).toBeGreaterThan(0);
    expect(messages[0].message).toContain('MC-8');
  });

  test.each([
    "import type { TrackResponse } from '@shared/api-client/types';\nexport type T = TrackResponse;\n",
    "import { asTrackId } from '@shared/api-client/ids';\nexport const t = asTrackId;\n",
  ])('allows a type or pure-module import in a feature hook', async (source) => {
    expect(await lintMc8At('src/features/library/hooks/useThing.ts', source)).toHaveLength(0);
  });

  test('allows a feature adapter to import the barrel', async () => {
    const messages = await lintMc8At(
      'src/features/library/adapters/http/httpLibraryPort.ts',
      barrelImport,
    );

    expect(messages).toHaveLength(0);
  });

  test('the real script exits 1 and names MC-8 and the file when a hook adds a barrel import', () => {
    const fixtureDir = path.join(mobileRoot, 'src', 'features', '__mc8_fixture__');
    const relativePath = path.join('src', 'features', '__mc8_fixture__', 'hooks', 'x.ts');
    const fixturePath = path.join(mobileRoot, relativePath);
    fs.mkdirSync(path.dirname(fixturePath), { recursive: true });
    fs.writeFileSync(fixturePath, barrelImport);

    try {
      const { status, stdout } = runRatchetCli();

      expect(status).toBe(1);
      expect(stdout).toContain('MC-8');
      expect(stdout).toContain(relativePath);
    } finally {
      fs.rmSync(fixtureDir, { recursive: true, force: true });
    }
  });
});
