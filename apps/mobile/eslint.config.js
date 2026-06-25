// ESLint flat config (ESLint 9) — replaces the legacy .eslintrc.cjs.
//
// eslint-config-expo/flat provides the Expo + React + react-hooks (incl.
// `react-hooks/rules-of-hooks`) baseline; the blocks below layer altune's
// stricter rules on top. rules-of-hooks here is what catches the "hook after
// an early return" class of bug that previously shipped while ESLint was
// unrunnable. Lives in a standalone mobile package — see ADR-0016.
//
// AIDEV-NOTE: eslint-config-expo@10 stopped registering @typescript-eslint
// under that name for ESLint 9, which silently broke the lint gate. We now
// register the plugin + parser ourselves (scoped to ts/tsx) so the altune TS
// rules resolve regardless of what config-expo wires internally. Keep
// @typescript-eslint/* as direct devDependencies for the same reason.
const expoConfig = require('eslint-config-expo/flat');
const tsPlugin = require('@typescript-eslint/eslint-plugin');
const tsParser = require('@typescript-eslint/parser');

module.exports = [
  ...expoConfig,
  {
    files: ['**/*.{ts,tsx}'],
    plugins: { '@typescript-eslint': tsPlugin },
    languageOptions: { parser: tsParser },
    rules: {
      '@typescript-eslint/no-explicit-any': 'error',
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
      '@typescript-eslint/consistent-type-imports': ['error', { prefer: 'type-imports' }],
      'react-hooks/exhaustive-deps': 'error',
    },
  },
  {
    rules: {
      'no-console': ['warn', { allow: ['warn', 'error'] }],
      // Web-only rule: React Native <Text> renders apostrophes/quotes natively,
      // so HTML entity escaping (&apos; etc.) is wrong for this platform.
      'react/no-unescaped-entities': 'off',
    },
  },
  {
    // Inline mock components in test factories don't need display names.
    files: ['**/__tests__/**'],
    rules: { 'react/display-name': 'off' },
  },
  {
    ignores: ['node_modules/**', '.expo/**', 'dist/**', 'web-build/**', 'coverage/**'],
  },
];
