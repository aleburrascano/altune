const expoConfig = require('eslint-config-expo/flat');
const tsPlugin = require('@typescript-eslint/eslint-plugin');
const tsParser = require('@typescript-eslint/parser');
const house = require('eslint-config-house-ts');
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

const FEATURES = ['auth', 'detail', 'discovery', 'library', 'playback', 'settings'];

const TEST_FILES = ['**/__tests__/**'];

const FILES_WHERE_EXPO_GO_FORCES_CONDITIONAL_REQUIRE = [
  'src/features/playback/adapters/startPlaybackService.ts',
  'src/features/playback/adapters/PlaybackProvider.tsx',
];

const PLATFORM_HOOKS_MOVED_INTO_HOOKS_FOLDER = [
  'src/features/playback/hooks/usePlaybackPosition.ts',
  'src/features/playback/hooks/usePlaybackSignals.ts',
  'src/features/playback/hooks/useQueueResume.ts',
  'src/features/playback/hooks/useScrubberGesture.ts',
  'src/features/playback/hooks/useMediaSession.ts',
  'src/features/library/hooks/useLibraryGridLayout.ts',
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
  ...['shared', 'features'].map((layer) => ({
    target: `./src/${layer}`,
    from: './src/chrome',
    message: `src/${layer} must not import from src/chrome: layering is app, chrome, features, shared.`,
  })),
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

const HERMES_ABORT_MESSAGE =
  'Hermes uses the abort-controller polyfill, which lacks this. Check signal.aborted and throw an AbortError instead (see shared/apiClient/tracks.ts).';

const abortApisTheHermesPolyfillLacks = [
  { selector: "MemberExpression[property.name='throwIfAborted']", message: HERMES_ABORT_MESSAGE },
  {
    selector: "MemberExpression[object.name='AbortSignal'][property.name='any']",
    message: HERMES_ABORT_MESSAGE,
  },
  {
    selector: "MemberExpression[object.name='AbortSignal'][property.name='timeout']",
    message: HERMES_ABORT_MESSAGE,
  },
];

const noAbortApisTheHermesPolyfillLacks = {
  name: 'noAbortApisTheHermesPolyfillLacks',
  files: ['src/**/*.{ts,tsx}'],
  rules: {
    'no-restricted-syntax': ['error', ...abortApisTheHermesPolyfillLacks],
  },
};

const noInlinePlatformBranchesInFeatureUi = {
  name: 'noInlinePlatformBranchesInFeatureUi',
  files: ['src/features/*/ui/**/*.{ts,tsx}', 'src/app/**/*.{ts,tsx}'],
  ignores: TEST_FILES,
  rules: {
    'no-restricted-syntax': [
      'error',
      ...abortApisTheHermesPolyfillLacks,
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
  'Logic must not import a platform API: Alert goes to @shared/ui/dialog, Keyboard to @shared/ui/keyboard, AppState to @shared/lifecycle, Platform and expo-constants to @shared/device, expo-router to @shared/navigation, expo-crypto to @shared/id/uuid, expo-secure-store to @shared/auth/authStorage; or move the code into a ui/, native/ or web/ folder, or a .web.ts(x) split file.';

const logicStaysPlatformFree = {
  name: 'logicStaysPlatformFree',
  files: ['src/features/**/*.{ts,tsx}', 'src/shared/**/*.{ts,tsx}'],
  ignores: [
    ...TEST_FILES,
    ...PLATFORM_HOOKS_MOVED_INTO_HOOKS_FOLDER,
    'src/features/*/ui/**',
    'src/features/**/native/**',
    'src/features/**/web/**',
    'src/features/**/*.web.{ts,tsx}',
    'src/features/**/*.native.{ts,tsx}',
    'src/shared/ui/**',
    'src/shared/*/ui/**',
    'src/shared/**/native/**',
    'src/shared/**/web/**',
    'src/shared/**/*.web.{ts,tsx}',
    'src/shared/**/*.native.{ts,tsx}',
    'src/shared/device/device.ts',
    'src/shared/lifecycle/appState.ts',
    'src/shared/navigation/expoRouterNavigator.ts',
    'src/shared/id/uuid.ts',
    'src/shared/auth/authStorage.ts',
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

const BROWSER_GLOBALS = ['window', 'document', 'localStorage', 'navigator'];

const browserGlobalsOnlyInWebCode = {
  name: 'browserGlobalsOnlyInWebCode',
  files: ['src/**/*.{ts,tsx}'],
  ignores: [
    ...TEST_FILES,
    ...PLATFORM_HOOKS_MOVED_INTO_HOOKS_FOLDER,
    'src/**/web/**',
    'src/**/*.web.{ts,tsx}',
  ],
  rules: {
    'no-restricted-globals': [
      'error',
      ...BROWSER_GLOBALS.map((name) => ({
        name,
        message: 'Browser globals live only in .web.ts(x) files or web/ folders.',
      })),
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

const QUERY_ADAPTER_HOOKS = ['src/features/discovery/hooks/useDiscoverSearch.ts'];

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
            'id-match': ['error', '^(?!data$)', { properties: false, onlyDeclarations: true }],
            'id-denylist': [
              'error',
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
        {
          files: QUERY_ADAPTER_HOOKS,
          rules: { 'max-lines-per-function': 'off' },
        },
        ...house.configs.recommended,
        ...house.configs.typed.map((block) => ({
          ...block,
          files: typedLinting[0].files,
          ignores: typedLinting[0].ignores,
          languageOptions: typedLinting[0].languageOptions,
        })),
      ]
    : [];

const localPlugin = {
  rules: {
    'no-comments': require('./eslint/rules/no-comments'),
    'no-banned-noun': require('./eslint/rules/no-banned-noun'),
    'one-alias-per-folder': require('./eslint/rules/one-alias-per-folder'),
    'no-parent-relative-imports': require('./eslint/rules/no-parent-relative-imports'),
    'top-level-function-declarations': require('./eslint/rules/top-level-function-declarations'),
    'no-raw-style-numbers': require('./eslint/rules/no-raw-style-numbers'),
  },
};

const noCommentsAnywhere = {
  files: ['**/*.{ts,tsx,js,jsx,mjs,cjs}'],
  linterOptions: { noInlineConfig: true },
  plugins: { local: localPlugin },
  languageOptions: {
    parser: tsParser,
    parserOptions: { ecmaFeatures: { jsx: true } },
  },
  rules: { 'local/no-comments': 'error' },
};

const theNounIsTrackNeverSong = {
  files: ['src/**/*.{ts,tsx}'],
  ignores: TEST_FILES,
  plugins: { local: localPlugin },
  rules: { 'local/no-banned-noun': 'error' },
};

const oneAliasPerFolder = {
  files: ['{src,jest,__tests__,e2e}/**/*.{ts,tsx,js,mjs}'],
  plugins: { local: localPlugin },
  rules: { 'local/one-alias-per-folder': 'error' },
};

const noParentRelativeImports = {
  files: ['{src,__tests__}/**/*.{ts,tsx,js,mjs}'],
  plugins: { local: localPlugin },
  rules: { 'local/no-parent-relative-imports': 'error' },
};

const topLevelFunctionDeclarations = {
  files: ['src/**/*.{ts,tsx}'],
  ignores: TEST_FILES,
  plugins: { local: localPlugin },
  rules: { 'local/top-level-function-declarations': 'error' },
};

const noRawStyleNumbers = {
  files: ['src/**/*.{ts,tsx}'],
  ignores: [...TEST_FILES, 'src/shared/ui/theme/**'],
  plugins: { local: localPlugin },
  rules: { 'local/no-raw-style-numbers': 'error' },
};

const SRC_ONLY = 'src/**/*';

const onlyWithinSrc = (block) => {
  const isGlobalIgnores = Object.keys(block).every((key) => key === 'ignores' || key === 'name');
  if (isGlobalIgnores) return block;
  const files = block.files ?? ['**/*'];
  return { ...block, files: files.map((pattern) => [SRC_ONLY, ...[pattern].flat()]) };
};

const styleRulesConfig = [
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
  noAbortApisTheHermesPolyfillLacks,
  noInlinePlatformBranchesInFeatureUi,
  logicStaysPlatformFree,
  browserGlobalsOnlyInWebCode,
  reactCompilerRulesRestoredToErrors,
  relaxationsForJestModuleMockingAndInlineMockComponents,
  relaxationForNativeModulesExpoGoDoesNotBundle,
  ...mechanicalStyleEnforcedOnChangedCodeOnly,
];

module.exports = [
  ...styleRulesConfig.map(onlyWithinSrc),
  noCommentsAnywhere,
  theNounIsTrackNeverSong,
  oneAliasPerFolder,
  noParentRelativeImports,
  topLevelFunctionDeclarations,
  noRawStyleNumbers,
  {
    ignores: ['node_modules/**', '.expo/**', 'dist/**', 'web-build/**', 'coverage/**'],
  },
];
