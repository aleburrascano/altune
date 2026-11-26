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
      '@typescript-eslint/ban-ts-comment': 'error',
      '@typescript-eslint/no-array-delete': 'error',
      '@typescript-eslint/no-base-to-string': 'error',
      '@typescript-eslint/no-duplicate-enum-values': 'error',
      '@typescript-eslint/no-extraneous-class': 'error',
      '@typescript-eslint/no-for-in-array': 'error',
      '@typescript-eslint/no-implied-eval': 'error',
      '@typescript-eslint/no-misused-new': 'error',
      '@typescript-eslint/no-mixed-enums': 'error',
      '@typescript-eslint/no-namespace': 'error',
      '@typescript-eslint/no-non-null-asserted-nullish-coalescing': 'error',
      '@typescript-eslint/no-non-null-asserted-optional-chain': 'error',
      '@typescript-eslint/no-redundant-type-constituents': 'error',
      '@typescript-eslint/no-this-alias': 'error',
      '@typescript-eslint/no-unnecessary-template-expression': 'error',
      '@typescript-eslint/no-unnecessary-type-constraint': 'error',
      '@typescript-eslint/no-unnecessary-type-conversion': 'error',
      '@typescript-eslint/no-unsafe-declaration-merging': 'error',
      '@typescript-eslint/no-unsafe-enum-comparison': 'error',
      '@typescript-eslint/no-unsafe-function-type': 'error',
      '@typescript-eslint/no-unsafe-unary-minus': 'error',
      'no-unused-expressions': 'off',
      '@typescript-eslint/no-unused-expressions': [
        'error',
        { allowShortCircuit: true, enforceForJSX: true },
      ],
      '@typescript-eslint/no-useless-default-assignment': 'error',
      '@typescript-eslint/prefer-as-const': 'error',
      '@typescript-eslint/prefer-literal-enum-member': 'error',
      '@typescript-eslint/prefer-namespace-keyword': 'error',
      '@typescript-eslint/prefer-promise-reject-errors': 'error',
      '@typescript-eslint/prefer-reduce-type-parameter': 'error',
      '@typescript-eslint/prefer-return-this-type': 'error',
      '@typescript-eslint/related-getter-setter-pairs': 'error',
      '@typescript-eslint/restrict-plus-operands': 'error',
      '@typescript-eslint/restrict-template-expressions': 'error',
      '@typescript-eslint/return-await': 'error',
      '@typescript-eslint/triple-slash-reference': 'error',
      '@typescript-eslint/unified-signatures': 'error',
      '@typescript-eslint/adjacent-overload-signatures': 'error',
      '@typescript-eslint/ban-tslint-comment': 'error',
      '@typescript-eslint/class-literal-property-style': 'error',
      '@typescript-eslint/consistent-generic-constructors': 'error',
      '@typescript-eslint/consistent-indexed-object-style': 'error',
      '@typescript-eslint/no-confusing-non-null-assertion': 'error',
      '@typescript-eslint/prefer-find': 'error',
      '@typescript-eslint/prefer-for-of': 'error',
      '@typescript-eslint/prefer-function-type': 'error',
      '@typescript-eslint/prefer-includes': 'error',
      '@typescript-eslint/prefer-string-starts-ends-with': 'error',
      'react-hooks/void-use-memo': 'error',
      '@typescript-eslint/prefer-nullish-coalescing': 'error',
      '@typescript-eslint/prefer-regexp-exec': 'error',
      '@typescript-eslint/no-invalid-void-type': 'error',
      '@typescript-eslint/no-duplicate-type-constituents': 'error',
      '@typescript-eslint/no-unnecessary-type-arguments': 'error',
      '@typescript-eslint/no-unnecessary-type-parameters': 'error',
    },
  },
];

const WITHOUT_TYPE_INFO = process.env.ESLINT_WITHOUT_TYPES === '1';

const needsTypeInfo = (ruleName) => {
  const prefix = '@typescript-eslint/';
  return (
    ruleName.startsWith(prefix) &&
    tsPlugin.rules[ruleName.slice(prefix.length)]?.meta.docs.requiresTypeChecking === true
  );
};

const withoutTypeInfo = (block) => {
  if (!WITHOUT_TYPE_INFO) return block;
  const { projectService, ...parserOptions } = block.languageOptions?.parserOptions ?? {};
  return {
    ...block,
    ...(block.languageOptions && {
      languageOptions: { ...block.languageOptions, parserOptions },
    }),
    ...(block.rules && {
      rules: Object.fromEntries(
        Object.entries(block.rules).filter(([name]) => !needsTypeInfo(name)),
      ),
    }),
  };
};

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

const mechanicalStyleEnforcedOnChangedCodeOnly =
  process.env.ESLINT_DIFF_SCOPED === '1'
    ? [
        {
          files: ['src/**/*.{ts,tsx}'],
          ignores: TEST_FILES,
          rules: {
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
            '@typescript-eslint/unbound-method': 'error',
            '@typescript-eslint/no-non-null-assertion': 'error',
            '@typescript-eslint/no-unnecessary-condition': 'error',
            '@typescript-eslint/strict-boolean-expressions': 'error',
          },
        },
        ...house.configs.recommended,
        ...house.configs.typed.map((block) => ({
          ...block,
          files: typedLinting[0].files,
          ignores: typedLinting[0].ignores,
          languageOptions: typedLinting[0].languageOptions,
        })),
      ].map(withoutTypeInfo)
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

const WARNING_SEVERITIES = new Set(['warn', 1]);

const warningsAreErrors = (block) => {
  if (!block.rules) return block;
  const rules = Object.fromEntries(
    Object.entries(block.rules).map(([name, setting]) => {
      const [severity, ...options] = [setting].flat();
      return [name, WARNING_SEVERITIES.has(severity) ? ['error', ...options] : setting];
    }),
  );
  return { ...block, rules };
};

const styleRulesConfig = [
  ...expoConfig.map(warningsAreErrors),
  ...typedLinting.map(withoutTypeInfo),
  typeScriptPluginRegisteredDirectlyRatherThanViaExpoConfig,
  {
    rules: {
      'no-console': ['error', { allow: ['warn', 'error'] }],
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
