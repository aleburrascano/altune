const { RuleTester } = require('eslint');
const { describe, it } = require('node:test');
const tsParser = require('@typescript-eslint/parser');
const rule = require('../no-test-code-in-app');

RuleTester.describe = describe;
RuleTester.it = it;
RuleTester.itOnly = it.only;

const ruleTester = new RuleTester({ languageOptions: { parser: tsParser } });

const flagged = (path) => [{ messageId: 'noTestCode', data: { path } }];

ruleTester.run('no-test-code-in-app', rule, {
  valid: [
    { code: "import { x } from '@features/library';" },
    { code: "export { warn } from '@shared/log/shell/log';" },
    { code: "import { y } from '@shared/testingTools';" },
    { code: "import { z } from '@features/library/my__tests__helper';" },
    { code: "const label = '@testing/fakes/log/inMemoryLogger';" },
    { code: "const a = require('@shared/files');" },
  ],
  invalid: [
    {
      code: "export { authPortWrapper } from '@features/signIn/__tests__/testUtils/authTestUtils';",
      errors: flagged('@features/signIn/__tests__/testUtils/authTestUtils'),
    },
    {
      code: "export * from '@shared/log/__tests__/recordLogs';",
      errors: flagged('@shared/log/__tests__/recordLogs'),
    },
    {
      code: "import { recordLogs } from '@testing/fakes/log/inMemoryLogger';",
      errors: flagged('@testing/fakes/log/inMemoryLogger'),
    },
    {
      code: "import type { RecordedLogs } from '@testing/fakes/log/inMemoryLogger';",
      errors: flagged('@testing/fakes/log/inMemoryLogger'),
    },
    { code: "import { x } from './__tests__/fixtures';", errors: flagged('./__tests__/fixtures') },
    {
      code: "const { driveAppState } = require('@testing/fakes/lifecycle/inMemoryAppStateSource');",
      errors: flagged('@testing/fakes/lifecycle/inMemoryAppStateSource'),
    },
    {
      code: "const fixtures = import('@features/player/__tests__/fixtures');",
      errors: flagged('@features/player/__tests__/fixtures'),
    },
  ],
});
