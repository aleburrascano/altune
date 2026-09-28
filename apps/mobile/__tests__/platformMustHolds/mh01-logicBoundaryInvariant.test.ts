import { ESLint } from 'eslint';
import * as path from 'node:path';

const CWD = path.join(__dirname, '..', '..');
const LOGIC_FILE = 'src/features/discover/searchLimits.ts';
const RULE = 'no-restricted-imports';

const eslint = new ESLint({
  cwd: CWD,
  overrideConfigFile: true,
  baseConfig: require('../../eslint.config.js'),
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
