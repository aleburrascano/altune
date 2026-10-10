import * as fs from 'node:fs';
import * as os from 'node:os';
import * as path from 'node:path';
import { execFileSync } from 'node:child_process';

const mobileRoot = path.join(__dirname, '..', '..');
const script = path.join(mobileRoot, 'scripts', 'module-shape.mjs');

type Tree = Record<string, string>;

const cleanFeature = (name: string): Tree => ({
  [`src/features/${name}/index.ts`]: 'export {};\n',
  [`src/features/${name}/domain/rule.ts`]: 'export const rule = 1;\n',
  [`src/features/${name}/ports/thing.ts`]: 'export interface Thing {}\n',
  [`src/features/${name}/adapters/thingAdapter.ts`]: 'export const thingAdapter = 1;\n',
  [`src/features/${name}/hooks/useThing.ts`]: 'export const useThing = 1;\n',
  [`src/features/${name}/ui/Screen.tsx`]: 'export const Screen = 1;\n',
});

const cleanTree = (): Tree => cleanFeature('library');

function run(tree: Tree, args: string[] = [], baseline = '') {
  const root = fs.mkdtempSync(path.join(process.env.TMPDIR ?? os.tmpdir(), 'module-shape-'));
  for (const [file, content] of Object.entries({ ...tree, 'module-shape.baseline': baseline })) {
    fs.mkdirSync(path.dirname(path.join(root, file)), { recursive: true });
    fs.writeFileSync(path.join(root, file), content);
  }
  const goApi = path.join(root, 'go-api');
  fs.mkdirSync(path.join(goApi, 'discovery'), { recursive: true });
  fs.mkdirSync(path.join(goApi, 'library'), { recursive: true });
  try {
    const stdout = execFileSync('node', [script, '--root', root, '--go-api', goApi, ...args], {
      encoding: 'utf8',
    });
    return { status: 0, stdout, root };
  } catch (error) {
    const failure = error as { status: number | null; stdout?: string };
    return { status: failure.status, stdout: failure.stdout ?? '', root };
  }
}

test('a conforming tree exits 0', () => {
  const { status, stdout } = run(cleanTree());
  expect(status).toBe(0);
  expect(stdout).toContain('module-shape: 0 violations, all in baseline');
});

describe('M1', () => {
  test('fails on a loose root file and a missing folder', () => {
    const { status, stdout } = run({
      ...cleanTree(),
      'src/features/library/gridColumns.ts': 'export const gridColumns = 1;\n',
      'src/features/library/domain/rule.ts': 'export const rule = 1;\n',
    });
    expect(status).toBe(1);
    expect(stdout).toContain(
      'new violation: M1 src/features/library/gridColumns.ts loose root file',
    );
  });

  test('fails on a missing index.ts and a missing domain/', () => {
    const tree = cleanTree();
    delete tree['src/features/library/index.ts'];
    delete tree['src/features/library/domain/rule.ts'];
    const { stdout } = run({
      ...tree,
      'src/features/library/ui/Other.tsx': 'export const Other = 1;\n',
    });
    expect(stdout).toContain('M1 src/features/library missing index.ts');
    expect(stdout).toContain('M1 src/features/library missing domain/');
  });

  test('passes a feature with no ports/ folder', () => {
    const tree = cleanTree();
    delete tree['src/features/library/ports/thing.ts'];
    const { status, stdout } = run(tree);
    expect(status).toBe(0);
    expect(stdout).not.toContain('missing ports/');
  });
});

describe('M2', () => {
  test('passes on a barrel import and fails on a deep one', () => {
    const ok = run({
      ...cleanTree(),
      'src/app/screen.tsx': "import { x } from '@features/library';\nexport const s = x;\n",
    });
    expect(ok.status).toBe(0);

    const bad = run({
      ...cleanTree(),
      'src/app/screen.tsx': "import { x } from '@features/library/ui';\nexport const s = x;\n",
    });
    expect(bad.status).toBe(1);
    expect(bad.stdout).toContain(
      'new violation: M2 src/app/screen.tsx deep import @features/library/ui',
    );
  });

  test('fails on a deep import of another feature but not of its own', () => {
    const own = run({
      ...cleanTree(),
      'src/features/library/ui/Own.tsx':
        "import { y } from '@features/library/domain/rule';\nexport const Own = y;\n",
    });
    expect(own.status).toBe(0);

    const other = run({
      ...cleanTree(),
      ...cleanFeature('settings'),
      'src/features/settings/ui/Cross.tsx':
        "import { y } from '@features/library/domain/rule';\nexport const Cross = y;\n",
    });
    expect(other.stdout).toContain(
      'M2 src/features/settings/ui/Cross.tsx deep import @features/library/domain/rule',
    );
  });
});

describe('M4', () => {
  test('passes on a use* file in hooks/ and fails on one in ui/', () => {
    expect(run(cleanTree()).status).toBe(0);
    const bad = run({
      ...cleanTree(),
      'src/features/library/ui/useAddable.ts': 'export const useAddable = 1;\n',
    });
    expect(bad.status).toBe(1);
    expect(bad.stdout).toContain(
      'new violation: M4 src/features/library/ui/useAddable.ts use* file outside hooks/',
    );
  });

  test('fails on a non-use* file in hooks/', () => {
    const { stdout } = run({
      ...cleanTree(),
      'src/features/library/hooks/gate.ts': 'export const gate = 1;\n',
    });
    expect(stdout).toContain('M4 src/features/library/hooks/gate.ts non-use* file in hooks/');
  });
});

describe('M7', () => {
  const factories = 'export const trackFactory = 1;\n';

  test('passes when a factory is imported from @testing/factories', () => {
    const { status } = run({
      ...cleanTree(),
      'jest/factories.ts': factories,
      'src/features/library/__tests__/a.test.ts':
        "import { trackFactory } from '@testing/factories';\nexport const a = trackFactory;\n",
    });
    expect(status).toBe(0);
  });

  test('fails when no file under src/ imports a factory', () => {
    const { status, stdout } = run({ ...cleanTree(), 'jest/factories.ts': factories });
    expect(status).toBe(1);
    expect(stdout).toContain(
      'new violation: M7 jest/factories.ts trackFactory has no importer under src/',
    );
  });
});

describe('N1', () => {
  test('passes on a named file and fails on types.ts and a lib folder', () => {
    expect(
      run({ ...cleanTree(), 'src/shared/trackKey.ts': 'export const trackKey = 1;\n' }).status,
    ).toBe(0);
    const bad = run({
      ...cleanTree(),
      'src/shared/api/types.ts': 'export type A = 1;\nexport type B = 2;\n',
      'src/shared/lib/trackKey.ts': 'export const trackKey = 1;\n',
    });
    expect(bad.status).toBe(1);
    expect(bad.stdout).toContain('new violation: N1 src/shared/api/types.ts generic name');
    expect(bad.stdout).toContain('new violation: N1 src/shared/lib generic name');
  });
});

describe('N2', () => {
  test('passes when the feature has the go-api module name and fails on a near miss', () => {
    expect(run(cleanFeature('library')).status).toBe(0);
    const bad = run(cleanFeature('discover'));
    expect(bad.status).toBe(1);
    expect(bad.stdout).toContain(
      'new violation: N2 src/features/discover go-api module is discovery',
    );
  });
});

describe('N3', () => {
  test('fails on a kebab-case file under src/shared', () => {
    const bad = run({
      ...cleanTree(),
      'src/shared/track-key.ts': 'export function trackKey() {}\n',
    });
    expect(bad.status).toBe(1);
    expect(bad.stdout).toContain('new violation: N3 src/shared/track-key.ts is not camelCase');
  });

  test('passes a single-export file named for its concept', () => {
    const { status } = run({
      ...cleanTree(),
      'src/shared/trackKey.ts': 'export function makeKey() {}\n',
    });
    expect(status).toBe(0);
  });

  test('passes a file holding several related exports', () => {
    const { status } = run({
      ...cleanTree(),
      'src/shared/trackKey.ts': 'export const a = 1;\nexport const b = 2;\n',
    });
    expect(status).toBe(0);
  });

  test('fails on a kebab-case folder', () => {
    const bad = run({
      ...cleanTree(),
      'src/shared/search-history/history.ts': 'export const history = 1;\n',
    });
    expect(bad.stdout).toContain('new violation: N3 src/shared/search-history is not camelCase');
    expect(bad.stdout).not.toContain('search-history/history.ts');
  });

  test('wants PascalCase for a component file and camelCase for any other', () => {
    const component = 'export function TrackRow(): null {\n  return null;\n}\n';
    expect(run({ ...cleanTree(), 'src/shared/ui/TrackRow.tsx': component }).status).toBe(0);
    const camelComponent = run({ ...cleanTree(), 'src/shared/ui/trackRow.tsx': component });
    expect(camelComponent.stdout).toContain(
      'new violation: N3 src/shared/ui/trackRow.tsx is not PascalCase',
    );
    const pascalModule = run({
      ...cleanTree(),
      'src/shared/TrackKey.ts': 'export const trackKey = 1;\n',
    });
    expect(pascalModule.stdout).toContain(
      'new violation: N3 src/shared/TrackKey.ts is not camelCase',
    );
  });

  test('reads the default export as the main export of a .tsx file', () => {
    const tree = {
      ...cleanTree(),
      'src/shared/ui/sheet.tsx': 'export default function Sheet() {}\nexport const x = 1;\n',
    };
    expect(run(tree).stdout).toContain(
      'new violation: N3 src/shared/ui/sheet.tsx is not PascalCase',
    );
  });

  test('keeps camelCase for a .tsx concept that mixes a component with plain exports', () => {
    const source = 'export function dismissKeyboard() {}\nexport function DismissArea() {}\n';
    expect(run({ ...cleanTree(), 'src/shared/ui/keyboard.tsx': source }).status).toBe(0);
  });

  test('allows platform suffixes, index.ts and the app folder', () => {
    const tree = {
      ...cleanTree(),
      'src/shared/haptics.web.ts': 'export const tap = 1;\n',
      'src/shared/haptics.native.ts': 'export const tap = 1;\n',
      'src/shared/haptics.android.ts': 'export const tap = 1;\n',
      'src/shared/haptics.ios.ts': 'export const tap = 1;\n',
      'src/shared/index.ts': 'export {};\n',
      'src/app/(tabs)/now-playing.tsx': 'export default function Screen() {}\n',
    };
    expect(run(tree).status).toBe(0);
  });
});

describe('baseline', () => {
  const drift = {
    ...cleanTree(),
    'src/features/library/ui/useAddable.ts': 'export const useAddable = 1;\n',
  };
  const line = 'M4 src/features/library/ui/useAddable.ts use* file outside hooks/';

  test('a violation listed in the baseline passes', () => {
    const { status, stdout } = run(drift, [], `${line}\n`);
    expect(status).toBe(0);
    expect(stdout).toContain('module-shape: 1 violations, all in baseline');
  });

  test('a baseline line that no longer occurs fails with a removal instruction', () => {
    const { status, stdout } = run(cleanTree(), [], `${line}\n`);
    expect(status).toBe(1);
    expect(stdout).toContain(`fixed: remove it from the baseline: ${line}`);
  });

  test('--write regenerates the baseline, sorted', () => {
    const { status, root } = run(drift, ['--write']);
    expect(status).toBe(0);
    expect(fs.readFileSync(path.join(root, 'module-shape.baseline'), 'utf8')).toBe(`${line}\n`);
  });
});

test('the real tree has every violation in the baseline', () => {
  const stdout = execFileSync('node', [script], { cwd: mobileRoot, encoding: 'utf8' });
  expect(stdout).toMatch(/^module-shape: \d+ violations, all in baseline$/m);
});
