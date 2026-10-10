import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { pathToFileURL } from 'node:url';
import { loadRules } from '../../eslint/consistency';

const ESLINT_SPAWNING_TEST_TIMEOUT_MS = 60_000;
jest.setTimeout(ESLINT_SPAWNING_TEST_TIMEOUT_MS);

const execFileAsync = promisify(execFile);

const mobileRoot = path.join(__dirname, '..', '..');
const ratchetScript = path.join(mobileRoot, 'scripts', 'consistency-ratchet.mjs');
const ratchetUrl = pathToFileURL(ratchetScript).href;

async function lintSnippets(rule: ReturnType<typeof loadRules>[number], sources: string[]) {
  const bridge = [
    `const { lintSource } = await import(${JSON.stringify(ratchetUrl)});`,
    `const sources = ${JSON.stringify(sources)};`,
    `const messages = await Promise.all(sources.map((source) => lintSource(${JSON.stringify(rule)}, source, ${JSON.stringify(`${rule.id}.tsx`)})));`,
    'process.stdout.write(JSON.stringify(messages));',
  ].join('\n');
  const { stdout } = await execFileAsync('node', ['--input-type=module', '-e', bridge], {
    cwd: mobileRoot,
    encoding: 'utf8',
  });
  return JSON.parse(stdout) as { message: string; ruleId?: string | null }[][];
}

const UPDATE_BASELINE_COMMAND = 'run `cd client && node scripts/consistency-ratchet.mjs --write`';

async function runRatchetCli(env: NodeJS.ProcessEnv = {}, args: string[] = []) {
  try {
    const { stdout } = await execFileAsync('node', [ratchetScript, ...args], {
      cwd: mobileRoot,
      encoding: 'utf8',
      env: { ...process.env, ...env },
    });
    return { status: 0, stdout };
  } catch (error) {
    const failure = error as { code: number | null; stdout?: string; stderr?: string };
    return { status: failure.code, stdout: `${failure.stdout ?? ''}${failure.stderr ?? ''}` };
  }
}

type FixtureFiles = Record<string, string>;

async function runRatchetOnFixtureTree(files: FixtureFiles, baselineLines: string[] = []) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'consistency-ratchet-'));
  const baselineFile = path.join(root, 'consistency.baseline');
  try {
    for (const [relativePath, source] of Object.entries(files)) {
      const filePath = path.join(root, relativePath);
      fs.mkdirSync(path.dirname(filePath), { recursive: true });
      fs.writeFileSync(filePath, source);
    }
    fs.writeFileSync(baselineFile, baselineLines.map((line) => `${line}\n`).join(''));
    return await runRatchetCli({ CONSISTENCY_ROOT: root, CONSISTENCY_BASELINE_FILE: baselineFile });
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
}

const rules = loadRules();

test.concurrent('at least one rule module is registered', () => {
  expect(rules.length).toBeGreaterThan(0);
});

describe.each(rules)('$id', (rule) => {
  test.concurrent('flags every invalid example', async () => {
    const results = await lintSnippets(rule, rule.examples.invalid);
    for (const messages of results) {
      expect(messages.length).toBeGreaterThan(0);
    }
  });

  test.concurrent('passes every valid example', async () => {
    const results = await lintSnippets(rule, rule.examples.valid);
    for (const messages of results) {
      expect(messages).toHaveLength(0);
    }
  });
});

test.concurrent('checklist doc lists MC-1 through MC-7', () => {
  const doc = fs.readFileSync(
    path.join(__dirname, '..', '..', '..', 'docs', 'conventions', 'mobile-consistency.md'),
    'utf8',
  );
  for (let n = 1; n <= 7; n += 1) {
    expect(doc).toMatch(new RegExp(`MC-${n}\\b`));
  }
});

const BASELINED_FETCH_PATH = path.join('src', 'legacy.ts');
const BASELINED_FETCH_LINE = `MC-1 ${BASELINED_FETCH_PATH} Unexpected use of 'fetch'. Use apiFetch/apiSend from @shared/apiClient, not raw fetch (MC-1).`;
const BASELINED_FETCH_SOURCE = "export function f() {\n  return fetch('https://example.com');\n}\n";

test.concurrent(
  'the script exits 0 on a tree matching its baseline, one line per rule module',
  async () => {
    const { status, stdout } = await runRatchetOnFixtureTree(
      { [BASELINED_FETCH_PATH]: BASELINED_FETCH_SOURCE },
      [BASELINED_FETCH_LINE],
    );

    expect(status).toBe(0);
    for (const rule of rules) {
      expect(stdout).toMatch(new RegExp(`^${rule.id}: \\d+$`, 'm'));
    }
  },
);

test.concurrent(
  'the script reports MC-1 at its baselined raw network call and no new violation',
  async () => {
    const { status, stdout } = await runRatchetOnFixtureTree(
      { [BASELINED_FETCH_PATH]: BASELINED_FETCH_SOURCE },
      [BASELINED_FETCH_LINE],
    );

    expect(status).toBe(0);
    expect(stdout).toMatch(/^MC-1: 1$/m);
    expect(stdout).not.toContain('new violation');
  },
);

test.concurrent(
  'running the script exits 1 when one violation is fixed and another added, count unchanged',
  async () => {
    const relativePath = path.join('src', '__consistency_ratchet_swap__.ts');
    const fixedLine = 'MC-1 src/gone.ts Use apiFetch/apiSend from @shared/apiClient (MC-1).';

    const { status, stdout } = await runRatchetOnFixtureTree(
      { [relativePath]: "export function f() {\n  return fetch('https://example.com');\n}\n" },
      [fixedLine],
    );

    expect(status).toBe(1);
    expect(stdout).toContain(`new violation: MC-1 ${relativePath}`);
    expect(stdout).toContain(`fixed: remove it from the baseline: ${fixedLine}`);
    expect(stdout).not.toContain('--write');
  },
);

test.concurrent(
  'the script exits 1 for a listed violation that no longer occurs, naming the refresh command',
  async () => {
    const stale = 'MC-1 src/gone.ts Use apiFetch/apiSend from @shared/apiClient (MC-1).';

    const { status, stdout } = await runRatchetOnFixtureTree(
      { [path.join('src', 'clean.ts')]: 'export const clean = true;\n' },
      [stale],
    );

    expect(status).toBe(1);
    expect(stdout).toContain(`fixed: remove it from the baseline: ${stale}`);
    expect(stdout).toContain(UPDATE_BASELINE_COMMAND);
  },
);

test.concurrent(
  '--write drops a fixed line from the baseline, so the next run passes',
  async () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'consistency-ratchet-'));
    const baselineFile = path.join(root, 'consistency.baseline');
    const stale = 'MC-1 src/gone.ts Use apiFetch/apiSend from @shared/apiClient (MC-1).';
    const env = { CONSISTENCY_ROOT: root, CONSISTENCY_BASELINE_FILE: baselineFile };
    try {
      fs.mkdirSync(path.join(root, 'src'));
      fs.writeFileSync(path.join(root, 'src', 'clean.ts'), 'export const clean = true;\n');
      fs.writeFileSync(baselineFile, `${stale}\n`);

      expect((await runRatchetCli(env, ['--write'])).status).toBe(0);

      expect(fs.readFileSync(baselineFile, 'utf8')).toBe('');
      expect((await runRatchetCli(env)).status).toBe(0);
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  },
);

test.concurrent(
  '--write with a new violation refuses, lists it and leaves the baseline unchanged',
  async () => {
    const root = fs.mkdtempSync(path.join(os.tmpdir(), 'consistency-ratchet-'));
    const baselineFile = path.join(root, 'consistency.baseline');
    const relativePath = path.join('src', '__consistency_ratchet_new__.ts');
    const baseline = 'MC-1 src/gone.ts Use apiFetch/apiSend from @shared/apiClient (MC-1).\n';
    try {
      fs.mkdirSync(path.join(root, 'src'));
      fs.writeFileSync(
        path.join(root, relativePath),
        "export function f() {\n  return fetch('https://example.com');\n}\n",
      );
      fs.writeFileSync(baselineFile, baseline);

      const { status, stdout } = await runRatchetCli(
        { CONSISTENCY_ROOT: root, CONSISTENCY_BASELINE_FILE: baselineFile },
        ['--write'],
      );

      expect(status).toBe(1);
      expect(stdout).toContain(`new violation: MC-1 ${relativePath}`);
      expect(fs.readFileSync(baselineFile, 'utf8')).toBe(baseline);
    } finally {
      fs.rmSync(root, { recursive: true, force: true });
    }
  },
);

test.concurrent(
  'running the script exits 1 and names MC-1 and the file when a raw fetch is added',
  async () => {
    const relativePath = path.join('src', '__consistency_ratchet_fixture__.ts');

    const { status, stdout } = await runRatchetOnFixtureTree({
      [relativePath]: "export function f() {\n  return fetch('https://example.com');\n}\n",
    });

    expect(status).toBe(1);
    expect(stdout).toContain('MC-1');
    expect(stdout).toContain(relativePath);
  },
);

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
])('running the script for %s', (ruleId, relativePath, source) => {
  test.concurrent(`exits 1 and names ${relativePath} when it is added`, async () => {
    const { status, stdout } = await runRatchetOnFixtureTree({ [relativePath]: source });

    expect(status).toBe(1);
    expect(stdout).toContain(`new violation: ${ruleId} ${relativePath}`);
  });
});

async function lintMc2At(filename: string, source: string) {
  const mc2 = rules.find((rule) => rule.id === 'MC-2')!;
  const bridge = [
    `const { lintSource } = await import(${JSON.stringify(ratchetUrl)});`,
    `const messages = await lintSource(${JSON.stringify(mc2)}, ${JSON.stringify(source)}, ${JSON.stringify(filename)});`,
    'process.stdout.write(JSON.stringify(messages));',
  ].join('\n');
  const { stdout } = await execFileAsync('node', ['--input-type=module', '-e', bridge], {
    cwd: mobileRoot,
    encoding: 'utf8',
  });
  return JSON.parse(stdout) as { message: string; ruleId?: string | null }[];
}

describe('MC-2 owner of the library delete and re-acquire calls', () => {
  const importing = (name: string) =>
    `import { ${name} } from '@shared/apiClient/tracks';\nexport const f = ${name};\n`;

  test.concurrent.each([
    ['useDeleteTracks.ts', 'deleteTrack'],
    ['useReacquireTrack.ts', 'reacquireTrack'],
  ])('flags %s importing %s from the apiClient', async (hook, name) => {
    const messages = await lintMc2At(`src/features/library/hooks/${hook}`, importing(name));

    expect(messages.length).toBeGreaterThan(0);
    expect(messages[0].message).toContain('MC-2');
  });

  test.concurrent.each(['deleteTrack', 'reacquireTrack'])(
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
    "import { backfillFeaturedArtists } from '@shared/apiClient/tracks';\nexport const f = backfillFeaturedArtists;\n";

  test.concurrent(
    'flags useBackfillFeatured.ts importing backfillFeaturedArtists from the apiClient',
    async () => {
      const messages = await lintMc2At(
        'src/features/settings/hooks/useBackfillFeatured.ts',
        importing,
      );

      expect(messages.length).toBeGreaterThan(0);
      expect(messages[0].message).toContain('MC-2');
    },
  );

  test.concurrent('allows httpSettingsPort.ts to import backfillFeaturedArtists', async () => {
    const messages = await lintMc2At(
      'src/features/settings/adapters/http/httpSettingsPort.ts',
      importing,
    );

    expect(messages).toHaveLength(0);
  });
});

describe('MC-2 keeps backfillFeaturedArtists out of every file but the settings adapter', () => {
  const importing =
    "import { backfillFeaturedArtists } from '@shared/apiClient/tracks';\nexport const f = backfillFeaturedArtists;\n";

  test.concurrent.each([
    'src/features/settings/components/LibraryCard.tsx',
    'src/features/settings/ports/SettingsPortProvider.tsx',
    'src/features/library/adapters/http/httpLibraryPort.ts',
    'src/features/settings/adapters/http/httpSettingsPortHelper.ts',
  ])('flags %s importing it from the apiClient', async (filename) => {
    const messages = await lintMc2At(filename, importing);

    expect(messages.length).toBeGreaterThan(0);
    expect(messages[0].message).toContain('MC-2');
  });

  test.concurrent('flags the hook importing it under an alias', async () => {
    const messages = await lintMc2At(
      'src/features/settings/hooks/useBackfillFeatured.ts',
      "import { backfillFeaturedArtists as runBackfill } from '@shared/apiClient/tracks';\nexport const f = runBackfill;\n",
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
  const { stdout } = await execFileAsync('node', ['--input-type=module', '-e', bridge], {
    cwd: mobileRoot,
    encoding: 'utf8',
  });
  return JSON.parse(stdout) as { message: string; ruleId?: string | null }[];
}

describe('MC-8 features reach the API only through their own adapters', () => {
  const barrelImport =
    "import { getTracks } from '@shared/apiClient';\nexport const g = getTracks;\n";

  test.concurrent('checklist doc has an MC-8 row', () => {
    const doc = fs.readFileSync(
      path.join(__dirname, '..', '..', '..', 'docs', 'conventions', 'mobile-consistency.md'),
      'utf8',
    );
    expect(doc).toMatch(/^\| MC-8 \|/m);
  });

  test.concurrent('the script prints MC-8 and exits 0 on a clean tree', async () => {
    const { status, stdout } = await runRatchetOnFixtureTree({
      [path.join('src', 'clean.ts')]: 'export const clean = true;\n',
    });

    expect(status).toBe(0);
    expect(stdout).toMatch(/^MC-8: \d+$/m);
  });

  test.concurrent('flags a value import from the barrel in a feature hook', async () => {
    const messages = await lintMc8At('src/features/library/hooks/useThing.ts', barrelImport);

    expect(messages.length).toBeGreaterThan(0);
    expect(messages[0].message).toContain('MC-8');
  });

  test.concurrent.each([
    "import type { TrackResponse } from '@shared/apiClient/types';\nexport type T = TrackResponse;\n",
    "import { asTrackId } from '@shared/apiClient/ids';\nexport const t = asTrackId;\n",
  ])('allows a type or pure-module import in a feature hook', async (source) => {
    expect(await lintMc8At('src/features/library/hooks/useThing.ts', source)).toHaveLength(0);
  });

  test.concurrent('allows a feature adapter to import the barrel', async () => {
    const messages = await lintMc8At(
      'src/features/library/adapters/http/httpLibraryPort.ts',
      barrelImport,
    );

    expect(messages).toHaveLength(0);
  });

  test.concurrent(
    'the script exits 1 and names MC-8 and the file when a hook adds a barrel import',
    async () => {
      const relativePath = path.join('src', 'features', '__mc8_fixture__', 'hooks', 'x.ts');

      const { status, stdout } = await runRatchetOnFixtureTree({ [relativePath]: barrelImport });

      expect(status).toBe(1);
      expect(stdout).toContain('MC-8');
      expect(stdout).toContain(relativePath);
    },
  );
});
