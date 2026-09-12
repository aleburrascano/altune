const expoConfig = require('eslint-config-expo/flat');
const tsPlugin = require('@typescript-eslint/eslint-plugin');
const tsParser = require('@typescript-eslint/parser');
// Typed (type-aware) linting. These rules use the TypeScript type-checker, so
// they catch what plain lint cannot: promises that are never awaited, promises
// passed where a sync value is expected, and other type-level footguns.
//
// This is a CURATED slice of typescript-eslint's strict-type-checked, not the
// whole thing: the full set flags ~2300 pre-existing issues, ~1400 of them
// no-unsafe-* from `any` crossing untyped boundaries — a real but separate
// typing project. The rules enabled here are the high-signal bug-catchers whose
// backlog was small enough to fix outright, so the gate stays meaningful and
// blocking rather than a wall of warnings. Grow this list as the codebase is
// hardened. `projectService` finds the nearest tsconfig per file automatically.
// Scoped to production src, not tests: type-aware linting needs each file in the
// tsconfig project (a stray test file errors otherwise), and an un-awaited
// promise in a test fails the test loudly anyway — production is where a floating
// promise silently drops work. require-await is deliberately absent: it fights
// async methods that exist only to satisfy a Promise-returning interface.
const typedLinting = [
  {
    files: ['src/**/*.{ts,tsx}'],
    ignores: ['**/__tests__/**'],
    plugins: { '@typescript-eslint': tsPlugin },
    languageOptions: {
      parser: tsParser,
      parserOptions: { projectService: true, tsconfigRootDir: __dirname },
    },
    rules: {
      '@typescript-eslint/no-floating-promises': 'error',
      '@typescript-eslint/no-misused-promises': 'error',
      '@typescript-eslint/await-thenable': 'error',
      '@typescript-eslint/no-misused-spread': 'error',
      '@typescript-eslint/use-unknown-in-catch-callback-variable': 'error',
      '@typescript-eslint/no-unnecessary-type-assertion': 'error',
    },
  },
];

const FEATURES = ['auth', 'detail', 'discover', 'library', 'playback', 'settings'];

const TEST_FILES = ['**/__tests__/**'];

const FILES_WHERE_EXPO_GO_FORCES_CONDITIONAL_REQUIRE = [
  'src/app/_layout.tsx',
  'src/features/playback/hooks/PlaybackProvider.tsx',
];

const featureIsolationZones = [
  ...FEATURES.map((feature) => ({
    target: `./src/features/${feature}`,
    from: './src/features',
    except: [`./${feature}`],
    message: 'Features must not import each other — promote shared code to src/shared.',
  })),
  {
    target: './src/shared',
    from: './src/features',
    message: 'src/shared must not import from src/features.',
  },
];

const typeScriptPluginRegisteredDirectlyRatherThanViaExpoConfig = {
  files: ['**/*.{ts,tsx}'],
  plugins: { '@typescript-eslint': tsPlugin },
  languageOptions: { parser: tsParser },
  rules: {
    '@typescript-eslint/no-explicit-any': 'error',
    '@typescript-eslint/no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
    '@typescript-eslint/consistent-type-imports': ['error', { prefer: 'type-imports' }],
    'react-hooks/exhaustive-deps': 'error',
  },
};

const rulesWrittenForTheWebAndWrongForReactNative = {
  rules: {
    'react/no-unescaped-entities': 'off',
  },
};

// SDK 57's eslint-config-expo bundles eslint-plugin-react-hooks v6, whose React
// Compiler ruleset (refs / purity / set-state-in-effect) gates these three
// rules. Their offending sites were fixed in #217 (refs read in effects and
// handlers rather than during render, pure helpers kept pure, no set-state in
// effects), so they are restored to errors here alongside the historical hook
// gates: rules-of-hooks (from expo config) and exhaustive-deps (set above).
const reactCompilerRulesRestoredToErrors = {
  rules: {
    'react-hooks/refs': 'error',
    'react-hooks/purity': 'error',
    'react-hooks/set-state-in-effect': 'error',
  },
};

const relaxationsForJestModuleMockingAndInlineMockComponents = {
  files: TEST_FILES,
  rules: {
    '@typescript-eslint/no-require-imports': 'off',
    'react/display-name': 'off',
    'import/no-named-as-default-member': 'off',
  },
};

const relaxationForNativeModulesExpoGoDoesNotBundle = {
  files: FILES_WHERE_EXPO_GO_FORCES_CONDITIONAL_REQUIRE,
  rules: {
    '@typescript-eslint/no-require-imports': 'off',
  },
};

module.exports = [
  ...expoConfig,
  ...typedLinting,
  typeScriptPluginRegisteredDirectlyRatherThanViaExpoConfig,
  {
    files: ['src/**/*.{ts,tsx}'],
    ignores: TEST_FILES,
    rules: {
      'import/no-restricted-paths': [
        'error',
        { basePath: __dirname, zones: featureIsolationZones },
      ],
    },
  },
  {
    rules: {
      'no-console': ['warn', { allow: ['warn', 'error'] }],
    },
  },
  rulesWrittenForTheWebAndWrongForReactNative,
  reactCompilerRulesRestoredToErrors,
  relaxationsForJestModuleMockingAndInlineMockComponents,
  relaxationForNativeModulesExpoGoDoesNotBundle,
  {
    ignores: ['node_modules/**', '.expo/**', 'dist/**', 'web-build/**', 'coverage/**'],
  },
];
