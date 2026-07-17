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
    // Feature isolation (see apps/mobile/CLAUDE.md): features must not import
    // each other — code with 2+ feature consumers gets promoted to src/shared
    // (see src/shared/auth/* promotion notes) — and shared must never import
    // features. The TS resolver from config-expo resolves both alias and
    // relative imports, so `../../auth/...` escapes are caught too. Tests are
    // exempt: they compose features the way the app/ composition root does
    // (e.g. rendering a screen inside another feature's provider).
    files: ['src/**/*.{ts,tsx}'],
    ignores: ['**/__tests__/**'],
    rules: {
      'import/no-restricted-paths': [
        'error',
        {
          basePath: __dirname,
          zones: [
            ...['auth', 'detail', 'discover', 'library', 'playback', 'settings'].map(
              (feature) => ({
                target: `./src/features/${feature}`,
                from: './src/features',
                except: [`./${feature}`],
                message: 'Features must not import each other — promote shared code to src/shared.',
              }),
            ),
            {
              target: './src/shared',
              from: './src/features',
              message: 'src/shared must not import from src/features.',
            },
          ],
        },
      ],
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
