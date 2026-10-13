const { RuleTester } = require('eslint');
const { describe, it } = require('node:test');
const tsParser = require('@typescript-eslint/parser');
const rule = require('../no-raw-style-numbers');

RuleTester.describe = describe;
RuleTester.it = it;
RuleTester.itOnly = it.only;

const ruleTester = new RuleTester({ languageOptions: { parser: tsParser } });

const flagged = [{ messageId: 'nameIt' }];

ruleTester.run('no-raw-style-numbers', rule, {
  valid: [
    { code: 'const styles = StyleSheet.create({ a: { margin: 0, opacity: 1, borderWidth: 1 } });' },
    { code: 'const styles = StyleSheet.create({ a: { flex: 3 } });' },
    { code: 'const styles = StyleSheet.create({ a: { marginLeft: -1 } });' },
    { code: 'const styles = StyleSheet.create({ a: { width: SIZE, gap: spacing.sm } });' },
    { code: 'const styles = StyleSheet.create({ a: { transform: [{ rotate: "-8deg" }] } });' },
    { code: 'const DELAY = 700; run(DELAY);' },
    { code: 'const margin = { width: 44 };' },
  ],
  invalid: [
    { code: 'const styles = StyleSheet.create({ a: { width: 44 } });', errors: flagged },
    { code: 'const styles = StyleSheet.create({ a: { opacity: 0.6 } });', errors: flagged },
    { code: 'const styles = StyleSheet.create({ a: { marginLeft: -7 } });', errors: flagged },
    { code: 'const styles = StyleSheet.create({ a: { gap: spacing.sm + 2 } });', errors: flagged },
    {
      code: 'const styles = StyleSheet.create({ a: { transform: [{ scale: 1.06 }] } });',
      errors: flagged,
    },
    {
      code: 'const styles = StyleSheet.create({ a: { width: 44, height: 44 } });',
      errors: [...flagged, ...flagged],
    },
  ],
});
