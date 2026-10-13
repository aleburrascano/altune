const { RuleTester } = require('eslint');
const { describe, it } = require('node:test');
const tsParser = require('@typescript-eslint/parser');
const rule = require('../top-level-function-declarations');

RuleTester.describe = describe;
RuleTester.it = it;
RuleTester.itOnly = it.only;

const ruleTester = new RuleTester({ languageOptions: { parser: tsParser } });

const flagged = [{ messageId: 'useDeclaration' }];

ruleTester.run('top-level-function-declarations', rule, {
  valid: [
    { code: 'export function f() { return 1; }' },
    { code: 'function f() { const g = () => 1; return g(); }' },
    { code: 'export const Row = memo(() => null);' },
    { code: 'const LIMIT = 3;' },
    { code: 'xs.map((x) => x + 1);' },
  ],
  invalid: [
    { code: 'const f = () => 1;', errors: flagged },
    { code: 'export const f = async (x: number) => x;', errors: flagged },
    { code: 'const f = function () { return 1; };', errors: flagged },
    { code: 'export const f: () => number = () => 1;', errors: flagged },
  ],
});
