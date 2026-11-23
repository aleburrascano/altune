const path = require('node:path');
const { RuleTester } = require('eslint');
const { describe, it } = require('node:test');
const tsParser = require('@typescript-eslint/parser');
const rule = require('../test-imports-through-doors');

RuleTester.describe = describe;
RuleTester.it = it;
RuleTester.itOnly = it.only;

const ruleTester = new RuleTester({ languageOptions: { parser: tsParser } });

const client = path.resolve(__dirname, '..', '..', '..');
const featureTest = path.join(client, 'src/features/player/__tests__/queue.test.ts');
const sharedTest = path.join(client, 'src/shared/catalog/core/__tests__/ids.test.ts');
const mobileTest = path.join(client, 'src/mobile/__tests__/wiring.test.tsx');
const routeTest = path.join(client, 'src/app/__tests__/route.test.tsx');

const at = (filename, code) => ({ code, filename });

const flagged = (filename, code, module, importPath, door) => ({
  ...at(filename, code),
  errors: [{ messageId: 'behindDoor', data: { module, path: importPath, door } }],
});

ruleTester.run('test-imports-through-doors', rule, {
  valid: [
    at(featureTest, "import { createPlayer } from '@features/player';"),
    at(featureTest, "import { createPlayer } from '@/features/player';"),
    at(featureTest, "import { createPlayer } from '@features/player/index';"),
    at(featureTest, "import { trackId } from '@shared/catalog';"),
    at(featureTest, "import { fakeQueue } from './fakeQueue';"),
    at(featureTest, "import { fake } from '@features/player/__tests__/fakes';"),
    at(featureTest, "import { signInTestUser } from '@testing/signInTestUser';"),
    at(featureTest, "import { render } from '@testing-library/react-native';"),
    at(featureTest, "import appJson from '@/../app.json';"),
    at(featureTest, "const actual = jest.requireActual('@shared/catalog');"),
    at(sharedTest, "import { trackId } from '@shared/catalog';"),
    at(sharedTest, "import { clamp } from '@shared/math';"),
    at(mobileTest, "import { RootLayout } from '@/mobile';"),
    at(mobileTest, "import { createPlayer } from '@features/player';"),
    at(mobileTest, "import { trackId } from '@shared/catalog';"),
    at(routeTest, "import { inside } from '@features/player/core/inside';"),
  ],
  invalid: [
    flagged(
      featureTest,
      "import { queue } from '@features/player/core/queue';",
      'player',
      '@features/player/core/queue',
      '@features/player',
    ),
    flagged(
      featureTest,
      "import { queue } from '@/features/player/core/queue';",
      'player',
      '@/features/player/core/queue',
      '@features/player',
    ),
    flagged(
      featureTest,
      "import { DetailScreen } from '@features/detail';",
      'player',
      '@features/detail',
      '@features/player',
    ),
    flagged(
      featureTest,
      "import { ids } from '@shared/catalog/core/ids';",
      'player',
      '@shared/catalog/core/ids',
      '@features/player',
    ),
    flagged(
      featureTest,
      "import { RootLayout } from '@/mobile';",
      'player',
      '@/mobile',
      '@features/player',
    ),
    flagged(
      featureTest,
      "import Tab from '@/app/(tabs)/index';",
      'player',
      '@/app/(tabs)/index',
      '@features/player',
    ),
    flagged(
      featureTest,
      "import { helper } from '@shared/catalog/__tests__/helper';",
      'player',
      '@shared/catalog/__tests__/helper',
      '@features/player',
    ),
    flagged(
      featureTest,
      "export { queue } from '@features/player/core/queue';",
      'player',
      '@features/player/core/queue',
      '@features/player',
    ),
    flagged(
      featureTest,
      "const m = import('@features/player/core/queue');",
      'player',
      '@features/player/core/queue',
      '@features/player',
    ),
    flagged(
      featureTest,
      "const { queue } = require('@features/player/core/queue');",
      'player',
      '@features/player/core/queue',
      '@features/player',
    ),
    flagged(
      featureTest,
      "const actual = jest.requireActual('@features/player/core/queue');",
      'player',
      '@features/player/core/queue',
      '@features/player',
    ),
    flagged(sharedTest, "import { ids } from '../ids';", 'catalog', '../ids', '@shared/catalog'),
    flagged(
      sharedTest,
      "import { createPlayer } from '@features/player';",
      'catalog',
      '@features/player',
      '@shared/catalog',
    ),
    flagged(
      mobileTest,
      "import { wire } from '@/mobile/wiring/wire';",
      'mobile',
      '@/mobile/wiring/wire',
      '@/mobile',
    ),
    flagged(
      mobileTest,
      "import { queue } from '@features/player/core/queue';",
      'mobile',
      '@features/player/core/queue',
      '@/mobile',
    ),
  ],
});
