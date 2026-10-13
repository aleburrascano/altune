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
  [`src/features/${name}/ports/track.ts`]: 'export interface Track {}\n',
  [`src/features/${name}/adapters/http/trackAdapter.ts`]: 'export const trackAdapter = 1;\n',
  [`src/features/${name}/hooks/play/usePlay.ts`]: 'export const usePlay = 1;\n',
  [`src/features/${name}/ui/play/Screen.tsx`]: 'export const Screen = 1;\n',
});

const cleanTree = (): Tree => cleanFeature('library');

function run(tree: Tree, args: string[] = [], baseline = '') {
  const root = fs.mkdtempSync(path.join(process.env.TMPDIR ?? os.tmpdir(), 'module-shape-'));
  for (const [file, content] of Object.entries({ ...tree, 'baselines/module-shape.baseline': baseline })) {
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
      'src/features/library/ui/play/Own.tsx':
        "import { y } from '@features/library/domain/rule';\nexport const Own = y;\n",
    });
    expect(own.status).toBe(0);

    const other = run({
      ...cleanTree(),
      ...cleanFeature('settings'),
      'src/features/settings/ui/play/Cross.tsx':
        "import { y } from '@features/library/domain/rule';\nexport const Cross = y;\n",
    });
    expect(other.stdout).toContain(
      'M2 src/features/settings/ui/play/Cross.tsx deep import @features/library/domain/rule',
    );
  });
});

describe('M4', () => {
  test('passes on a use* file in hooks/ and fails on one in ui/', () => {
    expect(run(cleanTree()).status).toBe(0);
    const bad = run({
      ...cleanTree(),
      'src/features/library/ui/play/useAddable.ts': 'export const useAddable = 1;\n',
    });
    expect(bad.status).toBe(1);
    expect(bad.stdout).toContain(
      'new violation: M4 src/features/library/ui/play/useAddable.ts use* file outside hooks/',
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
      run({ ...cleanTree(), 'src/shared/keys/trackKey.ts': 'export const trackKey = 1;\n' }).status,
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
      'src/shared/keys/trackKey.ts': 'export function makeKey() {}\n',
    });
    expect(status).toBe(0);
  });

  test('passes a file holding several related exports', () => {
    const { status } = run({
      ...cleanTree(),
      'src/shared/keys/trackKey.ts': 'export const a = 1;\nexport const b = 2;\n',
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
    expect(run({ ...cleanTree(), 'src/shared/ui/rows/TrackRow.tsx': component }).status).toBe(0);
    const camelComponent = run({ ...cleanTree(), 'src/shared/ui/rows/trackRow.tsx': component });
    expect(camelComponent.stdout).toContain(
      'new violation: N3 src/shared/ui/rows/trackRow.tsx is not PascalCase',
    );
    const pascalModule = run({
      ...cleanTree(),
      'src/shared/keys/TrackKey.ts': 'export const trackKey = 1;\n',
    });
    expect(pascalModule.stdout).toContain(
      'new violation: N3 src/shared/keys/TrackKey.ts is not camelCase',
    );
  });

  test('reads the default export as the main export of a .tsx file', () => {
    const tree = {
      ...cleanTree(),
      'src/shared/ui/dialog/sheet.tsx': 'export default function Sheet() {}\nexport const x = 1;\n',
    };
    expect(run(tree).stdout).toContain(
      'new violation: N3 src/shared/ui/dialog/sheet.tsx is not PascalCase',
    );
  });

  test('keeps camelCase for a .tsx concept that mixes a component with plain exports', () => {
    const source = 'export function dismissKeyboard() {}\nexport function DismissArea() {}\n';
    expect(run({ ...cleanTree(), 'src/shared/ui/keyboard/keyboard.tsx': source }).status).toBe(0);
  });

  test('allows platform suffixes, index.ts and the app folder', () => {
    const tree = {
      ...cleanTree(),
      'src/shared/haptics/haptics.web.ts': 'export const tap = 1;\n',
      'src/shared/haptics/haptics.native.ts': 'export const tap = 1;\n',
      'src/shared/haptics/haptics.android.ts': 'export const tap = 1;\n',
      'src/shared/haptics/haptics.ios.ts': 'export const tap = 1;\n',
      'src/shared/haptics/index.ts': 'export {};\n',
      'src/app/(tabs)/now-playing.tsx': 'export default function Screen() {}\n',
    };
    expect(run(tree).status).toBe(0);
  });
});

const compliantFeature = (name: string): Tree => ({
  [`src/features/${name}/index.ts`]: 'export {};\n',
  [`src/features/${name}/domain/rule.ts`]: 'export const rule = 1;\n',
  [`src/features/${name}/ports/track.ts`]: 'export interface Track {}\n',
  [`src/features/${name}/adapters/http/trackAdapter.ts`]: 'export const trackAdapter = 1;\n',
  [`src/features/${name}/hooks/play/usePlay.ts`]: 'export const usePlay = 1;\n',
  [`src/features/${name}/ui/play/Screen.tsx`]: 'export const Screen = 1;\n',
});

const compliantShared = (): Tree => ({
  'src/shared/keys/index.ts': 'export {};\n',
  'src/shared/keys/trackKey.ts': 'export const trackKey = 1;\n',
  'src/shared/ui/index.ts': 'export {};\n',
  'src/shared/ui/dialog/Dialog.tsx': 'export const Dialog = 1;\n',
});

const compliantTree = (): Tree => ({ ...compliantFeature('library'), ...compliantShared() });

const glossary = [
  '## Glossary',
  '',
  '- **Library** — a collection.',
  '- **Play** — a listen.',
  '- **Track** — a recording.',
  '- **Key** — a lookup key.',
  '- **Dialog** — a modal.',
  '',
  '## Structural words',
  '',
  '- `http`, `screen`, `ui`',
  '',
  '## Banned terms',
  '',
  '- **Song** — use Track.',
  '',
].join('\n');

const withGlossary = (tree: Tree): Tree => ({ ...tree, 'glossary.md': glossary });
const glossaryArgs = ['--glossary', 'glossary.md'];

const linesOf = (stdout: string, rule: string) =>
  stdout
    .split('\n')
    .filter((line) => line.startsWith(`new violation: ${rule} `))
    .map((line) => line.replace('new violation: ', ''));

describe('M8', () => {
  test('passes capability folders one level below hooks, ui and adapters', () => {
    const { status, stdout } = run(compliantTree());
    expect(status).toBe(0);
    expect(linesOf(stdout, 'M8')).toEqual([]);
  });

  test('flags a loose file, a deeper file, a reserved folder name and a hooks folder with no ui twin', () => {
    const { stdout } = run({
      ...compliantTree(),
      'src/features/library/hooks/useLoose.ts': 'export const useLoose = 1;\n',
      'src/features/library/ui/play/deep/Inner.tsx': 'export const Inner = 1;\n',
      'src/features/library/hooks/ports/usePorts.ts': 'export const usePorts = 1;\n',
      'src/features/library/hooks/queue/useQueue.ts': 'export const useQueue = 1;\n',
    });
    expect(linesOf(stdout, 'M8')).toEqual([
      'M8 src/features/library/hooks/ports folder named for a layer or feature',
      'M8 src/features/library/hooks/ports has no ui/ports/',
      'M8 src/features/library/hooks/queue has no ui/queue/',
      'M8 src/features/library/hooks/useLoose.ts loose file in hooks/',
      'M8 src/features/library/ui/play/deep/Inner.tsx nested more than one folder below ui/',
    ]);
  });

  test('applies to a layered shared folder without asking for a ui twin', () => {
    const tree = {
      ...compliantTree(),
      'src/shared/offline/index.ts': 'export {};\n',
      'src/shared/offline/ports/store.ts': 'export interface Store {}\n',
      'src/shared/offline/hooks/useStore.ts': 'export const useStore = 1;\n',
    };
    expect(linesOf(run(tree).stdout, 'M8')).toEqual([
      'M8 src/shared/offline/hooks/useStore.ts loose file in hooks/',
    ]);
  });
});

describe('M9', () => {
  test('passes a folder of files, a folder of folders, and platform variants of one stem', () => {
    const tree = {
      ...compliantTree(),
      'src/shared/keys/Haptics.ts': 'export const a = 1;\n',
      'src/shared/keys/Haptics.web.ts': 'export const a = 1;\n',
      'src/shared/keys/native/tap.ts': 'export const tap = 1;\n',
      'src/shared/keys/__tests__/trackKey.test.ts': 'export {};\n',
    };
    expect(linesOf(run(tree).stdout, 'M9')).toEqual([]);
  });

  test('flags a folder that holds files and folders, and one over sixteen stems', () => {
    const crowded = Object.fromEntries(
      Array.from({ length: 17 }, (_, i) => [
        `src/shared/crowd/file${String.fromCharCode(97 + i)}.ts`,
        'export const a = 1;\n',
      ]),
    );
    const { stdout } = run({
      ...compliantTree(),
      ...crowded,
      'src/shared/keys/nested/inner.ts': 'export const inner = 1;\n',
    });
    expect(linesOf(stdout, 'M9')).toEqual([
      'M9 src/shared/crowd holds more than 16 source files',
      'M9 src/shared/keys holds both files and folders',
    ]);
  });
});

describe('M10', () => {
  test('passes flat shared folders, layered shared folders and shared/ui families', () => {
    const tree = {
      ...compliantTree(),
      'src/shared/offline/index.ts': 'export {};\n',
      'src/shared/offline/ports/store.ts': 'export interface Store {}\n',
      'src/shared/offline/adapters/http/storeAdapter.ts': 'export const storeAdapter = 1;\n',
    };
    expect(linesOf(run(tree).stdout, 'M10')).toEqual([]);
  });

  test('flags a file at the shared root, a loose file in a layered or ui folder, and a folder in a flat one', () => {
    const { stdout } = run({
      ...compliantTree(),
      'src/shared/clock.ts': 'export const clock = 1;\n',
      'src/shared/ui/Loose.tsx': 'export const Loose = 1;\n',
      'src/shared/offline/ports/store.ts': 'export interface Store {}\n',
      'src/shared/offline/looseStore.ts': 'export const looseStore = 1;\n',
      'src/shared/keys/inner/deep.ts': 'export const deep = 1;\n',
    });
    expect(linesOf(stdout, 'M10')).toEqual([
      'M10 src/shared/clock.ts file at the shared root',
      'M10 src/shared/keys/inner folder inside a flat shared folder',
      'M10 src/shared/offline/looseStore.ts not a layer folder or index.ts',
      'M10 src/shared/ui/Loose.tsx loose file in shared/ui',
    ]);
  });
});

describe('M11', () => {
  test('passes the five allowed top-level entries', () => {
    const tree = {
      ...compliantTree(),
      'src/app/index.tsx': 'export default function Screen() {}\n',
      'src/shell/Frame.tsx': 'export const Frame = 1;\n',
      'src/wiring/ports.ts': 'export const ports = 1;\n',
    };
    expect(linesOf(run(tree).stdout, 'M11')).toEqual([]);
  });

  test('flags any other top-level entry', () => {
    const { stdout } = run({
      ...compliantTree(),
      'src/chrome/Frame.tsx': 'export const Frame = 1;\n',
      'src/stray.ts': 'export const stray = 1;\n',
    });
    expect(linesOf(stdout, 'M11')).toEqual([
      'M11 src/chrome not an allowed top-level entry',
      'M11 src/stray.ts not an allowed top-level entry',
    ]);
  });
});

describe('N4', () => {
  test('passes names without a vague word, and a glossary term that contains one', () => {
    const tree = {
      ...compliantTree(),
      'src/shared/keys/dataset.ts': 'export const dataset = 1;\n',
      'src/shared/keys/handleTap.ts': 'export const handleTap = 1;\n',
      'src/shared/keys/dataPoint.ts': 'export const dataPoint = 1;\n',
    };
    const { stdout } = run(
      { ...tree, 'glossary.md': glossary.replace('- **Track**', '- **Data** — a term.\n- **Track**') },
      glossaryArgs,
    );
    expect(linesOf(stdout, 'N4')).toEqual([]);
  });

  test('flags a vague word in a file name, a folder name and an exported name', () => {
    const { stdout } = run({
      ...compliantTree(),
      'src/shared/keys/trackInfo.ts': 'export const trackInfo = 1;\n',
      'src/shared/keys/fetcher.ts': 'export function fetchHelper() {}\nexport const KEY_V2 = 1;\n',
      'src/shared/keys/pairA.ts': 'export interface PairA {}\n',
      'src/shared/sharedUtil/index.ts': 'export {};\n',
    });
    expect(linesOf(stdout, 'N4')).toEqual([
      'N4 src/shared/keys/fetcher.ts exports KEY_V2 with the word V2',
      'N4 src/shared/keys/fetcher.ts exports fetchHelper with the word Helper',
      'N4 src/shared/keys/pairA.ts exports PairA with the word A',
      'N4 src/shared/keys/pairA.ts name has the word A',
      'N4 src/shared/keys/trackInfo.ts exports trackInfo with the word Info',
      'N4 src/shared/keys/trackInfo.ts name has the word Info',
      'N4 src/shared/sharedUtil name has the word Util',
    ]);
  });
});

describe('N5', () => {
  test('passes folder names made of glossary terms and structural words', () => {
    const { status, stdout } = run(withGlossary(compliantTree()), glossaryArgs);
    expect(status).toBe(0);
    expect(linesOf(stdout, 'N5')).toEqual([]);
  });

  test('flags a word that is neither a term nor a structural word, and a banned term', () => {
    const { stdout } = run(
      withGlossary({
        ...compliantTree(),
        'src/features/library/hooks/gizmo/useGizmo.ts': 'export const useGizmo = 1;\n',
        'src/features/library/ui/gizmo/Gizmo.tsx': 'export const Gizmo = 1;\n',
        'src/features/library/adapters/songStore/songStore.ts': 'export const songStore = 1;\n',
      }),
      glossaryArgs,
    );
    expect(linesOf(stdout, 'N5')).toEqual([
      'N5 src/features/library/adapters/songStore uses banned term song',
      'N5 src/features/library/adapters/songStore word Store is not in the glossary',
      'N5 src/features/library/hooks/gizmo word gizmo is not in the glossary',
      'N5 src/features/library/ui/gizmo word gizmo is not in the glossary',
    ]);
  });
});

describe('baseline', () => {
  const drift = {
    ...cleanTree(),
    'src/features/library/ui/play/useAddable.ts': 'export const useAddable = 1;\n',
  };
  const line = 'M4 src/features/library/ui/play/useAddable.ts use* file outside hooks/';

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
    expect(fs.readFileSync(path.join(root, 'baselines', 'module-shape.baseline'), 'utf8')).toBe(`${line}\n`);
  });
});

test('the real tree has every violation in the baseline', () => {
  const stdout = execFileSync('node', [script], { cwd: mobileRoot, encoding: 'utf8' });
  expect(stdout).toMatch(/^module-shape: \d+ violations, all in baseline$/m);
});
