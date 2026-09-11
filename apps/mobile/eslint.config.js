const expoConfig = require('eslint-config-expo/flat');
const tsPlugin = require('@typescript-eslint/eslint-plugin');
const tsParser = require('@typescript-eslint/parser');

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
// Compiler ruleset (refs / purity / set-state-in-effect) is brand new and did
// NOT gate before this upgrade. `refs` was adopted for real in #217 — every site
// now reads refs in effects/handlers rather than during render, so it stays at
// eslint-config-expo's default (error), not downgraded here. `purity` and
// `set-state-in-effect` still misfire on idiomatic RN gesture/Animated code and
// on pure helpers called from event handlers; adopting them (and enabling the
// compiler) is its own effort, so they remain visible as warnings during the
// mechanical SDK bump rather than being silenced or contorting untestable
// animation code. The historical hook gates stay errors: rules-of-hooks (from
// expo config) and exhaustive-deps (set above).
const reactCompilerRulesDeferredToTheirOwnTicket = {
  rules: {
    'react-hooks/purity': 'warn',
    'react-hooks/set-state-in-effect': 'warn',
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
  reactCompilerRulesDeferredToTheirOwnTicket,
  relaxationsForJestModuleMockingAndInlineMockComponents,
  relaxationForNativeModulesExpoGoDoesNotBundle,
  {
    ignores: ['node_modules/**', '.expo/**', 'dist/**', 'web-build/**', 'coverage/**'],
  },
];
