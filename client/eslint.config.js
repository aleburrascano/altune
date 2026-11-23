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

const TEST_FILES = ['**/__tests__/**'];

const FILES_WHERE_EXPO_GO_FORCES_CONDITIONAL_REQUIRE = [
  'src/features/player/adapters/startPlaybackService.ts',
  'src/features/player/adapters/PlaybackProvider.tsx',
];

const PLATFORM_HOOKS_THAT_READ_BROWSER_GLOBALS = ['src/features/player/view/useMediaSession.ts'];

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
  'Hermes uses the abort-controller polyfill, which lacks this. Check signal.aborted and throw an AbortError instead (see shared/apiClient/adapters/tracks.ts).';

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
  files: [
    'src/features/*/ui/**/*.{ts,tsx}',
    'src/features/*/view/**/*.{ts,tsx}',
    'src/app/**/*.{ts,tsx}',
  ],
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

const BROWSER_GLOBALS = ['window', 'document', 'localStorage', 'navigator'];

const browserGlobalsOnlyInWebCode = {
  name: 'browserGlobalsOnlyInWebCode',
  files: ['src/**/*.{ts,tsx}'],
  ignores: [
    ...TEST_FILES,
    ...PLATFORM_HOOKS_THAT_READ_BROWSER_GLOBALS,
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

const QUERY_ADAPTER_HOOKS = ['src/features/discovery/view/search/useDiscoverSearch.ts'];

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
    'no-test-code-in-app': require('./eslint/rules/no-test-code-in-app'),
    'test-imports-through-doors': require('./eslint/rules/test-imports-through-doors'),
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
  files: ['{src,jest,__tests__}/**/*.{ts,tsx,js,mjs}'],
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
  ignores: [
    ...TEST_FILES,
    'src/shared/ui/theme/**',
    'src/shared/ui/core/theme/**',
    'src/shared/ui/view/theme/**',
  ],
  plugins: { local: localPlugin },
  rules: { 'local/no-raw-style-numbers': 'error' },
};

const noTestCodeInApp = {
  files: ['src/**/*.{ts,tsx,js,mjs}'],
  ignores: [...TEST_FILES, 'src/**/*.test.{ts,tsx}'],
  plugins: { local: localPlugin },
  rules: { 'local/no-test-code-in-app': 'error' },
};

const READ_THE_RULES = 'Read client/src/README.md.';

const PURE_PACKAGES = ['@tanstack/react-query', 'zustand'];

const UI_PACKAGES = [
  'react',
  'react-dom',
  'react-native',
  'react-native-web',
  'react-native-gesture-handler',
  'react-native-reanimated',
  'react-native-safe-area-context',
  'react-native-screens',
  'react-native-svg',
  'react-native-worklets',
  'expo-blur',
  'expo-image',
  'expo-linear-gradient',
  'expo-status-bar',
  'lucide-react-native',
  '@expo-google-fonts/',
];

const PROJECT_IMPORTS = ['\\.', '@/', '@features/', '@shared/'];

const IMAGE_IMPORTS = ['@assets/'];

const importPattern = (name) => (name.endsWith('/') ? name : `${name}(?:/|$)`);

const importsAnyOf = (names) => `^(?:${names.map(importPattern).join('|')})`;

const importsNoneOf = (names) => `^(?!${names.map(importPattern).join('|')})`;

const partFiles = (part) => [`src/{features,shared}/*/${part}/**/*.{ts,tsx}`];

const uiFrameworkOnlyInView = {
  regex: importsAnyOf(UI_PACKAGES),
  message: `Only view/ imports the UI framework. ${READ_THE_RULES}`,
};

const plainPartsImportOnlyPurePackages = {
  name: 'plainPartsImportOnlyPurePackages',
  files: ['core', 'shell', 'ports'].flatMap(partFiles),
  ignores: TEST_FILES,
  rules: {
    'no-restricted-imports': [
      'error',
      {
        patterns: [
          uiFrameworkOnlyInView,
          {
            regex: importsNoneOf([...PROJECT_IMPORTS, ...UI_PACKAGES, ...PURE_PACKAGES]),
            message: `core/, shell/ and ports/ import no package but ${PURE_PACKAGES.join(' and ')}: I/O belongs in adapters/. ${READ_THE_RULES}`,
          },
        ],
      },
    ],
  },
};

const viewImportsOnlyTheUiFramework = {
  name: 'viewImportsOnlyTheUiFramework',
  files: partFiles('view'),
  ignores: TEST_FILES,
  rules: {
    'no-restricted-imports': [
      'error',
      {
        patterns: [
          {
            regex: importsNoneOf([
              ...PROJECT_IMPORTS,
              ...IMAGE_IMPORTS,
              ...UI_PACKAGES,
              ...PURE_PACKAGES,
            ]),
            message: `view/ imports no package but the UI framework, ${PURE_PACKAGES.join(' and ')}: a package that does I/O belongs in adapters/. ${READ_THE_RULES}`,
          },
        ],
      },
    ],
  },
};

const adaptersDrawNothing = {
  name: 'adaptersDrawNothing',
  files: partFiles('adapters'),
  ignores: TEST_FILES,
  rules: { 'no-restricted-imports': ['error', { patterns: [uiFrameworkOnlyInView] }] },
};

const JSX_ONLY_IN_VIEW = `Only view/ draws JSX. ${READ_THE_RULES}`;

const jsxOnlyInView = {
  name: 'jsxOnlyInView',
  files: ['core', 'shell', 'ports', 'adapters'].flatMap(partFiles),
  ignores: TEST_FILES,
  rules: {
    'no-restricted-syntax': [
      'error',
      ...abortApisTheHermesPolyfillLacks,
      { selector: 'JSXElement', message: JSX_ONLY_IN_VIEW },
      { selector: 'JSXFragment', message: JSX_ONLY_IN_VIEW },
    ],
  },
};

const testsImportThroughDoors = {
  name: 'testsImportThroughDoors',
  files: ['src/**/__tests__/**/*.{ts,tsx}'],
  plugins: { local: localPlugin },
  rules: { 'local/test-imports-through-doors': 'error' },
};

const PATCHES = [
  ['jest', 'mock'],
  ['jest', 'doMock'],
  ['jest', 'spyOn'],
  ['jest', 'unstable_mockModule'],
  ['vi', 'mock'],
  ['vi', 'doMock'],
  ['vi', 'spyOn'],
];

const testsPassFakesInNeverPatch = {
  name: 'testsPassFakesInNeverPatch',
  files: ['**/__tests__/**/*.{ts,tsx,js}', 'jest/**/*.{ts,tsx,js}'],
  rules: {
    'no-restricted-properties': [
      'error',
      ...PATCHES.map(([object, property]) => ({
        object,
        property,
        message: `Tests pass fakes in through the module's create*(deps) door and never patch what it imports. ${READ_THE_RULES}`,
      })),
    ],
  },
};

const onlyTheCompositionRootReadsTheEnvironment = {
  name: 'onlyTheCompositionRootReadsTheEnvironment',
  files: ['src/**/*.{ts,tsx}'],
  ignores: [...TEST_FILES, 'src/mobile/**'],
  rules: {
    'no-restricted-properties': [
      'error',
      {
        object: 'process',
        property: 'env',
        message: `Only src/mobile, the composition root, reads process.env and passes the values down. ${READ_THE_RULES}`,
      },
    ],
  },
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
    rules: {
      'no-console': ['warn', { allow: ['warn', 'error'] }],
    },
  },
  rulesWrittenForTheWebAndWrongForReactNative,
  noAbortApisTheHermesPolyfillLacks,
  noInlinePlatformBranchesInFeatureUi,
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
  noTestCodeInApp,
  plainPartsImportOnlyPurePackages,
  viewImportsOnlyTheUiFramework,
  adaptersDrawNothing,
  jsxOnlyInView,
  testsImportThroughDoors,
  testsPassFakesInNeverPatch,
  onlyTheCompositionRootReadsTheEnvironment,
  {
    ignores: ['node_modules/**', '.expo/**', 'dist/**', 'web-build/**', 'coverage/**'],
  },
];
