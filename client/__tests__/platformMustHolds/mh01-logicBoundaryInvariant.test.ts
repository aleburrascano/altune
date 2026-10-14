import { ESLint } from 'eslint';
import { execFileSync } from 'node:child_process';
import * as path from 'node:path';

const CWD = path.join(__dirname, '..', '..');
const LOGIC_FILE = 'src/features/discovery/domain/searchPaging.ts';
const RULE = 'no-restricted-imports';
const RULE_ERROR = 2;

const eslint = new ESLint({
  cwd: CWD,
  overrideConfigFile: true,
  baseConfig: require('@/../eslint.config.js'),
});

async function ruleErrorsFor(text: string, filePath = LOGIC_FILE) {
  const [report] = await eslint.lintText(text, { filePath: path.join(CWD, filePath) });
  return report.messages.filter((message) => message.ruleId === RULE);
}

describe('feature logic stays platform-free', () => {
  it.each([
    ["import { Alert } from 'react-native';", '@shared/ui/dialog'],
    ["import type { ViewToken } from 'react-native';", '@shared/ui/dialog'],
    ["import { useRouter } from 'expo-router';", '@shared/navigation'],
    ["import Constants from 'expo-constants';", '@shared/device'],
    ["import { x } from 'react-native-track-player';", '@shared/lifecycle'],
    ["import { x } from '@react-native-async-storage/async-storage';", '@shared/lifecycle'],
    ["import { x } from 'react-native/Libraries/Foo';", '@shared/lifecycle'],
    ["export { Alert } from 'react-native';", '@shared/ui/dialog'],
  ])('rejects %s once, naming its home', async (source, home) => {
    const errors = await ruleErrorsFor(`${source}\n`);

    expect(errors).toHaveLength(1);
    expect(errors[0].message).toContain(home);
  });

  it('rejects a non-baselined logic file under hooks', async () => {
    const errors = await ruleErrorsFor(
      "import { Alert } from 'react-native';\nexport const a = Alert;\n",
      'src/features/library/hooks/useLibraryIsEmpty.ts',
    );

    expect(errors).toHaveLength(1);
  });

  it.each([
    'src/features/library/ui/X.tsx',
    'src/features/playback/native/x.ts',
    'src/features/playback/web/x.ts',
    'src/features/auth/hooks/x.web.ts',
    'src/features/auth/hooks/x.native.ts',
    'src/features/auth/__tests__/x.test.ts',
  ])('does not apply the rule to %s', async (filePath) => {
    const config = await eslint.calculateConfigForFile(path.join(CWD, filePath));

    expect(config.rules?.[RULE]).toBeUndefined();
  });
});

describe('shared logic stays platform-free', () => {
  const SHARED_FILE = 'src/shared/events/serverEventTypes.ts';

  it.each([
    ["import { AppState } from 'react-native';", '@shared/lifecycle'],
    ["import * as Crypto from 'expo-crypto';", '@shared/id/uuid'],
    ["import * as S from 'expo-secure-store';", '@shared/auth/authStorage'],
  ])('rejects %s once, naming its home', async (source, home) => {
    const errors = await ruleErrorsFor(`${source}\n`, SHARED_FILE);

    expect(errors).toHaveLength(1);
    expect(errors[0].message).toContain(home);
  });

  it('accepts importing the port', async () => {
    const errors = await ruleErrorsFor(
      "import { randomUuid } from '@shared/id/uuid';\n",
      SHARED_FILE,
    );

    expect(errors).toHaveLength(0);
  });

  it.each([
    'src/shared/device/deviceHelpers.ts',
    'src/shared/uikit/x.ts',
    'src/shared/id/uuidTools.ts',
    'src/shared/auth/authStorageHelpers.ts',
    'src/shared/auth/session/authStorage.ts',
    'src/shared/telemetry/x.ts',
  ])('applies the rule to the look-alike %s', async (filePath) => {
    const config = await eslint.calculateConfigForFile(path.join(CWD, filePath));

    expect(config.rules?.[RULE]?.[0]).toBe(RULE_ERROR);
  });

  it('refuses a deep react-native import in telemetry', async () => {
    const errors = await ruleErrorsFor(
      "import { x } from 'react-native/Libraries/vendor/core/ErrorUtils';\n",
      'src/shared/telemetry/outbox.ts',
    );

    expect(errors).toHaveLength(1);
  });

  it.each([
    'src/shared/ui/primitives/X.tsx',
    'src/shared/acquisition/ui/X.tsx',
    'src/shared/files/native/x.ts',
    'src/shared/auth/x.web.ts',
    'src/shared/auth/x.native.ts',
    'src/shared/device/device.ts',
    'src/shared/lifecycle/appState.ts',
    'src/shared/navigation/expoRouterNavigator.ts',
    'src/shared/id/uuid.ts',
    'src/shared/auth/authStorage.ts',
    'src/shared/events/__tests__/x.test.ts',
  ])('does not apply the rule to %s', async (filePath) => {
    const config = await eslint.calculateConfigForFile(path.join(CWD, filePath));

    expect(config.rules?.[RULE]).toBeUndefined();
  });

  it('applies the rule to supabaseClient.ts', async () => {
    const config = await eslint.calculateConfigForFile(
      path.join(CWD, 'src/shared/auth/supabaseClient.ts'),
    );

    expect(config.rules?.[RULE]?.[0]).toBe(RULE_ERROR);
  });

  it('finds no platform import anywhere in the real src/shared tree', () => {
    const script = `
      const { ESLint } = require('eslint');
      new ESLint({ cwd: process.cwd(), overrideConfigFile: true, baseConfig: require('./eslint.config.js') })
        .lintFiles(['src/shared/**/*.{ts,tsx}'])
        .then((results) => process.stdout.write(JSON.stringify(results.map((r) => ({
          filePath: r.filePath,
          messages: r.messages.map((m) => ({ ruleId: m.ruleId, message: m.message })),
        })))));
    `;
    const output = execFileSync(process.execPath, ['-e', script], {
      cwd: CWD,
      encoding: 'utf8',
      maxBuffer: 64 * 1024 * 1024,
    });
    const results: { filePath: string; messages: { ruleId: string | null; message: string }[] }[] =
      JSON.parse(output);
    const violations = results.flatMap((r) =>
      r.messages.filter((m) => m.ruleId === RULE).map((m) => `${r.filePath}: ${m.message}`),
    );

    expect(violations).toEqual([]);
    expect(
      results.some((r) => r.filePath.includes(`${path.sep}shared${path.sep}auth${path.sep}`)),
    ).toBe(true);
  }, 120000);
});
