// ESLint flat config (ESLint 10) — replaces the legacy .eslintrc.cjs.
//
// eslint-config-expo/flat provides the Expo + React + react-hooks (incl.
// `react-hooks/rules-of-hooks`) + TypeScript baseline; the block below layers
// altune's stricter rules on top. rules-of-hooks here is what catches the
// "hook after an early return" class of bug that previously shipped while
// ESLint was unrunnable. Lives in a standalone mobile package — see ADR-0016.
const expoConfig = require('eslint-config-expo/flat');

module.exports = [
  ...expoConfig,
  {
    rules: {
      '@typescript-eslint/no-explicit-any': 'error',
      '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
      '@typescript-eslint/consistent-type-imports': ['error', { prefer: 'type-imports' }],
      'no-console': ['warn', { allow: ['warn', 'error'] }],
      'react-hooks/exhaustive-deps': 'error',
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
