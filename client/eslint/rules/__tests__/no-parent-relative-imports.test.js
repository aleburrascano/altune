const { RuleTester } = require('eslint');
const { describe, it } = require('node:test');
const tsParser = require('@typescript-eslint/parser');
const rule = require('../no-parent-relative-imports');

RuleTester.describe = describe;
RuleTester.it = it;
RuleTester.itOnly = it.only;

const ruleTester = new RuleTester({ languageOptions: { parser: tsParser } });

const flagged = (path) => [{ messageId: 'useAlias', data: { path } }];

ruleTester.run('no-parent-relative-imports', rule, {
  valid: [
    { code: "import { x } from '@features/library/x';" },
    { code: "import { y } from '@shared/apiClient';" },
    { code: "import { z } from './sibling';" },
    { code: "import appJson from '@/../app.json';" },
    { code: "jest.mock('@shared/apiClient/tracks');" },
    { code: "const { a } = require('@testing/doubles/fetch.js');" },
    { code: "const unsafeId = '../etc/passwd';" },
    { code: "expect(forbidden(\"import { x } from '../hooks/useThing';\")).toEqual([]);" },
    { code: "somethingElse.mock('../x');" },
  ],
  invalid: [
    { code: "import x from '../y';", errors: flagged('../y') },
    { code: "import type { T } from '../../deep/t';", errors: flagged('../../deep/t') },
    { code: "export { x } from '../y';", errors: flagged('../y') },
    { code: "export * from '../y';", errors: flagged('../y') },
    { code: "const m = import('../y');", errors: flagged('../y') },
    { code: "const { a } = require('../y');", errors: flagged('../y') },
    { code: "jest.mock('../y');", errors: flagged('../y') },
    { code: "jest.mock('../y', () => ({}));", errors: flagged('../y') },
    { code: "jest.doMock('../y');", errors: flagged('../y') },
    { code: "jest.requireActual('../y');", errors: flagged('../y') },
    { code: "import up from '..';", errors: flagged('..') },
  ],
});
