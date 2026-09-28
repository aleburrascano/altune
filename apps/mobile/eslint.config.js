const fs = require('fs');
const path = require('path');
const { sync: globSync } = require('glob');
const expoConfig = require('eslint-config-expo/flat');
const tsPlugin = require('@typescript-eslint/eslint-plugin');
const tsParser = require('@typescript-eslint/parser');
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

function platformLegacyPathsForRule(ruleKey) {
  return globSync('src/{features/*,app}/platform-legacy.json', { cwd: __dirname }).flatMap(
    (relativePath) => {
      const contents = JSON.parse(fs.readFileSync(path.join(__dirname, relativePath), 'utf8'));
      return contents[ruleKey] || [];
    },
  );
}

const noInlinePlatformBranchesInFeatureUi = {
  name: 'noInlinePlatformBranchesInFeatureUi',
  files: ['src/features/*/ui/**/*.{ts,tsx}', 'src/app/**/*.{ts,tsx}'],
  ignores: [...TEST_FILES, ...platformLegacyPathsForRule('ui')],
  rules: {
    'no-restricted-syntax': [
      'error',
      {
        selector: "MemberExpression[object.name='Platform'][property.name='OS']",
        message:
          'Feature UI must stay platform-neutral: split this into Foo.tsx + Foo.web.tsx, or move the divergent capability behind a port in src/shared/.',
      },
      {
        selector: "MemberExpression[object.name='Keyboard'][property.name='dismiss']",
        message:
          'Feature UI must stay platform-neutral: split this into Foo.tsx + Foo.web.tsx, or move the divergent capability behind a port in src/shared/.',
      },
    ],
  },
};

const CAPABILITY_HOMES =
  'Feature logic must not import a platform API: Alert goes to @shared/ui/dialog, Keyboard to @shared/ui/keyboard, AppState to @shared/lifecycle, Platform and expo-constants to @shared/device, expo-router to @shared/navigation; or move the code into a ui/, native/ or web/ folder, or a .web.ts(x) split file.';

const featureLogicStaysPlatformFree = {
  name: 'featureLogicStaysPlatformFree',
  files: ['src/features/**/*.{ts,tsx}'],
  ignores: [
    ...TEST_FILES,
    'src/features/*/ui/**',
    'src/features/**/native/**',
    'src/features/**/web/**',
    'src/features/**/*.web.{ts,tsx}',
    'src/features/**/*.native.{ts,tsx}',
    ...platformLegacyPathsForRule('logic'),
  ],
  rules: {
    'no-restricted-imports': [
      'error',
      {
        patterns: [
          {
            group: [
              'react-native',
              'react-native/*',
              'react-native-*',
              'react-native-*/**',
              '@react-native*',
              '@react-native*/**',
              'expo-*',
              'expo-*/**',
            ],
            message: CAPABILITY_HOMES,
          },
        ],
      },
    ],
  },
};

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

const mechanicalStyleEnforcedOnChangedCodeOnly =
  process.env.ESLINT_DIFF_SCOPED === '1'
    ? [
        {
          files: ['src/**/*.{ts,tsx}'],
          ignores: TEST_FILES,
          rules: {
            'max-lines-per-function': [
              'error',
              { max: 10, skipBlankLines: true, skipComments: true, IIFEs: true },
            ],
            complexity: ['error', 10],
            'no-else-return': ['error', { allowElseIf: false }],
            'id-denylist': [
              'error',
              'data',
              'handler',
              'manager',
              'helper',
              'util',
              'process',
              'doWork',
              'result',
              'temp',
              'arr',
              'val',
              'foo',
            ],
          },
        },
      ]
    : [];

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
  noInlinePlatformBranchesInFeatureUi,
  featureLogicStaysPlatformFree,
  reactCompilerRulesRestoredToErrors,
  relaxationsForJestModuleMockingAndInlineMockComponents,
  relaxationForNativeModulesExpoGoDoesNotBundle,
  ...mechanicalStyleEnforcedOnChangedCodeOnly,
  {
    ignores: ['node_modules/**', '.expo/**', 'dist/**', 'web-build/**', 'coverage/**'],
  },
];
