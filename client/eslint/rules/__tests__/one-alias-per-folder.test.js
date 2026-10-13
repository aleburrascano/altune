const { RuleTester } = require('eslint');
const { describe, it } = require('node:test');
const tsParser = require('@typescript-eslint/parser');
const rule = require('../one-alias-per-folder');

RuleTester.describe = describe;
RuleTester.it = it;
RuleTester.itOnly = it.only;

const ruleTester = new RuleTester({ languageOptions: { parser: tsParser } });

const flagged = (path, folder) => [{ messageId: 'useOwnAlias', data: { path, folder } }];

ruleTester.run('one-alias-per-folder', rule, {
  valid: [
    { code: "import { x } from '@features/library/x';" },
    { code: "import { y } from '@shared/apiClient';" },
    { code: "import { Chrome } from '@/chrome/Chrome';" },
    { code: "const label = 'features/library';" },
  ],
  invalid: [
    {
      code: "import { x } from '@/features/library/x';",
      output: "import { x } from '@features/library/x';",
      errors: flagged('@/features/library/x', 'features'),
    },
    {
      code: 'import { y } from "@/shared/apiClient/y";',
      output: 'import { y } from "@shared/apiClient/y";',
      errors: flagged('@/shared/apiClient/y', 'shared'),
    },
    {
      code: "jest.mock('@/features/auth/store');",
      output: "jest.mock('@features/auth/store');",
      errors: flagged('@/features/auth/store', 'features'),
    },
  ],
});
