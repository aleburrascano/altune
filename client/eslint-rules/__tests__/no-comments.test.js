const { RuleTester } = require('eslint');
const { describe, it } = require('node:test');
const tsParser = require('@typescript-eslint/parser');
const rule = require('../no-comments');

RuleTester.describe = describe;
RuleTester.it = it;
RuleTester.itOnly = it.only;

const jsxOptions = {
  languageOptions: {
    parser: tsParser,
    parserOptions: { ecmaFeatures: { jsx: true } },
  },
};

const ruleTester = new RuleTester();

ruleTester.run('no-comments', rule, {
  valid: [
    { code: "const url = 'https://x.dev';" },
    { code: 'const s = "// not a comment";' },
    { code: 'const re = /\\/\\//;' },
    { code: 'const x = <p>// hi</p>;', filename: 'valid.tsx', ...jsxOptions },
    { code: '#!/usr/bin/env node\nconst a = 1;' },
  ],
  invalid: [
    {
      code: 'const a = 1; // x',
      errors: [{ messageId: 'comment' }],
    },
    {
      code: '/* x */ f();',
      errors: [{ messageId: 'comment' }],
    },
    {
      code: 'const x = <p>{/* x */}</p>;',
      filename: 'invalid.tsx',
      ...jsxOptions,
      errors: [{ messageId: 'comment' }],
    },
    {
      code: '// @ts-expect-error\nconst a = 1;',
      errors: [{ messageId: 'comment' }],
    },
  ],
});
